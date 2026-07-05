package cmd

import (
	"net/url"
	"strings"
	"testing"
)

// TestBuildEventsQueryStringHappyPaths asserts that the three accepted --for
// shapes (kind/name, version.kind/name, namespace/version.kind/name) produce
// the expected query keys and kebab-to-camel conversion of the kind.
func TestBuildEventsQueryStringHappyPaths(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect map[string]string
	}{
		{
			// broad form: only kind and name; no version/namespace
			name:  "kind and name only",
			input: "helm-workload-instance/my-app",
			expect: map[string]string{
				"objectname":     "my-app",
				"objecttypename": "HelmWorkloadInstance",
			},
		},
		{
			// version-scoped form: dot-separated version.kind + name
			name:  "version dot kind and name",
			input: "v0.helm-workload-instance/my-app",
			expect: map[string]string{
				"objectname":     "my-app",
				"objecttypename": "HelmWorkloadInstance",
				"objectversion":  "v0",
			},
		},
		{
			// fully qualified form: namespace / version.kind / name
			name:  "namespace version dot kind and name",
			input: "threeport.io/v0.helm-workload-instance/my-app",
			expect: map[string]string{
				"objectname":      "my-app",
				"objecttypename":  "HelmWorkloadInstance",
				"objectversion":   "v0",
				"objectnamespace": "threeport.io",
			},
		},
		{
			// namespace + kind (no version prefix on kind segment)
			name:  "namespace and kind without version",
			input: "threeport.io/helm-workload-instance/my-app",
			expect: map[string]string{
				"objectname":      "my-app",
				"objecttypename":  "HelmWorkloadInstance",
				"objectnamespace": "threeport.io",
			},
		},
		{
			// single-word kind stays camel-cased (leading char up)
			name:  "single word kind",
			input: "cluster/prod",
			expect: map[string]string{
				"objectname":     "prod",
				"objecttypename": "Cluster",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// call under test: parse --for into events query
			got, err := buildEventsQueryString(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// assert every expected key/value is present, and no extras
			parsed, perr := url.ParseQuery(got)
			if perr != nil {
				t.Fatalf("returned value is not a valid query string: %v", perr)
			}
			if len(parsed) != len(tc.expect) {
				t.Errorf("expected %d query keys, got %d: %v", len(tc.expect), len(parsed), parsed)
			}
			for k, v := range tc.expect {
				if parsed.Get(k) != v {
					t.Errorf("expected %s=%q, got %q", k, v, parsed.Get(k))
				}
			}
		})
	}
}

// TestBuildEventsQueryStringEmptyReturnsEmpty asserts that an unset --for flag
// yields an empty query string so the caller lists every event unfiltered.
func TestBuildEventsQueryStringEmptyReturnsEmpty(t *testing.T) {
	// call with empty flag: caller expects no filter to be applied
	got, err := buildEventsQueryString("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// assert the returned query string is empty so no filter params are attached
	if got != "" {
		t.Errorf("expected empty query string, got %q", got)
	}
}

// TestBuildEventsQueryStringErrorPaths asserts that malformed --for values
// return an error naming the offending shape (too few/many parts, empty
// name/kind/version/namespace segments).
func TestBuildEventsQueryStringErrorPaths(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains string
	}{
		{
			// no slash at all: only 1 segment, below the 2-segment minimum
			name:     "no slash",
			input:    "just-a-name",
			contains: "expected",
		},
		{
			// too many slashes: 4 segments exceeds max
			name:     "four segments",
			input:    "a/b/c/d",
			contains: "expected",
		},
		{
			// empty name after trailing slash
			name:     "empty name",
			input:    "kind/",
			contains: "empty name",
		},
		{
			// empty kind before name
			name:     "empty kind",
			input:    "/name",
			contains: "empty kind",
		},
		{
			// dot-prefixed kind: version segment empty
			name:     "empty version around dot",
			input:    ".helm-workload-instance/my-app",
			contains: "empty version or kind",
		},
		{
			// dot-suffixed version: kind segment empty
			name:     "empty kind around dot",
			input:    "v0./my-app",
			contains: "empty version or kind",
		},
		{
			// leading slash: namespace segment empty in 3-part form
			name:     "empty namespace",
			input:    "/v0.helm-workload-instance/my-app",
			contains: "empty namespace",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// call under test: parse should reject and describe the failure
			got, err := buildEventsQueryString(tc.input)
			if err == nil {
				t.Fatalf("expected error for input %q, got %q", tc.input, got)
			}

			// assert the returned value is empty on error (no partial query leak)
			if got != "" {
				t.Errorf("expected empty result on error, got %q", got)
			}

			// assert the error message names the specific failure so callers can log it
			if !strings.Contains(err.Error(), tc.contains) {
				t.Errorf("expected error to contain %q, got %q", tc.contains, err.Error())
			}
		})
	}
}

