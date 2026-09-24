package docs

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/swaggo/swag"
)

// TestSwaggerInfoFieldsExported asserts the package-level SwaggerInfo
// carries the constant metadata the swagger endpoint serves, so a
// regenerate that silently drops one is caught.
func TestSwaggerInfoFieldsExported(t *testing.T) {
	// generated spec is registered at package init and must expose it
	require.NotNil(t, SwaggerInfo)

	// title and description name the threeport API and its purpose
	assert.Equal(t, "Threeport RESTful API", SwaggerInfo.Title)
	assert.Contains(t, SwaggerInfo.Description, "Threeport")

	// basePath, delimiters, and instance name are the swaggo defaults
	// the ReadDoc template renderer relies on
	assert.Equal(t, "/", SwaggerInfo.BasePath)
	assert.Equal(t, "{{", SwaggerInfo.LeftDelim)
	assert.Equal(t, "}}", SwaggerInfo.RightDelim)
	assert.Equal(t, "swagger", SwaggerInfo.InfoInstanceName)

	// template body is the raw doc string the init hook registers
	assert.NotEmpty(t, SwaggerInfo.SwaggerTemplate)
}

// TestSwaggerInfoInstanceName asserts the Spec exposes its
// InfoInstanceName through the Swagger interface method used to
// look the spec up in the swag registry.
func TestSwaggerInfoInstanceName(t *testing.T) {
	// InstanceName mirrors the struct field verbatim
	assert.Equal(t, "swagger", SwaggerInfo.InstanceName())
}

// TestSwaggerInfoReadDocRendersJSON asserts the template renders to a
// swagger 2.0 JSON document with the expected info block, so a broken
// template does not silently ship an empty spec.
func TestSwaggerInfoReadDocRendersJSON(t *testing.T) {
	// render the template through the swag pipeline
	rendered := SwaggerInfo.ReadDoc()
	require.NotEmpty(t, rendered)

	// output must parse as JSON so consumers do not choke
	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(rendered), &parsed))

	// swagger version marker anchors the spec at 2.0
	assert.Equal(t, "2.0", parsed["swagger"])

	// info block carries the title we asserted on the raw spec
	info, ok := parsed["info"].(map[string]interface{})
	require.True(t, ok, "info block must be a JSON object")
	assert.Equal(t, "Threeport RESTful API", info["title"])
}

// TestInitRegistersSpec asserts the package init hook registered the
// SwaggerInfo under its instance name so swag.ReadDoc can look it up.
func TestInitRegistersSpec(t *testing.T) {
	// ReadDoc resolves the registered instance by name
	doc, err := swag.ReadDoc("swagger")
	require.NoError(t, err)

	// registered doc matches what the exported Spec renders
	assert.Equal(t, SwaggerInfo.ReadDoc(), doc)
}

// TestSwaggerJsonEmbedded asserts the embedded swagger.json content is
// non-empty and parses as a swagger 2.0 spec, so a missing file or a
// broken embed directive is caught at test time.
func TestSwaggerJsonEmbedded(t *testing.T) {
	// embed directive must populate the string at package load
	require.NotEmpty(t, SwaggerJson)

	// content parses as JSON and reports swagger 2.0
	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(SwaggerJson), &parsed))
	assert.Equal(t, "2.0", parsed["swagger"])

	// paths block is present so the spec describes at least one route
	paths, ok := parsed["paths"].(map[string]interface{})
	require.True(t, ok, "paths block must be a JSON object")
	assert.NotEmpty(t, paths)
}

// TestDocTemplateShape asserts the raw docTemplate holds the swaggo
// template delimiters and swagger 2.0 marker, guarding against a
// regeneration that drops the template body.
func TestDocTemplateShape(t *testing.T) {
	// template opens with a JSON object and carries the swagger marker
	assert.True(t, strings.HasPrefix(docTemplate, "{"))
	assert.Contains(t, docTemplate, `"swagger": "2.0"`)

	// template placeholders use the swaggo default delimiters
	assert.Contains(t, docTemplate, "{{.Title}}")
	assert.Contains(t, docTemplate, "{{.BasePath}}")
}
