package v0

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"unicode"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	apiserver "github.com/threeport/threeport/pkg/api-server/lib/v0"
	api_v0 "github.com/threeport/threeport/pkg/api/v0"
)

// TestContract_KubernetesWorkloadDefinition exercises the full client
// pipeline: a YAML config is unmarshaled into a Values struct, mapped to
// an api type, serialized to JSON, and inspected to confirm the wire
// body meets the api server's expectations. A representative GET filter
// query is then bound via the same QueryBinder the api server uses.
//
// Pinned by this test:
//   - YAML PascalCase keys decode into Values pointer fields
//   - The Values -> api type mapping populates required fields
//   - The JSON wire body is shaped the way the api server expects
//   - QueryBinder reads filter query params into the api type
func TestContract_KubernetesWorkloadDefinition(t *testing.T) {
	// 1. YAML config — the shape a user writes
	yamlInput := []byte(`KubernetesWorkloadDefinition:
  Name: my-workload
  YAMLDocument: |
    apiVersion: v1
    kind: ConfigMap
`)

	// 2. unmarshal into Values via sigs.k8s.io/yaml (encoding/json under the hood)
	var config KubernetesWorkloadDefinitionConfig
	require.NoError(t, yaml.Unmarshal(yamlInput, &config))
	require.NotNil(t, config.KubernetesWorkloadDefinition.Name)
	require.Equal(t, "my-workload", *config.KubernetesWorkloadDefinition.Name)
	require.NotNil(t, config.KubernetesWorkloadDefinition.YAMLDocument)

	// 3. map Values -> api type. mirrors what Create() builds before the HTTP call
	apiObject := api_v0.KubernetesWorkloadDefinition{
		Definition: api_v0.Definition{
			Name: config.KubernetesWorkloadDefinition.Name,
		},
		YAMLDocument: config.KubernetesWorkloadDefinition.YAMLDocument,
	}

	// 4. marshal to JSON — the wire body the api server receives
	body, err := json.Marshal(apiObject)
	require.NoError(t, err)

	// 5. inspect the wire body. required fields must be present and non-null,
	// so the server's PayloadCheck null-on-required guard does not reject
	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &parsed))
	assert.Equal(t, "my-workload", parsed["Name"])
	for _, key := range []string{"YAMLDocument", "Name"} {
		require.NotNil(t, parsed[key], "required field %q must be non-null on the wire", key)
	}

	// 6. QueryBinder picks up GET filter params. exercises the same binder
	// the api server runs against an inbound request
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/?name=my-workload", bytes.NewReader(nil))
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	var filter api_v0.KubernetesWorkloadDefinition
	require.NoError(t, apiserver.NewQueryBinder().Bind(&filter, ctx))
	require.NotNil(t, filter.Name)
	assert.Equal(t, "my-workload", *filter.Name)
}

// TestContract_OmitemptyBehavior verifies nil pointer fields are absent
// from the marshaled output. The api server's PayloadCheck rejects
// bodies that include `null` on validate:"required" fields, so the
// omitempty contract keeps controllers safe: untouched fields don't
// appear on the wire.
func TestContract_OmitemptyBehavior(t *testing.T) {
	// only Name populated; every other pointer field left nil
	name := "my-workload"
	values := KubernetesWorkloadDefinitionValues{
		Name: &name,
	}

	out, err := yaml.Marshal(values)
	require.NoError(t, err)
	rendered := string(out)

	assert.Contains(t, rendered, "Name: my-workload",
		"populated field must appear in output")

	// nil pointer fields must be absent — this is the omitempty contract
	for _, nilField := range []string{"YAMLDocument", "WorkloadConfigPath", "Age"} {
		assert.NotContains(t, rendered, nilField,
			"nil pointer field %q must be omitted (omitempty contract)", nilField)
	}

	assert.NotContains(t, rendered, "null",
		"no field should serialize as null on the wire")
}

// TestContract_PascalCaseKeysPreserved verifies the marshaled wire
// format uses PascalCase keys (Name, YAMLDocument) matching the Go
// field names. This is the wire format the api server's binder expects.
func TestContract_PascalCaseKeysPreserved(t *testing.T) {
	name := "my-workload"
	yamlDoc := "apiVersion: v1\nkind: ConfigMap\n"
	values := KubernetesWorkloadDefinitionValues{
		Name:         &name,
		YAMLDocument: &yamlDoc,
	}

	out, err := yaml.Marshal(values)
	require.NoError(t, err)

	// parse the marshaled output back into a generic map so the key
	// inspection is robust to formatting differences (block vs flow
	// scalars, indentation, etc.)
	var keys map[string]interface{}
	require.NoError(t, yaml.Unmarshal(out, &keys))

	// every top-level key must start with an uppercase letter
	for key := range keys {
		require.NotEmpty(t, key, "key must not be empty")
		assert.True(t, unicode.IsUpper(rune(key[0])),
			"key %q must start with uppercase", key)
	}

	// specific keys we expect on this fixture
	assert.Contains(t, keys, "Name")
	assert.Contains(t, keys, "YAMLDocument")
}

