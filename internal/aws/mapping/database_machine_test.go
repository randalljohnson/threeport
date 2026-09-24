package mapping

import (
	"errors"
	"testing"
)

// TestProviderError_Error asserts that ProviderError.Error returns the wrapped message verbatim.
func TestProviderError_Error(t *testing.T) {
	// build error with a known message
	msg := "provider foo not supported"
	err := &ProviderError{Message: msg}

	// message returned matches the field
	if got := err.Error(); got != msg {
		t.Fatalf("Error() = %q, want %q", got, msg)
	}

	// verify empty message returns empty string
	empty := &ProviderError{}
	if got := empty.Error(); got != "" {
		t.Fatalf("Error() on empty = %q, want empty string", got)
	}
}

// TestMachineClassError_Error asserts that MachineClassError.Error returns the wrapped message verbatim.
func TestMachineClassError_Error(t *testing.T) {
	msg := "AWS machine class db.foo.bar not supported"
	err := &MachineClassError{Message: msg}

	if got := err.Error(); got != msg {
		t.Fatalf("Error() = %q, want %q", got, msg)
	}
}

// TestMachineSizeError_Error asserts that MachineSizeError.Error returns the wrapped message verbatim.
func TestMachineSizeError_Error(t *testing.T) {
	msg := "machine size Huge not supported"
	err := &MachineSizeError{Message: msg}

	if got := err.Error(); got != msg {
		t.Fatalf("Error() = %q, want %q", got, msg)
	}
}

// TestValidMachineSize covers accepted sizes, rejected inputs, and empty-string boundary.
func TestValidMachineSize(t *testing.T) {
	cases := []struct {
		name        string
		machineSize string
		want        bool
	}{
		// happy path: every mapping entry is accepted
		{"XSmall accepted", "XSmall", true},
		{"Small accepted", "Small", true},
		{"Medium accepted", "Medium", true},
		{"Large accepted", "Large", true},
		{"XLarge accepted", "XLarge", true},
		{"2XLarge accepted", "2XLarge", true},
		{"3XLarge accepted", "3XLarge", true},
		{"4XLarge accepted", "4XLarge", true},
		// rejection: unknown token
		{"unknown rejected", "Huge", false},
		// boundary: case sensitivity
		{"lowercase rejected", "small", false},
		// boundary: empty string
		{"empty rejected", "", false},
		// rejection: AWS class isn't a valid size
		{"aws class rejected", "db.t3.micro", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidMachineSize(tc.machineSize); got != tc.want {
				t.Fatalf("ValidMachineSize(%q) = %v, want %v", tc.machineSize, got, tc.want)
			}
		})
	}
}

// TestGetProviderMachineClassForMachineSize covers the aws happy path, unsupported provider,
// unsupported size, and boundary inputs.
func TestGetProviderMachineClassForMachineSize(t *testing.T) {
	// happy path: aws returns the mapped machine class for each supported size
	awsCases := []struct {
		machineSize string
		want        string
	}{
		{"XSmall", "db.t3.micro"},
		{"Small", "db.t3.small"},
		{"Medium", "db.t3.medium"},
		{"Large", "db.m5.large"},
		{"XLarge", "db.m5.xlarge"},
		{"2XLarge", "db.m5.2xlarge"},
		{"3XLarge", "db.m5.4xlarge"},
		{"4XLarge", "db.m5.8xlarge"},
	}
	for _, tc := range awsCases {
		tc := tc
		t.Run("aws/"+tc.machineSize, func(t *testing.T) {
			got, err := GetProviderMachineClassForMachineSize("aws", tc.machineSize)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}

	// error path: unsupported provider on a supported size returns ProviderError
	t.Run("unsupported provider returns ProviderError", func(t *testing.T) {
		got, err := GetProviderMachineClassForMachineSize("gcp", "Small")
		if got != "" {
			t.Fatalf("got %q, want empty string on error", got)
		}
		var pErr *ProviderError
		if !errors.As(err, &pErr) {
			t.Fatalf("err = %v, want *ProviderError", err)
		}
		if pErr.Message != "provider gcp not supported" {
			t.Fatalf("unexpected message: %q", pErr.Message)
		}
	})

	// error path: unsupported machine size returns MachineSizeError; provider isn't checked
	t.Run("unsupported size returns MachineSizeError", func(t *testing.T) {
		got, err := GetProviderMachineClassForMachineSize("aws", "Huge")
		if got != "" {
			t.Fatalf("got %q, want empty string on error", got)
		}
		var sErr *MachineSizeError
		if !errors.As(err, &sErr) {
			t.Fatalf("err = %v, want *MachineSizeError", err)
		}
		if sErr.Message != "machine size Huge not supported" {
			t.Fatalf("unexpected message: %q", sErr.Message)
		}
	})

	// boundary: empty inputs go through the size-not-found branch (size checked first)
	t.Run("empty size returns MachineSizeError", func(t *testing.T) {
		_, err := GetProviderMachineClassForMachineSize("", "")
		var sErr *MachineSizeError
		if !errors.As(err, &sErr) {
			t.Fatalf("err = %v, want *MachineSizeError", err)
		}
	})

	// boundary: empty provider with a valid size trips the provider-not-supported branch
	t.Run("empty provider with valid size returns ProviderError", func(t *testing.T) {
		_, err := GetProviderMachineClassForMachineSize("", "Small")
		var pErr *ProviderError
		if !errors.As(err, &pErr) {
			t.Fatalf("err = %v, want *ProviderError", err)
		}
	})
}

// TestGetMachineSizeForAwsMachineClass covers reverse lookup happy path and unsupported-class error.
func TestGetMachineSizeForAwsMachineClass(t *testing.T) {
	// happy path: every mapped aws class reverses to its threeport size
	cases := []struct {
		awsClass string
		want     string
	}{
		{"db.t3.micro", "XSmall"},
		{"db.t3.small", "Small"},
		{"db.t3.medium", "Medium"},
		{"db.m5.large", "Large"},
		{"db.m5.xlarge", "XLarge"},
		{"db.m5.2xlarge", "2XLarge"},
		{"db.m5.4xlarge", "3XLarge"},
		{"db.m5.8xlarge", "4XLarge"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.awsClass, func(t *testing.T) {
			got, err := GetMachineSizeForAwsMachineClass(tc.awsClass)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}

	// error path: unknown class returns MachineClassError with the input in the message
	t.Run("unsupported class returns MachineClassError", func(t *testing.T) {
		got, err := GetMachineSizeForAwsMachineClass("db.unknown.class")
		if got != "" {
			t.Fatalf("got %q, want empty string on error", got)
		}
		var cErr *MachineClassError
		if !errors.As(err, &cErr) {
			t.Fatalf("err = %v, want *MachineClassError", err)
		}
		if cErr.Message != "AWS machine class db.unknown.class not supported" {
			t.Fatalf("unexpected message: %q", cErr.Message)
		}
	})

	// boundary: empty string is an unknown class
	t.Run("empty class returns MachineClassError", func(t *testing.T) {
		_, err := GetMachineSizeForAwsMachineClass("")
		var cErr *MachineClassError
		if !errors.As(err, &cErr) {
			t.Fatalf("err = %v, want *MachineClassError", err)
		}
	})

	// boundary: a threeport size name isn't a valid aws class
	t.Run("threeport size not a class", func(t *testing.T) {
		_, err := GetMachineSizeForAwsMachineClass("Small")
		var cErr *MachineClassError
		if !errors.As(err, &cErr) {
			t.Fatalf("err = %v, want *MachineClassError", err)
		}
	})
}
