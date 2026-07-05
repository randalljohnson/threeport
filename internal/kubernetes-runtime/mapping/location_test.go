package mapping

import (
	"errors"
	"testing"

	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestProviderError_Error asserts the ProviderError returns its stored message verbatim.
func TestProviderError_Error(t *testing.T) {
	// build error with a known message
	e := &ProviderError{Message: "provider foo not supported"}

	// assert the Error method returns the stored text unchanged
	if got := e.Error(); got != "provider foo not supported" {
		t.Fatalf("expected message passthrough, got %q", got)
	}

	// assert satisfies the error interface via errors.As
	var target *ProviderError
	if !errors.As(error(e), &target) {
		t.Fatalf("expected errors.As to match *ProviderError")
	}
}

// TestLocationError_Error asserts the LocationError returns its stored message verbatim.
func TestLocationError_Error(t *testing.T) {
	// build error with a known message
	e := &LocationError{Message: "location foo not supported"}

	// assert the Error method returns the stored text unchanged
	if got := e.Error(); got != "location foo not supported" {
		t.Fatalf("expected message passthrough, got %q", got)
	}

	// assert satisfies the error interface via errors.As
	var target *LocationError
	if !errors.As(error(e), &target) {
		t.Fatalf("expected errors.As to match *LocationError")
	}
}

// TestRegionError_Error asserts the RegionError returns its stored message verbatim.
func TestRegionError_Error(t *testing.T) {
	// build error with a known message
	e := &RegionError{Message: "region foo not supported"}

	// assert the Error method returns the stored text unchanged
	if got := e.Error(); got != "region foo not supported" {
		t.Fatalf("expected message passthrough, got %q", got)
	}

	// assert satisfies the error interface via errors.As
	var target *RegionError
	if !errors.As(error(e), &target) {
		t.Fatalf("expected errors.As to match *RegionError")
	}
}

// TestGetRegionMap covers that the map is non-empty, non-nil, and every entry
// has all four fields populated (no zero-valued locations or regions).
func TestGetRegionMap(t *testing.T) {
	// fetch the region map
	m := GetRegionMap()

	// assert non-nil
	if m == nil {
		t.Fatalf("expected non-nil map pointer")
	}

	// assert non-empty
	if len(*m) == 0 {
		t.Fatalf("expected non-empty region map")
	}

	// assert every entry has each field populated and locations are unique
	seen := make(map[string]bool)
	for i, rm := range *m {
		if rm.Location == "" {
			t.Errorf("entry %d: empty Location", i)
		}
		if rm.AwsRegion == "" {
			t.Errorf("entry %d (%s): empty AwsRegion", i, rm.Location)
		}
		if rm.OciRegion == "" {
			t.Errorf("entry %d (%s): empty OciRegion", i, rm.Location)
		}
		if rm.GcpRegion == "" {
			t.Errorf("entry %d (%s): empty GcpRegion", i, rm.Location)
		}
		if seen[rm.Location] {
			t.Errorf("entry %d: duplicate location %q", i, rm.Location)
		}
		seen[rm.Location] = true
	}
}

// TestValidLocation covers the happy path (known location), the negative path
// (unknown location), and the empty-string boundary.
func TestValidLocation(t *testing.T) {
	tests := []struct {
		name     string
		location string
		want     bool
	}{
		{name: "known location Local", location: "Local", want: true},
		{name: "known location NorthAmerica:NewYork", location: "NorthAmerica:NewYork", want: true},
		{name: "known location Europe:London", location: "Europe:London", want: true},
		{name: "known location Africa:Johannesburg", location: "Africa:Johannesburg", want: true},
		{name: "unknown location", location: "Mars:Olympus", want: false},
		{name: "empty string", location: "", want: false},
		{name: "case-sensitive miss", location: "local", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// invoke and compare against expected boolean
			if got := ValidLocation(tt.location); got != tt.want {
				t.Errorf("ValidLocation(%q) = %v, want %v", tt.location, got, tt.want)
			}
		})
	}
}

