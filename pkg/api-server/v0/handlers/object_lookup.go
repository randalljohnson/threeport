package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	strcase "github.com/iancoleman/strcase"
	gorm "gorm.io/gorm"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	client_lib "github.com/threeport/threeport/pkg/client/lib/v0"
)

// moduleHTTPClient is the HTTP client used for module API dispatch. It loads
// the api-server's own mTLS certs from /etc/threeport, falling back to
// http.DefaultClient when those aren't present (auth disabled).
var moduleHTTPClient = func() *http.Client {
	c, err := client_lib.GetHTTPClient(true, "", "", "", "")
	if err != nil {
		return http.DefaultClient
	}
	return c
}()

// GetObjectNames returns id->name for each id of the given object type,
// dispatching to core SQL or the owning module's API as needed. Returns an
// empty map if the type has no resolver. includeDeleted=true includes
// soft-deleted rows.
func GetObjectNames(db *gorm.DB, objectType string, ids []uint, includeDeleted bool) (map[uint]string, error) {
	if len(ids) == 0 {
		return map[uint]string{}, nil
	}

	names, err := GetCoreObjectNamesByIDs(db, objectType, ids, includeDeleted)
	if err == nil {
		return names, nil
	}
	if !errors.Is(err, ErrUnknownCoreType) {
		return nil, err
	}

	endpoint, path, err := GetModuleRouteForType(db, objectType)
	if err != nil {
		return nil, err
	}
	if endpoint == "" {
		return map[uint]string{}, nil
	}

	return getNamesFromModule(endpoint, path, ids, includeDeleted)
}

// GetObjectIDByName returns the ID of the named object of the given type,
// dispatching to core SQL or the owning module's API as needed.
func GetObjectIDByName(db *gorm.DB, objectType, name string) (uint, error) {
	id, err := GetCoreObjectIDByName(db, objectType, name)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, ErrUnknownCoreType) {
		return 0, err
	}

	endpoint, path, err := GetModuleRouteForType(db, objectType)
	if err != nil {
		return 0, err
	}
	if endpoint == "" {
		return 0, fmt.Errorf("object type %q not owned by core or any registered module", objectType)
	}

	return getIDFromModuleByName(endpoint, path, objectType, name)
}

// GetModuleRouteForType returns the upstream endpoint and CRUD path for
// an ObjectType prefixed with its module's ApiNamespace, or empty strings
// if the type isn't prefixed or no module owns it.
func GetModuleRouteForType(db *gorm.DB, objectType string) (string, string, error) {
	namespace, version, typeName, ok := parseQualifiedType(objectType)
	if !ok {
		return "", "", nil
	}

	q := db.Table("v0_module_api_routes mar")

	// join the module to filter by namespace and read endpoint
	q = q.Joins("JOIN v0_module_apis ma ON ma.id = mar.module_api_id").
		Where("ma.api_namespace = ? AND ma.core = false", namespace)

	// join the module object to filter by name and version
	q = q.Joins("JOIN v0_module_api_routes_module_objects rel ON rel.module_api_route_id = mar.id").
		Joins("JOIN v0_module_objects mo ON mo.id = rel.module_object_id").
		Where("mo.name = ? AND mo.version = ?", typeName, version)

	// exclude the versions route, which ends in /versions
	q = q.Where("mar.path NOT LIKE ?", "%/versions")

	var result struct {
		Path     string
		Endpoint string
	}
	if err := q.Select("mar.path, ma.endpoint").Limit(1).Scan(&result).Error; err != nil {
		return "", "", fmt.Errorf("failed to look up module route for %s: %w", objectType, err)
	}
	if result.Path == "" || result.Endpoint == "" {
		return "", "", nil
	}
	return result.Endpoint, result.Path, nil
}

