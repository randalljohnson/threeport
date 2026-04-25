package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	pluralize "github.com/gertd/go-pluralize"
	strcase "github.com/iancoleman/strcase"
	gorm "gorm.io/gorm"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	api_v0 "github.com/threeport/threeport/pkg/api/v0"
	client_lib "github.com/threeport/threeport/pkg/client/lib/v0"
)

// moduleHTTPClient is reused across module dispatch calls. The api-server
// runs in a long-lived process, so a shared client with pooled connections
// is appropriate here.
var moduleHTTPClient = &http.Client{Timeout: 10 * time.Second}

// LookupObjectNames returns id->name for each id of the given object type.
// Tries core types first via GetCoreObjectNamesByIDs (generated batched SQL);
// on ErrUnknownCoreType falls back to per-id HTTP GETs against the owning
// module API. Returns an empty map if no resolver is found.
func LookupObjectNames(db *gorm.DB, objectType string, ids []uint) (map[uint]string, error) {
	if len(ids) == 0 {
		return map[uint]string{}, nil
	}

	names, err := GetCoreObjectNamesByIDs(db, objectType, ids)
	if err == nil {
		return names, nil
	}
	if !errors.Is(err, ErrUnknownCoreType) {
		return nil, err
	}

	endpoint, err := FindModuleEndpointForType(db, objectType)
	if err != nil {
		return nil, err
	}
	if endpoint == "" {
		return map[uint]string{}, nil
	}

	return lookupNamesFromModule(endpoint, objectType, ids)
}

// LookupObjectIDByName returns the ID of the named object of the given type.
// Tries core types first via GetCoreObjectIDByName; on ErrUnknownCoreType
// falls back to a single HTTP GET with name filter against the owning
// module API.
func LookupObjectIDByName(db *gorm.DB, objectType, name string) (uint, error) {
	id, err := GetCoreObjectIDByName(db, objectType, name)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, ErrUnknownCoreType) {
		return 0, err
	}

	endpoint, err := FindModuleEndpointForType(db, objectType)
	if err != nil {
		return 0, err
	}
	if endpoint == "" {
		return 0, fmt.Errorf("object type %q not owned by core or any registered module", objectType)
	}

	return lookupIDFromModuleByName(endpoint, objectType, name)
}

// FindModuleEndpointForType returns the upstream endpoint that owns the
// given ObjectType, or an empty string if no module owns it. The lookup
// derives the type's REST path prefix from its ObjectType
// (e.g. "v0.RouterDefinition" -> "/v0/router-definitions") and queries
// v0_module_api_routes for that path. Path derivation matches what modules
// register at startup.
func FindModuleEndpointForType(db *gorm.DB, objectType string) (string, error) {
	path, err := pathForType(objectType)
	if err != nil {
		return "", err
	}

	var route api_v0.ModuleApiRoute
	if err := db.Where("path = ?", path).First(&route).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("failed to query module_api_routes for %s: %w", path, err)
	}

	var modApi api_v0.ModuleApi
	if err := db.Where("id = ? AND core = ?", route.ModuleApiID, false).First(&modApi).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("failed to query module_apis for route %s: %w", path, err)
	}

	if modApi.Endpoint == nil {
		return "", nil
	}
	return *modApi.Endpoint, nil
}

// pathForType converts a versioned ObjectType like "v0.RouterDefinition" to
// its REST path prefix "/v0/router-definitions" using the same kebab-plural
// convention modules use when registering routes.
func pathForType(objectType string) (string, error) {
	parts := strings.SplitN(objectType, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("invalid object type %q: expected <version>.<TypeName>", objectType)
	}
	plural := pluralize.NewClient().Plural(strcase.ToKebab(parts[1]))
	return fmt.Sprintf("/%s/%s", parts[0], plural), nil
}

// lookupNamesFromModule fetches each object by ID from the module API and
// extracts its Name. Returns a partial map even on per-id errors so a
// single bad id doesn't fail the whole batch.
func lookupNamesFromModule(endpoint, objectType string, ids []uint) (map[uint]string, error) {
	path, err := pathForType(objectType)
	if err != nil {
		return nil, err
	}

	out := make(map[uint]string, len(ids))
	for _, id := range ids {
		url := fmt.Sprintf("%s%s/%d", endpoint, path, id)
		resp, err := client_lib.GetResponse(
			moduleHTTPClient,
			url,
			http.MethodGet,
			new(bytes.Buffer),
			map[string]string{},
			http.StatusOK,
		)
		if err != nil {
			continue
		}
		if name, ok := pluckNameFromResponse(resp); ok {
			out[id] = name
		}
	}
	return out, nil
}

// lookupIDFromModuleByName issues a single name-filtered GET to the module
// API and pulls the ID of the first matching row.
func lookupIDFromModuleByName(endpoint, objectType, name string) (uint, error) {
	path, err := pathForType(objectType)
	if err != nil {
		return 0, err
	}

	url := fmt.Sprintf("%s%s?name=%s", endpoint, path, name)
	resp, err := client_lib.GetResponse(
		moduleHTTPClient,
		url,
		http.MethodGet,
		new(bytes.Buffer),
		map[string]string{},
		http.StatusOK,
	)
	if err != nil {
		return 0, fmt.Errorf("module lookup of %s by name failed: %w", objectType, err)
	}

	id, ok := pluckIDFromResponse(resp)
	if !ok {
		return 0, fmt.Errorf("no %s found with name %q in module response", objectType, name)
	}
	return id, nil
}

// pluckNameFromResponse extracts the Name field of the first object in a
// threeport API response's Data array.
func pluckNameFromResponse(resp *apiserver_lib.Response) (string, bool) {
	row, ok := firstRow(resp)
	if !ok {
		return "", false
	}
	name, ok := row["Name"].(string)
	if !ok || name == "" {
		return "", false
	}
	return name, true
}

// pluckIDFromResponse extracts the ID field of the first object in a
// threeport API response's Data array. Handles both float64 and json.Number
// since responses use UseNumber decoding inconsistently.
func pluckIDFromResponse(resp *apiserver_lib.Response) (uint, bool) {
	row, ok := firstRow(resp)
	if !ok {
		return 0, false
	}
	switch v := row["ID"].(type) {
	case float64:
		return uint(v), true
	case json.Number:
		i, err := v.Int64()
		if err != nil {
			return 0, false
		}
		return uint(i), true
	}
	return 0, false
}

// firstRow normalizes a threeport response Data field into the first object
// as a map. Data may arrive as a single object or as a slice of objects.
func firstRow(resp *apiserver_lib.Response) (map[string]interface{}, bool) {
	if resp == nil {
		return nil, false
	}
	if len(resp.Data) == 0 {
		return nil, false
	}
	row, ok := resp.Data[0].(map[string]interface{})
	if !ok {
		return nil, false
	}
	return row, true
}

