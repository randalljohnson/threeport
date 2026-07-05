package v0

import "testing"

// TestDefinitionZeroValue asserts a fresh Definition has nil pointer fields
// so callers can distinguish unset from set state.
func TestDefinitionZeroValue(t *testing.T) {
	// build the zero value
	var d Definition

	// each pointer field must be nil in the zero value
	if d.Name != nil {
		t.Errorf("zero-value Name: want nil, got %v", d.Name)
	}
	if d.ProfileID != nil {
		t.Errorf("zero-value ProfileID: want nil, got %v", d.ProfileID)
	}
	if d.TierID != nil {
		t.Errorf("zero-value TierID: want nil, got %v", d.TierID)
	}
}

// TestDefinitionFieldAssignment covers assigning each field and reading it back
// through the pointer for the Name, ProfileID, and TierID fields.
func TestDefinitionFieldAssignment(t *testing.T) {
	name := "example"
	var profileID uint = 7
	var tierID uint = 42

	// populate every field on the struct
	d := Definition{
		Name:      &name,
		ProfileID: &profileID,
		TierID:    &tierID,
	}

	// each dereferenced field should match the source value
	if got := *d.Name; got != name {
		t.Errorf("Name: want %q, got %q", name, got)
	}
	if got := *d.ProfileID; got != profileID {
		t.Errorf("ProfileID: want %d, got %d", profileID, got)
	}
	if got := *d.TierID; got != tierID {
		t.Errorf("TierID: want %d, got %d", tierID, got)
	}
}

// TestInstanceZeroValue asserts a fresh Instance has nil pointer fields.
func TestInstanceZeroValue(t *testing.T) {
	// build the zero value
	var i Instance

	// each pointer field must be nil in the zero value
	if i.Name != nil {
		t.Errorf("zero-value Name: want nil, got %v", i.Name)
	}
	if i.Status != nil {
		t.Errorf("zero-value Status: want nil, got %v", i.Status)
	}
}

// TestInstanceFieldAssignment covers assigning Name and Status on an Instance.
func TestInstanceFieldAssignment(t *testing.T) {
	cases := []struct {
		name       string
		nameVal    string
		statusVal  string
		wantName   string
		wantStatus string
	}{
		{
			name:       "typical values",
			nameVal:    "worker-1",
			statusVal:  "Healthy",
			wantName:   "worker-1",
			wantStatus: "Healthy",
		},
		{
			name:       "empty strings still round-trip",
			nameVal:    "",
			statusVal:  "",
			wantName:   "",
			wantStatus: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// populate the instance under test
			n := tc.nameVal
			s := tc.statusVal
			i := Instance{Name: &n, Status: &s}

			// each dereferenced field should match the source value
			if got := *i.Name; got != tc.wantName {
				t.Errorf("Name: want %q, got %q", tc.wantName, got)
			}
			if got := *i.Status; got != tc.wantStatus {
				t.Errorf("Status: want %q, got %q", tc.wantStatus, got)
			}
		})
	}
}
