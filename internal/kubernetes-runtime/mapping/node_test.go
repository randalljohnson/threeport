package mapping

import (
	"errors"
	"strings"
	"testing"
)

// TestMachineTypeError_Error asserts the MachineTypeError returns its stored message verbatim.
func TestMachineTypeError_Error(t *testing.T) {
	// build error with a known message
	e := &MachineTypeError{Message: "node size Small not supported"}

	// assert the Error method returns the stored text unchanged
	if got := e.Error(); got != "node size Small not supported" {
		t.Fatalf("expected message passthrough, got %q", got)
	}

	// assert satisfies the error interface via errors.As
	var target *MachineTypeError
	if !errors.As(error(e), &target) {
		t.Fatalf("expected errors.As to match *MachineTypeError")
	}
}

// TestGetMachineTypeMap_ReturnsPopulatedMap asserts the map is non-empty and every entry has required fields set.
func TestGetMachineTypeMap_ReturnsPopulatedMap(t *testing.T) {
	// fetch the map
	m := GetMachineTypeMap()

	// assert non-nil and non-empty
	if m == nil {
		t.Fatalf("expected non-nil map pointer")
	}
	if len(*m) == 0 {
		t.Fatalf("expected non-empty map")
	}

	// assert every entry has non-empty required fields
	for i, entry := range *m {
		if entry.NodeProfile == "" {
			t.Errorf("entry %d has empty NodeProfile", i)
		}
		if entry.NodeSize == "" {
			t.Errorf("entry %d (%s) has empty NodeSize", i, entry.NodeProfile)
		}
		if entry.AwsMachineType == "" {
			t.Errorf("entry %d (%s/%s) has empty AwsMachineType", i, entry.NodeProfile, entry.NodeSize)
		}
		if entry.OciMachineType == "" {
			t.Errorf("entry %d (%s/%s) has empty OciMachineType", i, entry.NodeProfile, entry.NodeSize)
		}
		if entry.GcpMachineType == "" {
			t.Errorf("entry %d (%s/%s) has empty GcpMachineType", i, entry.NodeProfile, entry.NodeSize)
		}
	}
}

