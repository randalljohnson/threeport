package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	strcase "github.com/iancoleman/strcase"
	gorm "gorm.io/gorm"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	client_lib "github.com/threeport/threeport/pkg/client/lib/v0"
)

// moduleHTTPClient is the HTTP client used for module API dispatch.
// Module api-servers don't currently support server-side TLS, so we
// always use plain HTTP regardless of threeport-core's auth-enabled
// setting; the call is in-cluster service-to-service. A short timeout
// caps cross-module lookups so a misconfigured/unreachable module
// doesn't stall response formatting; the caller falls back to id-only
// when the lookup fails.
var moduleHTTPClient = func() *http.Client {
	c, err := client_lib.GetHTTPClient(false, "", "", "", "")
	if err != nil {
		c = http.DefaultClient
	}
	c.Timeout = 3 * time.Second
	return c
}()

// GetObjectNames returns id->name for each id of the given object type,
// dispatching to core SQL or the owning module's API as needed. Returns an
// empty map if the type has no resolver. includeDeleted=true includes
// soft-deleted rows.
func GetObjectNames(db *gorm.DB, objectType string, ids []uint, includeDeleted bool) (map[uint]string, error) {
	// nothing to look up
	if len(ids) == 0 {
		return map[uint]string{}, nil
	}

	// try the core SQL resolver first; most types live in core
	names, err := GetCoreObjectNamesByIDs(db, objectType, ids, includeDeleted)
	if err == nil {
		return names, nil
	}

	// a non-"unknown core type" error is a real failure; surface it
	if !errors.Is(err, ErrUnknownCoreType) {
		return nil, err
	}

	// core doesn't know this type; look up which module owns it
	endpoint, path, err := GetModuleRouteForType(db, objectType)
	if err != nil {
		return nil, err
	}

	// no module owns it either; return empty so callers can degrade
	// gracefully rather than failing the whole response
	if endpoint == "" {
		return map[uint]string{}, nil
	}

	// dispatch to the owning module's CRUD endpoint
	return getNamesFromModule(endpoint, path, ids, includeDeleted)
}

// GetObjectIDByName returns the ID of the named object of the given type,
// dispatching to core SQL or the owning module's API as needed.
func GetObjectIDByName(db *gorm.DB, objectType, name string) (uint, error) {
	// try the core SQL resolver first
	id, err := GetCoreObjectIDByName(db, objectType, name)
	if err == nil {
		return id, nil
	}

	// a non-"unknown core type" error is a real failure; surface it
	if !errors.Is(err, ErrUnknownCoreType) {
		return 0, err
	}

	// core doesn't know this type; look up which module owns it
	endpoint, path, err := GetModuleRouteForType(db, objectType)
	if err != nil {
		return 0, err
	}

	// no module owns it either; this resolver has no fallback so the
	// caller needs a hard error rather than a soft empty
	if endpoint == "" {
		return 0, fmt.Errorf("object type %q not owned by core or any registered module", objectType)
	}

	// dispatch to the owning module's CRUD endpoint
	return getIDFromModuleByName(endpoint, path, objectType, name)
}

