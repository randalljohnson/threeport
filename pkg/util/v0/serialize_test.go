package v0

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"gorm.io/datatypes"
)

// TestMarshalObject_CoversHappyPathAndError asserts MarshalObject returns
// the JSON encoding of the input and wraps encoder errors.
func TestMarshalObject_CoversHappyPathAndError(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		want    string
		wantErr bool
	}{
		{
			name:  "encodes struct to json",
			input: struct{ Name string }{Name: "alice"},
			want:  `{"Name":"alice"}`,
		},
		{
			name:  "encodes map to json",
			input: map[string]int{"a": 1},
			want:  `{"a":1}`,
		},
		{
			name:  "encodes nil to json null",
			input: nil,
			want:  `null`,
		},
		{
			name:    "rejects unsupported channel type",
			input:   make(chan int),
			wantErr: true,
		},
		{
			name:    "rejects unsupported float value",
			input:   math.NaN(),
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// invoke MarshalObject on the case input
			got, err := MarshalObject(tc.input)

			// assert error expectation
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !strings.Contains(err.Error(), "failed to marshal object to JSON") {
					t.Fatalf("expected wrapped marshal error, got %v", err)
				}
				return
			}

			// assert the byte payload matches the expected json
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("got %q, want %q", string(got), tc.want)
			}
		})
	}
}

// TestMarshalJSON_ProducesDatatypesJSON asserts MarshalJSON returns a
// datatypes.JSON round-trippable value for a given map.
func TestMarshalJSON_ProducesDatatypesJSON(t *testing.T) {
	// build a representative input map
	input := map[string]interface{}{
		"name":    "widget",
		"count":   float64(3),
		"enabled": true,
	}

	// invoke MarshalJSON on the map
	got, err := MarshalJSON(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// assert the returned value round-trips back to the same map
	var round map[string]interface{}
	if err := json.Unmarshal([]byte(got), &round); err != nil {
		t.Fatalf("returned bytes are not valid json: %v", err)
	}
	if round["name"] != "widget" || round["count"] != float64(3) || round["enabled"] != true {
		t.Fatalf("round trip mismatch: %#v", round)
	}
}

// TestMarshalJSON_HandlesNilMap asserts MarshalJSON serializes a nil map to
// the JSON null literal without error.
func TestMarshalJSON_HandlesNilMap(t *testing.T) {
	// pass a nil map to MarshalJSON
	got, err := MarshalJSON(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// assert the payload is the json null literal
	if string(got) != "null" {
		t.Fatalf("got %q, want null", string(got))
	}
}

// TestMarshalJSON_RejectsUnmarshalableValue asserts MarshalJSON returns a
// wrapped error when json.Marshal cannot encode a map entry.
func TestMarshalJSON_RejectsUnmarshalableValue(t *testing.T) {
	// build a map with an unsupported channel value
	input := map[string]interface{}{"chan": make(chan int)}

	// invoke MarshalJSON and expect a wrapped marshal error
	_, err := MarshalJSON(input)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to marshal json") {
		t.Fatalf("expected wrapped marshal error, got %v", err)
	}
}

// TestUnmarshalJSON_CoversHappyPathAndError asserts UnmarshalJSON decodes a
// datatypes.JSON payload into a map and wraps decode errors.
func TestUnmarshalJSON_CoversHappyPathAndError(t *testing.T) {
	tests := []struct {
		name    string
		input   datatypes.JSON
		want    map[string]interface{}
		wantErr bool
	}{
		{
			name:  "decodes simple object",
			input: datatypes.JSON([]byte(`{"a":1,"b":"two"}`)),
			want:  map[string]interface{}{"a": float64(1), "b": "two"},
		},
		{
			name:  "decodes empty object",
			input: datatypes.JSON([]byte(`{}`)),
			want:  map[string]interface{}{},
		},
		{
			name:    "rejects malformed json",
			input:   datatypes.JSON([]byte(`{`)),
			wantErr: true,
		},
		{
			name:    "rejects empty payload",
			input:   datatypes.JSON([]byte(``)),
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// invoke UnmarshalJSON on the case input
			got, err := UnmarshalJSON(tc.input)

			// assert error expectation
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !strings.Contains(err.Error(), "failed to unmarshal json") {
					t.Fatalf("expected wrapped unmarshal error, got %v", err)
				}
				return
			}

			// assert the decoded map matches the expected value
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Fatalf("key %q got %#v, want %#v", k, got[k], v)
				}
			}
		})
	}
}

// TestUnmarshalYAML_CoversHappyPathAndError asserts UnmarshalYAML parses a
// YAML string into a map and reports a parse error on malformed input.
func TestUnmarshalYAML_CoversHappyPathAndError(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		check   func(t *testing.T, got map[string]interface{})
		wantErr bool
	}{
		{
			name:  "decodes simple mapping",
			input: "name: widget\ncount: 3\n",
			check: func(t *testing.T, got map[string]interface{}) {
				// verify each key decoded to the expected typed value
				if got["name"] != "widget" {
					t.Fatalf("name got %#v", got["name"])
				}
				if got["count"] != float64(3) {
					t.Fatalf("count got %#v", got["count"])
				}
			},
		},
		{
			name:  "decodes empty document to nil map",
			input: "",
			check: func(t *testing.T, got map[string]interface{}) {
				// an empty document decodes to a nil map
				if got != nil {
					t.Fatalf("expected nil map, got %#v", got)
				}
			},
		},
		{
			name:    "rejects malformed yaml",
			input:   "name: : bad",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// invoke UnmarshalYAML on the case input
			got, err := UnmarshalYAML(tc.input)

			// assert error expectation
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !strings.Contains(err.Error(), "error parsing YAML") {
					t.Fatalf("expected wrapped parse error, got %v", err)
				}
				return
			}

			// delegate value assertions to the case check function
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			tc.check(t, got)
		})
	}
}
