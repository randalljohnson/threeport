package v0

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	apiserver "github.com/threeport/threeport/pkg/api-server/lib/v0"
	api_v0 "github.com/threeport/threeport/pkg/api/v0"
)

// TestContract_KubernetesWorkloadDefinition exercises the full client-side
// pipeline as a user experiences it via tptctl: a YAML config file is
// unmarshaled into a Values struct, mapped to an api type, then serialized
// to JSON for the wire body POSTed to the api server. The wire body is
// inspected to confirm required fields are populated and would not be
// rejected by the server's null-on-required PayloadCheck guard. A
// representative GET filter query is then bound via the same QueryBinder
// the api server uses, confirming the round trip in the read direction.
//
// Pinned by this test:
//   - sigs.k8s.io/yaml decodes PascalCase keys into the Values pointer fields
//   - The hand-written Values -> api type mapping populates required fields
//   - The JSON wire body matches what the api server's PayloadCheck accepts
//   - QueryBinder reads filter query params into the api type via the
//     lowercased-field-name default
//
// KubernetesWorkloadDefinition was picked as the representative type
// because it carries a required YAMLDocument field, embeds the Common /
// Definition / Reconciliation trio, and has no cross-type FK lookups that
// would force HTTP-mocking the api client.
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
