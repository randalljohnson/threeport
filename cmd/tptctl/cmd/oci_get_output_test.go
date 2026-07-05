package cmd

import (
	"strings"
	"testing"

	config_v0 "github.com/threeport/threeport/pkg/config/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestOutputGetv0OciProvidersCmdRendersRows covers the happy path: header
// always prints, populated rows render every field, and a nil Age falls back
// to empty without introducing "<nil>" into the output.
func TestOutputGetv0OciProvidersCmdRendersRows(t *testing.T) {
	cases := []struct {
		// name identifies the sub-test in output
		name string
		// providers is the input slice passed as a pointer to the helper
		providers []config_v0.OciProviderConfig
		// wants is the set of substrings expected in the rendered output
		wants []string
		// notWants is the set of substrings that must not appear
		notWants []string
	}{
		{
			name:      "empty slice renders header only",
			providers: []config_v0.OciProviderConfig{},
			wants:     []string{"NAME", "USER OCID", "COMPARTMENT OCID", "DEFAULT PROVIDER", "DEFAULT REGION", "AGE"},
			notWants:  []string{"provider-a", "provider-b"},
		},
		{
			name: "populated row prints all fields with age",
			providers: []config_v0.OciProviderConfig{
				{OciProvider: config_v0.OciProviderValues{
					Name:            util.Ptr("provider-a"),
					UserOCID:        util.Ptr("ocid1.user.oc1..user-a"),
					CompartmentOCID: util.Ptr("ocid1.compartment.oc1..cmp-a"),
					DefaultProvider: util.Ptr(true),
					DefaultRegion:   util.Ptr("us-phoenix-1"),
					Age:             util.Ptr("2d"),
				}},
			},
			wants: []string{"provider-a", "ocid1.user.oc1..user-a", "ocid1.compartment.oc1..cmp-a", "true", "us-phoenix-1", "2d"},
		},
		{
			name: "nil age renders as empty and does not panic",
			providers: []config_v0.OciProviderConfig{
				{OciProvider: config_v0.OciProviderValues{
					Name:            util.Ptr("provider-b"),
					UserOCID:        util.Ptr("ocid1.user.oc1..user-b"),
					CompartmentOCID: util.Ptr("ocid1.compartment.oc1..cmp-b"),
					DefaultProvider: util.Ptr(false),
					DefaultRegion:   util.Ptr("us-ashburn-1"),
					Age:             nil,
				}},
			},
			wants:    []string{"provider-b", "false", "us-ashburn-1"},
			notWants: []string{"<nil>"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke helper and capture stdout
			providers := tc.providers
			out, err := captureStdout(t, func() error {
				return outputGetv0OciProvidersCmd(&providers)
			})
			// verify contract error is always nil
			if err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			// verify expected substrings appear in the tabular output
			for _, want := range tc.wants {
				if !strings.Contains(out, want) {
					t.Errorf("expected output to contain %q; got:\n%s", want, out)
				}
			}
			// verify absent substrings really are absent
			for _, nw := range tc.notWants {
				if strings.Contains(out, nw) {
					t.Errorf("expected output NOT to contain %q; got:\n%s", nw, out)
				}
			}
			// verify header appears exactly once regardless of row count
			if got := strings.Count(out, "USER OCID"); got != 1 {
				t.Errorf("expected header USER OCID once, got %d occurrences in:\n%s", got, out)
			}
		})
	}
}

// TestOutputGetv0OciOkeKubernetesRuntimesCmdRendersRows covers the happy path
// for the runtimes helper: header always prints, populated rows print every
// field, and nil Region or Age fall back to empty without producing "<nil>".
func TestOutputGetv0OciOkeKubernetesRuntimesCmdRendersRows(t *testing.T) {
	cases := []struct {
		name     string
		runtimes []config_v0.OciOkeKubernetesRuntimeConfig
		wants    []string
		notWants []string
	}{
		{
			name:     "empty slice renders header only",
			runtimes: []config_v0.OciOkeKubernetesRuntimeConfig{},
			wants:    []string{"NAME", "PROVIDER NAME", "WORKER NODE SHAPE", "WORKER NODE INITIAL COUNT", "REGION", "AGE"},
		},
		{
			name: "populated row prints all fields",
			runtimes: []config_v0.OciOkeKubernetesRuntimeConfig{
				{OciOkeKubernetesRuntime: config_v0.OciOkeKubernetesRuntimeValues{
					Name:                   util.Ptr("runtime-a"),
					OciProviderName:        util.Ptr("provider-a"),
					WorkerNodeShape:        util.Ptr("VM.Standard.E4.Flex"),
					WorkerNodeInitialCount: util.Ptr(3),
					Region:                 util.Ptr("us-phoenix-1"),
					Age:                    util.Ptr("7d"),
				}},
			},
			wants: []string{"runtime-a", "provider-a", "VM.Standard.E4.Flex", "3", "us-phoenix-1", "7d"},
		},
		{
			name: "nil region and age fall back to empty",
			runtimes: []config_v0.OciOkeKubernetesRuntimeConfig{
				{OciOkeKubernetesRuntime: config_v0.OciOkeKubernetesRuntimeValues{
					Name:                   util.Ptr("runtime-b"),
					OciProviderName:        util.Ptr("provider-b"),
					WorkerNodeShape:        util.Ptr("VM.Standard.E3.Flex"),
					WorkerNodeInitialCount: util.Ptr(2),
					Region:                 nil,
					Age:                    nil,
				}},
			},
			wants:    []string{"runtime-b", "provider-b", "VM.Standard.E3.Flex", "2"},
			notWants: []string{"<nil>"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke helper and capture stdout
			runtimes := tc.runtimes
			out, err := captureStdout(t, func() error {
				return outputGetv0OciOkeKubernetesRuntimesCmd(&runtimes)
			})
			// verify contract error is nil
			if err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			// verify expected substrings present
			for _, want := range tc.wants {
				if !strings.Contains(out, want) {
					t.Errorf("expected output to contain %q; got:\n%s", want, out)
				}
			}
			// verify absent substrings
			for _, nw := range tc.notWants {
				if strings.Contains(out, nw) {
					t.Errorf("expected output NOT to contain %q; got:\n%s", nw, out)
				}
			}
		})
	}
}

// TestOutputGetv0OciOkeKubernetesRuntimeDefinitionsCmdRendersRows covers the
// happy path for the definitions helper: header always prints, populated rows
// print every field, and a nil Age falls back to empty without panicking.
func TestOutputGetv0OciOkeKubernetesRuntimeDefinitionsCmdRendersRows(t *testing.T) {
	cases := []struct {
		name        string
		definitions []config_v0.OciOkeKubernetesRuntimeDefinitionConfig
		wants       []string
		notWants    []string
	}{
		{
			name:        "empty slice renders header only",
			definitions: []config_v0.OciOkeKubernetesRuntimeDefinitionConfig{},
			wants:       []string{"NAME", "WORKER NODE SHAPE", "WORKER NODE INITIAL COUNT", "AGE"},
		},
		{
			name: "populated row prints all fields",
			definitions: []config_v0.OciOkeKubernetesRuntimeDefinitionConfig{
				{OciOkeKubernetesRuntimeDefinition: config_v0.OciOkeKubernetesRuntimeDefinitionValues{
					Name:                   util.Ptr("def-a"),
					WorkerNodeShape:        util.Ptr("VM.Standard.E4.Flex"),
					WorkerNodeInitialCount: util.Ptr(4),
					Age:                    util.Ptr("1h"),
				}},
			},
			wants: []string{"def-a", "VM.Standard.E4.Flex", "4", "1h"},
		},
		{
			name: "nil age renders as empty and does not panic",
			definitions: []config_v0.OciOkeKubernetesRuntimeDefinitionConfig{
				{OciOkeKubernetesRuntimeDefinition: config_v0.OciOkeKubernetesRuntimeDefinitionValues{
					Name:                   util.Ptr("def-b"),
					WorkerNodeShape:        util.Ptr("VM.Standard.E3.Flex"),
					WorkerNodeInitialCount: util.Ptr(2),
					Age:                    nil,
				}},
			},
			wants:    []string{"def-b", "VM.Standard.E3.Flex", "2"},
			notWants: []string{"<nil>"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke helper and capture stdout
			defs := tc.definitions
			out, err := captureStdout(t, func() error {
				return outputGetv0OciOkeKubernetesRuntimeDefinitionsCmd(&defs)
			})
			// verify contract error is nil
			if err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			// verify expected substrings present
			for _, want := range tc.wants {
				if !strings.Contains(out, want) {
					t.Errorf("expected output to contain %q; got:\n%s", want, out)
				}
			}
			// verify absent substrings
			for _, nw := range tc.notWants {
				if strings.Contains(out, nw) {
					t.Errorf("expected output NOT to contain %q; got:\n%s", nw, out)
				}
			}
		})
	}
}

// TestOutputGetv0OciOkeKubernetesRuntimeInstancesCmdRendersRows covers every
// nil-guard branch in the instances helper: the nested definition pointer,
// its inner Name, Region, Status, and Age each fall back to empty
// independently and the header always prints exactly once.
func TestOutputGetv0OciOkeKubernetesRuntimeInstancesCmdRendersRows(t *testing.T) {
	cases := []struct {
		name      string
		instances []config_v0.OciOkeKubernetesRuntimeInstanceConfig
		wants     []string
		notWants  []string
	}{
		{
			name:      "empty slice renders header only",
			instances: []config_v0.OciOkeKubernetesRuntimeInstanceConfig{},
			wants:     []string{"NAME", "OCI OKE KUBERNETES RUNTIME DEFINITION", "REGION", "STATUS", "AGE"},
		},
		{
			name: "populated row with nested definition prints all fields",
			instances: []config_v0.OciOkeKubernetesRuntimeInstanceConfig{
				{OciOkeKubernetesRuntimeInstance: config_v0.OciOkeKubernetesRuntimeInstanceValues{
					Name: util.Ptr("inst-a"),
					OciOkeKubernetesRuntimeDefinition: &config_v0.OciOkeKubernetesRuntimeDefinitionValues{
						Name: util.Ptr("def-a"),
					},
					Region: util.Ptr("us-phoenix-1"),
					Status: util.Ptr("Reconciled"),
					Age:    util.Ptr("3d"),
				}},
			},
			wants: []string{"inst-a", "def-a", "us-phoenix-1", "Reconciled", "3d"},
		},
		{
			name: "nil nested definition pointer falls back to empty without panic",
			instances: []config_v0.OciOkeKubernetesRuntimeInstanceConfig{
				{OciOkeKubernetesRuntimeInstance: config_v0.OciOkeKubernetesRuntimeInstanceValues{
					Name:                              util.Ptr("inst-b"),
					OciOkeKubernetesRuntimeDefinition: nil,
					Region:                            nil,
					Status:                            nil,
					Age:                               nil,
				}},
			},
			wants:    []string{"inst-b"},
			notWants: []string{"<nil>", "def-a"},
		},
		{
			name: "non-nil nested definition but inner name nil still falls back",
			instances: []config_v0.OciOkeKubernetesRuntimeInstanceConfig{
				{OciOkeKubernetesRuntimeInstance: config_v0.OciOkeKubernetesRuntimeInstanceValues{
					Name:                              util.Ptr("inst-c"),
					OciOkeKubernetesRuntimeDefinition: &config_v0.OciOkeKubernetesRuntimeDefinitionValues{Name: nil},
					Region:                            util.Ptr("us-ashburn-1"),
					Status:                            util.Ptr("Reconciling"),
					Age:                               util.Ptr("5m"),
				}},
			},
			wants:    []string{"inst-c", "us-ashburn-1", "Reconciling", "5m"},
			notWants: []string{"<nil>"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke helper and capture stdout
			instances := tc.instances
			out, err := captureStdout(t, func() error {
				return outputGetv0OciOkeKubernetesRuntimeInstancesCmd(&instances)
			})
			// verify contract error is nil
			if err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			// verify expected substrings present
			for _, want := range tc.wants {
				if !strings.Contains(out, want) {
					t.Errorf("expected output to contain %q; got:\n%s", want, out)
				}
			}
			// verify absent substrings
			for _, nw := range tc.notWants {
				if strings.Contains(out, nw) {
					t.Errorf("expected output NOT to contain %q; got:\n%s", nw, out)
				}
			}
			// verify header appears exactly once regardless of row count
			if got := strings.Count(out, "STATUS"); got != 1 {
				t.Errorf("expected header STATUS once, got %d occurrences in:\n%s", got, out)
			}
		})
	}
}