// TestGetEventsCmdMetadata asserts that GetEventsCmd exposes the expected
// cobra metadata (Use, aliases, Short/Long populated) so help output and
// command routing stay stable.
func TestGetEventsCmdMetadata(t *testing.T) {
	// verify command name used for CLI routing
	if GetEventsCmd.Use != "events" {
		t.Errorf("expected Use=events, got %q", GetEventsCmd.Use)
	}

	// verify the "event" alias so both singular and plural resolve
	var hasEventAlias bool
	for _, a := range GetEventsCmd.Aliases {
		if a == "event" {
			hasEventAlias = true
			break
		}
	}
	if !hasEventAlias {
		t.Errorf("expected alias %q in %v", "event", GetEventsCmd.Aliases)
	}

	// verify short/long help are populated for the help index
	if GetEventsCmd.Short == "" {
		t.Error("expected non-empty Short description")
	}
	if GetEventsCmd.Long == "" {
		t.Error("expected non-empty Long description")
	}

	// verify SilenceUsage so cobra doesn't echo help on runtime errors
	if !GetEventsCmd.SilenceUsage {
		t.Error("expected SilenceUsage=true so runtime errors don't dump help")
	}
}

// TestGetEventsCmdFlags asserts that GetEventsCmd defines the expected flags
// with their documented defaults, so the Run body sees the values init() sets.
func TestGetEventsCmdFlags(t *testing.T) {
	// resolve the flag set to inspect names and defaults
	flags := GetEventsCmd.Flags()

	// verify --for exists and defaults to empty (no filter)
	forFlag := flags.Lookup("for")
	if forFlag == nil {
		t.Fatal("expected --for flag to be registered")
	}
	if forFlag.DefValue != "" {
		t.Errorf("expected --for default to be empty, got %q", forFlag.DefValue)
	}

	// verify --output/-o exists and defaults to tabular
	outputFlag := flags.Lookup("output")
	if outputFlag == nil {
		t.Fatal("expected --output flag to be registered")
	}
	if outputFlag.DefValue != "tabular" {
		t.Errorf("expected --output default to be tabular, got %q", outputFlag.DefValue)
	}
	if outputFlag.Shorthand != "o" {
		t.Errorf("expected --output shorthand to be o, got %q", outputFlag.Shorthand)
	}

	// verify --sort exists and defaults to newest
	sortFlag := flags.Lookup("sort")
	if sortFlag == nil {
		t.Fatal("expected --sort flag to be registered")
	}
	if sortFlag.DefValue != "newest" {
		t.Errorf("expected --sort default to be newest, got %q", sortFlag.DefValue)
	}

	// verify --limit exists and defaults to 0 (no cap)
	limitFlag := flags.Lookup("limit")
	if limitFlag == nil {
		t.Fatal("expected --limit flag to be registered")
	}
	if limitFlag.DefValue != "0" {
		t.Errorf("expected --limit default to be 0, got %q", limitFlag.DefValue)
	}

	// verify --control-plane-name/-i exists for control-plane targeting
	cpFlag := flags.Lookup("control-plane-name")
	if cpFlag == nil {
		t.Fatal("expected --control-plane-name flag to be registered")
	}
	if cpFlag.Shorthand != "i" {
		t.Errorf("expected --control-plane-name shorthand to be i, got %q", cpFlag.Shorthand)
	}
}

// TestGetEventsCmdRegisteredWithGet asserts that init() attached GetEventsCmd
// to GetCmd so `tptctl get events` resolves at the CLI layer.
func TestGetEventsCmdRegisteredWithGet(t *testing.T) {
	// walk GetCmd's children looking for the events subcommand
	var found bool
	for _, c := range GetCmd.Commands() {
		if c == GetEventsCmd {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected GetEventsCmd to be registered as a subcommand of GetCmd")
	}
}
