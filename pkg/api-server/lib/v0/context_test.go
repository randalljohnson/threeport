package v0

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// payloadCheckTestObject covers the field shapes the helper must
// handle: required pointer by Go name, optional pointer, and required
// pointer carrying a json tag alias.
type payloadCheckTestObject struct {
	ID              *uint
	WorkloadDefID   *uint
	OptionalStatus  *string
	NullableAliased *string `json:"nullable_aliased"`
}

// TestNullValuedRequiredFields exercises the payload shapes the
// helper must distinguish, asserting which trigger rejection.
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