// resolveObjectType returns one ObjectType per registered version of
// the kind, erroring on cross-namespace ambiguity.
func resolveObjectType(db *gorm.DB, bareKind string) ([]string, error) {
	type row struct {
		Namespace string
		Version   string
	}
	var rows []row
	if err := db.
		Table("v0_module_objects mo").
		Select("ma.api_namespace AS namespace, mo.version AS version").
		Joins("JOIN v0_module_apis ma ON ma.id = mo.module_api_id").
		Where("mo.name = ? AND ma.core = false", bareKind).
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to resolve object type for %q: %w", bareKind, err)
	}

	// surface ambiguity when a kind is registered in more than one namespace
	namespaces := map[string]bool{}
	for _, r := range rows {
		namespaces[r.Namespace] = true
	}
	if len(namespaces) > 1 {
		kebab := strcase.ToKebab(bareKind)
		hints := make([]string, 0, len(namespaces))
		for ns := range namespaces {
			hints = append(hints, fmt.Sprintf("%s/%s", ns, kebab))
		}
		return nil, fmt.Errorf(
			"kind %q is registered in multiple modules; specify one of: %s",
			kebab, strings.Join(hints, ", "),
		)
	}

	// module match: return every registered version
	if len(rows) > 0 {
		out := make([]string, 0, len(rows))
		for _, r := range rows {
			out = append(out, fmt.Sprintf("%s/%s.%s", r.Namespace, r.Version, bareKind))
		}
		return out, nil
	}

	// no module match; treat as core and enumerate every version
	// registered at startup for the kind. empty result means the kind
	// isn't registered anywhere.
	obj, ok := apiserver_lib.ObjectVersions[bareKind]
	if !ok {
		return nil, nil
	}
	out := make([]string, 0, len(obj.Versions))
	for _, v := range obj.Versions {
		out = append(out, fmt.Sprintf("%s.%s", v, bareKind))
	}
	return out, nil
}

// parseQualifiedType splits "<namespace>/<version>.<TypeName>" into its
// three parts. Returns ok=false for unqualified strings (core types).
func parseQualifiedType(objectType string) (namespace, version, typeName string, ok bool) {
	slashIdx := strings.Index(objectType, "/")
	if slashIdx < 1 || slashIdx == len(objectType)-1 {
		return "", "", "", false
	}
	namespace = objectType[:slashIdx]
	rest := objectType[slashIdx+1:]
	dotIdx := strings.Index(rest, ".")
	if dotIdx < 1 || dotIdx == len(rest)-1 {
		return "", "", "", false
	}
	return namespace, rest[:dotIdx], rest[dotIdx+1:], true
}

// getNamesFromModule fetches each id's Name from the module API, skipping
// ids whose lookups fail.
func getNamesFromModule(endpoint, path string, ids []uint, includeDeleted bool) (map[uint]string, error) {
	out := make(map[uint]string, len(ids))
	suffix := ""
	if includeDeleted {
		suffix = "?includedeleted=true"
	}
	for _, id := range ids {
		url := fmt.Sprintf("%s%s/%d%s", endpoint, path, id, suffix)
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
		row, ok := getFirstRow(resp)
		if !ok {
			continue
		}
		if name, ok := row["Name"].(string); ok && name != "" {
			out[id] = name
		}
	}
	return out, nil
}

// getIDFromModuleByName issues a name-filtered GET to the module API and
// returns the first matching row's ID.
func getIDFromModuleByName(endpoint, path, objectType, name string) (uint, error) {
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
	row, ok := getFirstRow(resp)
	if !ok {
		return 0, fmt.Errorf("no %s found with name %q in module response", objectType, name)
	}
	switch v := row["ID"].(type) {
	case float64:
		return uint(v), nil
	case json.Number:
		i, err := v.Int64()
		if err != nil {
			return 0, fmt.Errorf("invalid ID for %s: %w", objectType, err)
		}
		return uint(i), nil
	}
	return 0, fmt.Errorf("no %s found with name %q in module response", objectType, name)
}

// getFirstRow returns the first object in the response's Data array as a
// generic JSON map, or false if there isn't one.
func getFirstRow(resp *apiserver_lib.Response) (map[string]interface{}, bool) {
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