// TestGetMachineType_ReturnsProviderMachineType covers the happy-path provider/profile/size lookup for each supported provider.
func TestGetMachineType_ReturnsProviderMachineType(t *testing.T) {
	tests := []struct {
		name        string
		provider    string
		nodeProfile string
		nodeSize    string
		want        string
	}{
		{"aws balanced small", "aws", "Balanced", "Small", "t3.small"},
		{"oci balanced small", "oci", "Balanced", "Small", "VM.Standard3.Flex"},
		{"gcp balanced small", "gcp", "Balanced", "Small", "e2-medium"},
		{"aws compute-optimized medium", "aws", "ComputeOptimized", "Medium", "c8g.large"},
		{"gcp memory-optimized xlarge", "gcp", "MemoryOptimized", "XLarge", "n2-highmem-8"},
		{"aws balanced 2xsmall boundary first", "aws", "Balanced", "2XSmall", "t3.nano"},
		{"gcp memory-optimized 7xlarge boundary last", "gcp", "MemoryOptimized", "7XLarge", "n2-highmem-128"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// invoke happy-path lookup
			got, err := GetMachineType(tc.provider, tc.nodeProfile, tc.nodeSize)

			// assert no error and returned machine type matches expected
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestGetMachineType_UnsupportedProvider asserts an unknown provider yields a ProviderError.
func TestGetMachineType_UnsupportedProvider(t *testing.T) {
	// lookup with a valid profile/size but bogus provider
	got, err := GetMachineType("azure", "Balanced", "Small")

	// assert empty result plus ProviderError type
	if got != "" {
		t.Errorf("expected empty machine type, got %q", got)
	}
	var perr *ProviderError
	if !errors.As(err, &perr) {
		t.Fatalf("expected *ProviderError, got %T (%v)", err, err)
	}
	if !strings.Contains(perr.Error(), "azure") {
		t.Errorf("expected provider name in message, got %q", perr.Error())
	}
}

// TestGetMachineType_UnsupportedNodeSize asserts a valid profile with unknown size yields a MachineTypeError.
func TestGetMachineType_UnsupportedNodeSize(t *testing.T) {
	// lookup with a valid profile and provider but bogus size
	got, err := GetMachineType("aws", "Balanced", "Ginormous")

	// assert empty result and MachineTypeError
	if got != "" {
		t.Errorf("expected empty machine type, got %q", got)
	}
	var merr *MachineTypeError
	if !errors.As(err, &merr) {
		t.Fatalf("expected *MachineTypeError, got %T (%v)", err, err)
	}
	msg := merr.Error()
	if !strings.Contains(msg, "Ginormous") || !strings.Contains(msg, "Balanced") {
		t.Errorf("expected size and profile in message, got %q", msg)
	}
}

// TestGetMachineType_UnsupportedProfile asserts an unknown profile yields the profile-not-supported error.
func TestGetMachineType_UnsupportedProfile(t *testing.T) {
	// lookup with a bogus profile
	got, err := GetMachineType("aws", "SuperOptimized", "Small")

	// assert empty result and MachineTypeError naming the profile
	if got != "" {
		t.Errorf("expected empty machine type, got %q", got)
	}
	var merr *MachineTypeError
	if !errors.As(err, &merr) {
		t.Fatalf("expected *MachineTypeError, got %T (%v)", err, err)
	}
	if !strings.Contains(merr.Error(), "SuperOptimized") {
		t.Errorf("expected profile name in message, got %q", merr.Error())
	}
}

// TestGetMachineType_EmptyInputs covers the empty-string boundary on each argument.
func TestGetMachineType_EmptyInputs(t *testing.T) {
	tests := []struct {
		name        string
		provider    string
		nodeProfile string
		nodeSize    string
	}{
		{"empty provider", "", "Balanced", "Small"},
		{"empty profile", "aws", "", "Small"},
		{"empty size", "aws", "Balanced", ""},
		{"all empty", "", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// call with empty arg(s)
			got, err := GetMachineType(tc.provider, tc.nodeProfile, tc.nodeSize)

			// assert no successful result on empty inputs
			if err == nil {
				t.Fatalf("expected error, got machine type %q", got)
			}
			if got != "" {
				t.Errorf("expected empty machine type, got %q", got)
			}
		})
	}
}

// TestGetNodeSizeForProfile_ReturnsSizes asserts a valid profile returns the full list of sizes defined for it.
func TestGetNodeSizeForProfile_ReturnsSizes(t *testing.T) {
	tests := []struct {
		name    string
		profile string
	}{
		{"balanced", "Balanced"},
		{"compute optimized", "ComputeOptimized"},
		{"memory optimized", "MemoryOptimized"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// call the sizes lookup
			sizes, err := GetNodeSizeForProfile(tc.profile)

			// assert no error and non-empty result
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(sizes) == 0 {
				t.Fatalf("expected at least one size for profile %q", tc.profile)
			}

			// assert the count matches the underlying map entries for the profile
			var want int
			for _, m := range *GetMachineTypeMap() {
				if m.NodeProfile == tc.profile {
					want++
				}
			}
			if len(sizes) != want {
				t.Errorf("got %d sizes, want %d", len(sizes), want)
			}
		})
	}
}

// TestGetNodeSizeForProfile_UnsupportedProfile asserts an unknown profile yields a MachineTypeError and empty slice.
func TestGetNodeSizeForProfile_UnsupportedProfile(t *testing.T) {
	// call with a bogus profile
	sizes, err := GetNodeSizeForProfile("Nonsense")

	// assert empty slice and MachineTypeError naming the profile plus supported list
	if len(sizes) != 0 {
		t.Errorf("expected empty slice, got %v", sizes)
	}
	var merr *MachineTypeError
	if !errors.As(err, &merr) {
		t.Fatalf("expected *MachineTypeError, got %T (%v)", err, err)
	}
	msg := merr.Error()
	if !strings.Contains(msg, "Nonsense") {
		t.Errorf("expected profile name in message, got %q", msg)
	}
	if !strings.Contains(msg, "Balanced") {
		t.Errorf("expected supported-profile list in message, got %q", msg)
	}
}

// TestGetNodeSizeForProfile_EmptyProfile asserts the empty-string profile is treated as unsupported.
func TestGetNodeSizeForProfile_EmptyProfile(t *testing.T) {
	// call with empty profile
	sizes, err := GetNodeSizeForProfile("")

	// assert empty slice and MachineTypeError
	if len(sizes) != 0 {
		t.Errorf("expected empty slice, got %v", sizes)
	}
	var merr *MachineTypeError
	if !errors.As(err, &merr) {
		t.Fatalf("expected *MachineTypeError, got %T (%v)", err, err)
	}
}

// TestGetNodeProfiles_ReturnsUniqueProfiles asserts all supported profiles appear exactly once.
func TestGetNodeProfiles_ReturnsUniqueProfiles(t *testing.T) {
	// fetch profile list
	profiles := GetNodeProfiles()

	// assert non-empty and contains the three known profiles
	if len(profiles) == 0 {
		t.Fatalf("expected non-empty profile list")
	}
	expected := map[string]bool{"Balanced": true, "ComputeOptimized": true, "MemoryOptimized": true}
	seen := make(map[string]int)
	for _, p := range profiles {
		seen[p]++
	}
	for name := range expected {
		if seen[name] == 0 {
			t.Errorf("expected profile %q to appear", name)
		}
	}

	// assert uniqueness: every profile appears exactly once
	for name, count := range seen {
		if count != 1 {
			t.Errorf("profile %q appeared %d times, want 1", name, count)
		}
	}
}
