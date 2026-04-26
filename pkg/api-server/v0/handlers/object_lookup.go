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

	endpoint, path, err := FindModuleRouteForType(db, objectType)
	if err != nil {
		return nil, err
	}
	if endpoint == "" {
		return map[uint]string{}, nil
	}

	return lookupNamesFromModule(endpoint, path, ids)
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

	endpoint, path, err := FindModuleRouteForType(db, objectType)
	if err != nil {
		return 0, err
	}
	if endpoint == "" {
		return 0, fmt.Errorf("object type %q not owned by core or any registered module", objectType)
	}

	return lookupIDFromModuleByName(endpoint, path, objectType, name)
}

// FindModuleRouteForType returns the upstream endpoint and the registered
// CRUD path for the given namespace-qualified ObjectType (e.g.
// "example.com/v0.RouterDefinition"), or empty strings if the type isn't
// qualified or no module owns it. Looks up the owning module by exact
// `api_namespace` match, then resolves the CRUD route by kebab-pluralized
// type name within that module.
func FindModuleRouteForType(db *gorm.DB, objectType string) (string, string, error) {
	namespace, kebabPlural, ok := parseQualifiedType(objectType)
	if !ok {
		return "", "", nil
	}

	var modApi api_v0.ModuleApi
	if err := db.Where("api_namespace = ? AND core = ?", namespace, false).First(&modApi).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", "", nil
		}
		return "", "", fmt.Errorf("failed to query module_apis for namespace %s: %w", namespace, err)
	}

	// match the CRUD route owned by this module - its path ends in
	// "/v<version>/<kebab-plural>" - ignore /versions and similar siblings.
	pathSuffix := "/" + kebabPlural
	var route api_v0.ModuleApiRoute
	if err := db.Where("module_api_id = ? AND path LIKE ?", modApi.ID, "%"+pathSuffix).First(&route).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", "", nil
		}
		return "", "", fmt.Errorf("failed to query module_api_routes for namespace %s: %w", namespace, err)
	}

	if modApi.Endpoint == nil || route.Path == nil {
		return "", "", nil
	}
	return *modApi.Endpoint, *route.Path, nil
}

// parseQualifiedType splits a namespace-qualified ObjectType
// ("<namespace>/<version>.<TypeName>") into its namespace and the
// kebab-pluralized REST tail. Returns ok=false for unqualified strings
// (core types) which don't go through module dispatch.
func parseQualifiedType(objectType string) (string, string, bool) {
	slashIdx := strings.Index(objectType, "/")
	if slashIdx < 1 || slashIdx == len(objectType)-1 {
		return "", "", false
	}
	namespace := objectType[:slashIdx]
	rest := objectType[slashIdx+1:]
	dotIdx := strings.Index(rest, ".")
	if dotIdx < 1 || dotIdx == len(rest)-1 {
		return "", "", false
	}
	typeName := rest[dotIdx+1:]
	plural := pluralize.NewClient().Plural(strcase.ToKebab(typeName))
	return namespace, plural, true
}

// lookupNamesFromModule fetches each object by ID from the module API and
// extracts its Name. Returns a partial map even on per-id errors so a
// single bad id doesn't fail the whole batch.
func lookupNamesFromModule(endpoint, path string, ids []uint) (map[uint]string, error) {
	out := make(map[uint]string, len(ids))
	for _, id := range ids {
		url := fmt.Sprintf("%s%s/%d?includedeleted=true", endpoint, path, id)
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
func lookupIDFromModuleByName(endpoint, path, objectType, name string) (uint, error) {
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

