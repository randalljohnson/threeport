package v0

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// payloadCheckTestObject mirrors a typical threeport api type: required
// pointer FKs without `json:` tags (post tag-normalization) plus an
// optional pointer. NullableOptional is required but uses a `json:`
// alias so the helper's alias-lookup path is exercised too.
type payloadCheckTestObject struct {
	ID              *uint
	WorkloadDefID   *uint
	OptionalStatus  *string
	NullableAliased *string `json:"nullable_aliased"`
}

// TestNullValuedRequiredFields covers the three states the helper
// needs to distinguish on an update payload: a required field with an
// explicit null value (rejected), a required field omitted from the
// payload entirely (allowed - gorm just skips it), and a required
// field whose payload key is the `json:` alias rather than the Go
// field name (rejected, with the Go field name returned).
func TestNullValuedRequiredFields(t *testing.T) {
	required := []string{"WorkloadDefID", "NullableAliased"}
	obj := payloadCheckTestObject{}

	tests := []struct {
		name     string
		payload  map[string]interface{}
		expected []string
	}{
		{
			name:     "required field set to null is rejected",
			payload:  map[string]interface{}{"ID": float64(1), "WorkloadDefID": nil},
			expected: []string{"WorkloadDefID"},
		},
		{
			name:     "required field omitted from payload is allowed",
			payload:  map[string]interface{}{"ID": float64(1), "OptionalStatus": "ok"},
			expected: nil,
		},
		{
			name:     "required field set to a value is allowed",
			payload:  map[string]interface{}{"ID": float64(1), "WorkloadDefID": float64(7)},
			expected: nil,
		},
		{
			name:     "optional field set to null is allowed",
			payload:  map[string]interface{}{"ID": float64(1), "OptionalStatus": nil},
			expected: nil,
		},
		{
			name:     "required field nulled under its json alias is rejected by Go field name",
			payload:  map[string]interface{}{"nullable_aliased": nil},
			expected: []string{"NullableAliased"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := nullValuedRequiredFields(tc.payload, required, obj)
			assert.ElementsMatch(t, tc.expected, got)
		})
	}
}