// GetModuleRouteForType returns the endpoint and CRUD path that the
// owning module exposes for a given module type. The input must be the
// qualified form "<api-namespace>/<version>.<TypeName>". Returns empty
// strings for inputs that don't parse as qualified or don't match a
// registered module.
//
// For an input of "example.com/v0.Widget", returns:
//   endpoint = "http://widget-api.threeport-control-plane:80"
//   path     = "/v0/widgets"
func GetModuleRouteForType(db *gorm.DB, objectType string) (string, string, error) {
	// fresh session to avoid inheriting WHERE clauses from the caller's db
	db = db.Session(&gorm.Session{NewDB: true})

	// unqualified inputs aren't module types; caller falls through to a
	// graceful empty result
	namespace, version, typeName, ok := parseQualifiedType(objectType)
	if !ok {
		return "", "", nil
	}

	// start from the routes table; we'll JOIN outward to filter by
	// module, type, and version.
	//   rows so far: every row in v0_module_api_routes
	q := db.Table("v0_module_api_routes AS route")

	// pull in the parent ModuleApi row so its endpoint and namespace
	// are queryable.
	//   rows so far: every (route, module_api) pair
	q = q.Joins("JOIN v0_module_apis AS module_api ON module_api.id = route.module_api_id")

	// keep only routes owned by the named non-core module
	// (core=false skips the threeport core API itself).
	//   rows so far: (route, module_api) pairs for "example.com" only
	q = q.Where("module_api.api_namespace = ? AND module_api.core = false", namespace)

	// follow the ModuleApiRoute↔ModuleObject junction; the m2m is needed
	// because one route can in principle serve multiple registered types.
	//   rows so far: (route, module_api, link) triples for "example.com"
	q = q.Joins("JOIN v0_module_api_routes_module_objects AS link ON link.module_api_route_id = route.id")

	// land on the registered type's row, where its name and version live.
	//   rows so far: (route, module_api, link, object) for every type on
	//   "example.com"'s routes
	q = q.Joins("JOIN v0_module_objects AS object ON object.id = link.module_object_id")

	// keep only the specific (name, version) the caller asked for.
	//   rows so far: routes serving "example.com"'s "Widget" at "v0"
	//   (typically 2: the CRUD path and the /versions discovery path)
	q = q.Where("object.name = ? AND object.version = ?", typeName, version)

	// each registered type gets two routes: a CRUD path and a /versions
	// discovery path. This query wants the CRUD endpoint, so exclude the
	// discovery one by path suffix.
	//   rows so far: 1 - the CRUD route for "example.com/v0.Widget"
	q = q.Where("route.path NOT LIKE ?", "%/versions")

	var result struct {
		Path     string
		Endpoint string
	}
	if err := q.Select("route.path, module_api.endpoint").Limit(1).Scan(&result).Error; err != nil {
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

	// start from the registered objects table; JOIN outward to get the
	// namespace each registration belongs to
	q := db.Table("v0_module_objects AS object")

	// pull in the parent ModuleApi row so its namespace is queryable
	q = q.Joins("JOIN v0_module_apis AS module_api ON module_api.id = object.module_api_id")

	// keep only registrations of the bare kind, owned by non-core modules
	q = q.Where("object.name = ? AND module_api.core = false", bareKind)

	if err := q.Select("module_api.api_namespace AS namespace, object.version AS version").Scan(&rows).Error; err != nil {
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

// parseQualifiedType splits "<api-namespace>/<version>.<TypeName>" into its
// three parts. Returns ok=false for unqualified strings (core types).
//
// For "example.com/v0.Widget":
//   namespace = "example.com", version = "v0", typeName = "Widget", ok = true
func parseQualifiedType(objectType string) (namespace, version, typeName string, ok bool) {
	// find the slash that separates the namespace from the rest.
	// for "example.com/v0.Widget" this is at index 11
	slashIdx := strings.Index(objectType, "/")

	// reject malformed inputs: no slash, slash at position 0 ("/foo"),
	// or slash as the last char ("foo/"). either side of the slash
	// must be non-empty
	if slashIdx < 1 || slashIdx == len(objectType)-1 {
		return "", "", "", false
	}

	// everything before the slash is the api namespace.
	// for "example.com/v0.Widget": namespace = "example.com"
	namespace = objectType[:slashIdx]

	// everything after the slash is "<version>.<TypeName>".
	// for "example.com/v0.Widget": rest = "v0.Widget"
	rest := objectType[slashIdx+1:]

	// find the dot that separates the version from the type name.
	// for rest = "v0.Widget" this is at index 2
	dotIdx := strings.Index(rest, ".")

	// reject malformed inputs: no dot, dot at position 0 (".Widget"),
	// or dot as the last char ("v0."). either side of the dot must
	// be non-empty
	if dotIdx < 1 || dotIdx == len(rest)-1 {
		return "", "", "", false
	}

	// split rest into version and type name on the dot.
	// for "v0.Widget": version = "v0", typeName = "Widget"
	return namespace, rest[:dotIdx], rest[dotIdx+1:], true
}

// getNamesFromModule fetches each id's Name from the module API, skipping
// ids whose lookups fail.
func getNamesFromModule(endpoint, path string, ids []uint, includeDeleted bool) (map[uint]string, error) {
	// preallocate the result map at the upper bound; ids that fail
	// lookup simply won't appear in it
	out := make(map[uint]string, len(ids))

	// when the caller wants soft-deleted rows too, ask the module
	// to bypass its delete filter via the shared query param
	suffix := ""
	if includeDeleted {
		suffix = "?" + apiserver_lib.QueryParamIncludeDeleted + "=true"
	}

	// one GET per id; the module API doesn't have a batch by-ids
	// endpoint, so this is a fan-out of N requests
	for _, id := range ids {
		// build the per-id URL.
		// e.g. "http://widget-api.threeport-control-plane:80/v0/widgets/42"
		url := fmt.Sprintf("%s%s/%d%s", endpoint, path, id, suffix)

		// dispatch via the shared module HTTP client
		resp, err := client_lib.GetResponse(
			moduleHTTPClient,
			url,
			http.MethodGet,
			new(bytes.Buffer),
			map[string]string{},
			http.StatusOK,
		)

		// transport or non-200 error - skip this id rather than
		// failing the whole batch; the caller renders id-only for
		// missing names
		if err != nil {
			continue
		}

		// the module response wraps the row in a Data array; pluck
		// the first (and typically only) row as a generic map
		row, ok := getFirstRow(resp)
		if !ok {
			continue
		}

		// only record entries that actually have a non-empty Name;
		// otherwise drop and let the caller fall back to id-only
		if name, ok := row["Name"].(string); ok && name != "" {
			out[id] = name
		}
	}
	return out, nil
}

// getIDFromModuleByName issues a name-filtered GET to the module API and
// returns the first matching row's ID.
func getIDFromModuleByName(endpoint, path, objectType, name string) (uint, error) {
	// build the name-filtered list URL.
	// e.g. "http://widget-api.threeport-control-plane:80/v0/widgets?name=my-widget"
	url := fmt.Sprintf("%s%s?name=%s", endpoint, path, name)

	// dispatch via the shared module HTTP client
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

	// the module response wraps rows in a Data array; get the first
	// (and typically only) one as a generic map
	row, ok := getFirstRow(resp)
	if !ok {
		return 0, fmt.Errorf("no %s found with name %q in module response", objectType, name)
	}

	// extract the ID field. JSON unmarshalling into interface{} gives
	// us float64 by default; if the response was decoded with
	// json.Number (UseNumber), we get json.Number instead. handle both
	// so we don't depend on the caller's decoder settings
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

	// row exists but the ID field is missing or has an unexpected type;
	// treat as "not found" rather than crashing the response
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
