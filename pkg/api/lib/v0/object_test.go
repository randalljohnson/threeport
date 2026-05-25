package v0

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestParseQualifiedType walks every malformed shape the parser is
// meant to reject. The parser underpins cross-module identity, so a
// regression here surfaces as silently empty lookups rather than
// loud failures.
func TestParseQualifiedType(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		namespace string
		version   string
		typeName  string
		ok        bool
	}{
		// happy paths
		{
			name:      "core namespace",
			input:     "threeport.io/v0.Widget",
			namespace: "threeport.io",
			version:   "v0",
			typeName:  "Widget",
			ok:        true,
		},
		{
			name:      "module namespace",
			input:     "example.com/v1.Gadget",
			namespace: "example.com",
			version:   "v1",
			typeName:  "Gadget",
			ok:        true,
		},
		{
			name:      "type name contains additional dots",
			input:     "example.com/v0.Foo.Bar",
			namespace: "example.com",
			version:   "v0",
			typeName:  "Foo.Bar",
			ok:        true,
		},

		// malformed: missing or boundary slash
		{name: "no slash", input: "threeport.io-v0.Widget"},
		{name: "leading slash", input: "/v0.Widget"},
		{name: "trailing slash", input: "threeport.io/"},
		{name: "empty input", input: ""},

		// malformed: missing or boundary dot in the post-slash segment
		{name: "no dot after slash", input: "threeport.io/v0Widget"},
		{name: "leading dot after slash", input: "threeport.io/.Widget"},
		{name: "trailing dot after slash", input: "threeport.io/v0."},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			namespace, version, typeName, ok := ParseQualifiedType(tc.input)
			assert.Equal(t, tc.ok, ok, "ok mismatch")
			assert.Equal(t, tc.namespace, namespace, "namespace mismatch")
			assert.Equal(t, tc.version, version, "version mismatch")
			assert.Equal(t, tc.typeName, typeName, "typeName mismatch")
		})
	}
}