// TestGetProviderRegionForLocation covers happy paths for each supported
// provider, the unsupported-provider error, and the unsupported-location error.
func TestGetProviderRegionForLocation(t *testing.T) {
	tests := []struct {
		name       string
		provider   string
		location   string
		wantRegion string
		wantErrAs  interface{}
	}{
		{
			name:       "aws happy path",
			provider:   util.AwsProvider,
			location:   "NorthAmerica:NewYork",
			wantRegion: "us-east-1",
		},
		{
			name:       "oci happy path",
			provider:   util.OciProvider,
			location:   "NorthAmerica:NewYork",
			wantRegion: "us-ashburn-1",
		},
		{
			name:       "gcp happy path",
			provider:   util.GcpProvider,
			location:   "NorthAmerica:NewYork",
			wantRegion: "us-east4",
		},
		{
			name:       "aws for Local",
			provider:   util.AwsProvider,
			location:   "Local",
			wantRegion: "us-east-1",
		},
		{
			name:      "unsupported provider on known location",
			provider:  "azure",
			location:  "NorthAmerica:NewYork",
			wantErrAs: &ProviderError{},
		},
		{
			name:      "unknown location",
			provider:  util.AwsProvider,
			location:  "Mars:Olympus",
			wantErrAs: &LocationError{},
		},
		{
			name:      "empty location",
			provider:  util.AwsProvider,
			location:  "",
			wantErrAs: &LocationError{},
		},
		{
			name:      "empty provider on known location",
			provider:  "",
			location:  "NorthAmerica:NewYork",
			wantErrAs: &ProviderError{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// invoke the function under test
			region, err := GetProviderRegionForLocation(tt.provider, tt.location)

			// assert error path
			if tt.wantErrAs != nil {
				if err == nil {
					t.Fatalf("expected error, got nil (region=%q)", region)
				}
				if region != "" {
					t.Errorf("expected empty region on error, got %q", region)
				}
				switch tt.wantErrAs.(type) {
				case *ProviderError:
					var pe *ProviderError
					if !errors.As(err, &pe) {
						t.Errorf("expected *ProviderError, got %T: %v", err, err)
					}
				case *LocationError:
					var le *LocationError
					if !errors.As(err, &le) {
						t.Errorf("expected *LocationError, got %T: %v", err, err)
					}
				}
				return
			}

			// assert happy path
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if region != tt.wantRegion {
				t.Errorf("expected region %q, got %q", tt.wantRegion, region)
			}
		})
	}
}

// TestGetLocationForAwsRegion covers the happy path (first-match returned) and
// the unsupported-region error.
func TestGetLocationForAwsRegion(t *testing.T) {
	tests := []struct {
		name      string
		awsRegion string
		wantLoc   string
		wantErr   bool
	}{
		// us-east-1 is shared by Local and NorthAmerica:NewYork; the first
		// entry (Local) wins because the loop returns on first match.
		{name: "first-match wins for shared aws region", awsRegion: "us-east-1", wantLoc: "Local"},
		{name: "unique aws region", awsRegion: "eu-west-3", wantLoc: "Europe:Paris"},
		{name: "unknown region", awsRegion: "no-such-region", wantErr: true},
		{name: "empty region", awsRegion: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// invoke the function under test
			loc, err := GetLocationForAwsRegion(tt.awsRegion)

			// assert error path
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (loc=%q)", loc)
				}
				if loc != "" {
					t.Errorf("expected empty location on error, got %q", loc)
				}
				var re *RegionError
				if !errors.As(err, &re) {
					t.Errorf("expected *RegionError, got %T", err)
				}
				return
			}

			// assert happy path
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if loc != tt.wantLoc {
				t.Errorf("expected location %q, got %q", tt.wantLoc, loc)
			}
		})
	}
}

// TestGetLocationForOciRegion covers the happy path (first-match returned) and
// the unsupported-region error.
func TestGetLocationForOciRegion(t *testing.T) {
	tests := []struct {
		name      string
		ociRegion string
		wantLoc   string
		wantErr   bool
	}{
		// us-ashburn-1 is shared; Local is the first entry in the map.
		{name: "first-match wins for shared oci region", ociRegion: "us-ashburn-1", wantLoc: "Local"},
		{name: "unique oci region", ociRegion: "eu-paris-1", wantLoc: "Europe:Paris"},
		{name: "unknown region", ociRegion: "no-such-region", wantErr: true},
		{name: "empty region", ociRegion: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// invoke the function under test
			loc, err := GetLocationForOciRegion(tt.ociRegion)

			// assert error path
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (loc=%q)", loc)
				}
				if loc != "" {
					t.Errorf("expected empty location on error, got %q", loc)
				}
				var re *RegionError
				if !errors.As(err, &re) {
					t.Errorf("expected *RegionError, got %T", err)
				}
				return
			}

			// assert happy path
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if loc != tt.wantLoc {
				t.Errorf("expected location %q, got %q", tt.wantLoc, loc)
			}
		})
	}
}

// TestGetLocationForGcpRegion covers the happy path (first-match returned) and
// the unsupported-region error.
func TestGetLocationForGcpRegion(t *testing.T) {
	tests := []struct {
		name      string
		gcpRegion string
		wantLoc   string
		wantErr   bool
	}{
		// us-east1 is shared by Local and NorthAmerica:Atlanta; Local wins.
		{name: "first-match wins for shared gcp region", gcpRegion: "us-east1", wantLoc: "Local"},
		{name: "unique gcp region", gcpRegion: "europe-west9", wantLoc: "Europe:Paris"},
		{name: "unknown region", gcpRegion: "no-such-region", wantErr: true},
		{name: "empty region", gcpRegion: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// invoke the function under test
			loc, err := GetLocationForGcpRegion(tt.gcpRegion)

			// assert error path
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (loc=%q)", loc)
				}
				if loc != "" {
					t.Errorf("expected empty location on error, got %q", loc)
				}
				var re *RegionError
				if !errors.As(err, &re) {
					t.Errorf("expected *RegionError, got %T", err)
				}
				return
			}

			// assert happy path
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if loc != tt.wantLoc {
				t.Errorf("expected location %q, got %q", tt.wantLoc, loc)
			}
		})
	}
}