// assertWireConventions runs the json wire conventions over a marshaled
// body: every key starts with an uppercase letter (PascalCase contract),
// and no value serializes as `null` (PayloadCheck null-on-required guard
// would reject the body otherwise). The required-keys list is asserted
// non-nil. The omittedKeys list is asserted absent (the omitempty
// contract: nil pointer fields must not appear).
func assertWireConventions(t *testing.T, body []byte, requiredKeys, omittedKeys []string) {
	t.Helper()

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &parsed))

	// required keys present and non-null
	for _, key := range requiredKeys {
		require.NotNil(t, parsed[key], "required key %q must be present and non-null", key)
	}

	// omitted keys (representing nil pointer fields) must be absent
	for _, key := range omittedKeys {
		assert.NotContains(t, parsed, key,
			"key %q must be absent from the wire body (omitempty contract)", key)
	}

	// PascalCase keys throughout
	for key := range parsed {
		require.NotEmpty(t, key, "key must not be empty")
		assert.True(t, unicode.IsUpper(rune(key[0])),
			"key %q must start with uppercase", key)
	}

	// no `null` anywhere on the wire
	assert.NotContains(t, string(body), "null",
		"no field should serialize as null on the wire")
}

// TestContract_CreateBody_KubernetesWorkloadInstance verifies the JSON
// wire body for POST /v0/kubernetes-workload-instances. Every required
// field (KubernetesRuntimeInstanceID, KubernetesWorkloadDefinitionID,
// Name) is populated; the client doesn't set ID; optional fields left
// nil (like Status) are absent from the wire.
func TestContract_CreateBody_KubernetesWorkloadInstance(t *testing.T) {
	// mirror what KubernetesWorkloadInstanceConfig.Create() builds
	name := "my-workload-instance"
	runtimeID := uint(1)
	definitionID := uint(2)
	apiObject := api_v0.KubernetesWorkloadInstance{
		Instance:                       api_v0.Instance{Name: &name},
		KubernetesRuntimeInstanceID:    &runtimeID,
		KubernetesWorkloadDefinitionID: &definitionID,
	}

	body, err := json.Marshal(apiObject)
	require.NoError(t, err)

	assertWireConventions(t, body,
		[]string{"Name", "KubernetesRuntimeInstanceID", "KubernetesWorkloadDefinitionID"},
		// ID not set by client; Status not set on Create
		[]string{"ID", "Status"},
	)
}

// TestContract_UpdateBody_KubernetesWorkloadInstance verifies the JSON
// wire body for PATCH /v0/kubernetes-workload-instances/<id>. A
// partial update contains only ID and the changed field; untouched
// required fields are absent so the server's PayloadCheck null-on-
// required guard does not reject the request.
func TestContract_UpdateBody_KubernetesWorkloadInstance(t *testing.T) {
	// a controller PATCH-ing just the Status on an existing instance
	id := uint(42)
	status := "Healthy"
	patch := api_v0.KubernetesWorkloadInstance{
		Common: api_v0.Common{ID: &id},
		Status: &status,
	}

	body, err := json.Marshal(patch)
	require.NoError(t, err)

	assertWireConventions(t, body,
		[]string{"ID", "Status"},
		// untouched required FKs and Name must NOT appear; without
		// omitempty they would serialize as null and the server would
		// reject the PATCH
		[]string{"Name", "KubernetesRuntimeInstanceID", "KubernetesWorkloadDefinitionID"},
	)
}

// TestContract_ReplaceBody_KubernetesWorkloadInstance verifies the JSON
// wire body for PUT /v0/kubernetes-workload-instances/<id>. ID and
// every required field are populated.
func TestContract_ReplaceBody_KubernetesWorkloadInstance(t *testing.T) {
	id := uint(42)
	name := "my-workload-instance"
	runtimeID := uint(1)
	definitionID := uint(2)
	apiObject := api_v0.KubernetesWorkloadInstance{
		Common:                         api_v0.Common{ID: &id},
		Instance:                       api_v0.Instance{Name: &name},
		KubernetesRuntimeInstanceID:    &runtimeID,
		KubernetesWorkloadDefinitionID: &definitionID,
	}

	body, err := json.Marshal(apiObject)
	require.NoError(t, err)

	assertWireConventions(t, body,
		[]string{"ID", "Name", "KubernetesRuntimeInstanceID", "KubernetesWorkloadDefinitionID"},
		// Status left nil for this Replace example
		[]string{"Status"},
	)
}

// Delete sends no body — the resource is identified by the URL path.
// The marshaling conventions don't apply, so there's no test for it here.
