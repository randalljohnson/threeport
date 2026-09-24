package cmd

import (
	"strings"
	"testing"

	config_v0 "github.com/threeport/threeport/pkg/config/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestOutputGetv0KubernetesRuntimesCmdRendersRows covers the happy path: the
// header always prints, populated rows print every field, and nil age plus nil
// infra-provider-account fall back to empty strings without panicking.
func TestOutputGetv0KubernetesRuntimesCmdRendersRows(t *testing.T) {
	cases := []struct {
		// name identifies the sub-test in output
		name string
		// runtimes is the input slice passed as a pointer to the helper
		runtimes []config_v0.KubernetesRuntimeConfig
		// wants is the set of substrings expected in the rendered output
		wants []string
		// notWants is the set of substrings that must not appear
		notWants []string
	}{
		{
			name:     "empty slice renders header only",
			runtimes: []config_v0.KubernetesRuntimeConfig{},
			wants: []string{
				"NAME",
				"KUBERNETES RUNTIME DEFINITION",
				"KUBERNETES RUNTIME INSTANCE",
				"INFRA PROVIDER",
				"HIGH AVAILABILITY",
				"INFRA PROVIDER ACCOUNT",
				"LOCATION",
				"DEFAULT RUNTIME",
				"AGE",
			},
			notWants: []string{"runtime-a", "runtime-b"},
		},
		{
			name: "populated row prints all fields",
			runtimes: []config_v0.KubernetesRuntimeConfig{
				{KubernetesRuntime: config_v0.KubernetesRuntimeValues{
					Name:                     util.Ptr("runtime-a"),
					InfraProvider:            util.Ptr("aws"),
					InfraProviderAccountName: util.Ptr("acct-a"),
					HighAvailability:         util.Ptr(true),
					Location:                 util.Ptr("us-west-2"),
					DefaultRuntime:           util.Ptr(true),
					Age:                      util.Ptr("2d"),
				}},
			},
			wants: []string{"runtime-a", "aws", "acct-a", "true", "us-west-2", "2d"},
		},
		{
			name: "nil age and nil infra provider account fall back to empty",
			runtimes: []config_v0.KubernetesRuntimeConfig{
				{KubernetesRuntime: config_v0.KubernetesRuntimeValues{
					Name:                     util.Ptr("runtime-b"),
					InfraProvider:            util.Ptr("gcp"),
					InfraProviderAccountName: nil,
					HighAvailability:         util.Ptr(false),
					Location:                 util.Ptr("us-central1"),
					DefaultRuntime:           util.Ptr(false),
					Age:                      nil,
				}},
			},
			wants:    []string{"runtime-b", "gcp", "us-central1", "false"},
			notWants: []string{"<nil>"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke helper and capture stdout
			runtimes := tc.runtimes
			out, err := captureStdout(t, func() error {
				return outputGetv0KubernetesRuntimesCmd(&runtimes)
			})
			// verify contract error is nil
			if err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			// verify expected substrings appear
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
			// verify header row prints exactly once regardless of row count
			if got := strings.Count(out, "NAME"); got != 1 {
				t.Errorf("expected header NAME once, got %d occurrences in:\n%s", got, out)
			}
		})
	}
}

// TestOutputGetv0KubernetesRuntimeDefinitionsCmdRendersRows covers the happy
// path for the definitions helper: header always prints, nil age and nil
// account name fall back to empty strings, and populated rows print every
// field.
func TestOutputGetv0KubernetesRuntimeDefinitionsCmdRendersRows(t *testing.T) {
	cases := []struct {
		name        string
		definitions []config_v0.KubernetesRuntimeDefinitionConfig
		wants       []string
		notWants    []string
	}{
		{
			name:        "empty slice renders header only",
			definitions: []config_v0.KubernetesRuntimeDefinitionConfig{},
			wants: []string{
				"NAME",
				"INFRA PROVIDER",
				"HIGH AVAILABILITY",
				"INFRA PROVIDER ACCOUNT",
				"AGE",
			},
		},
		{
			name: "populated row prints all fields",
			definitions: []config_v0.KubernetesRuntimeDefinitionConfig{
				{KubernetesRuntimeDefinition: config_v0.KubernetesRuntimeDefinitionValues{
					Name:                     util.Ptr("def-a"),
					InfraProvider:            util.Ptr("aws"),
					InfraProviderAccountName: util.Ptr("acct-a"),
					HighAvailability:         util.Ptr(true),
					Age:                      util.Ptr("1h"),
				}},
			},
			wants: []string{"def-a", "aws", "acct-a", "true", "1h"},
		},
		{
			name: "nil age and nil account name fall back to empty",
			definitions: []config_v0.KubernetesRuntimeDefinitionConfig{
				{KubernetesRuntimeDefinition: config_v0.KubernetesRuntimeDefinitionValues{
					Name:                     util.Ptr("def-b"),
					InfraProvider:            util.Ptr("gcp"),
					InfraProviderAccountName: nil,
					HighAvailability:         util.Ptr(false),
					Age:                      nil,
				}},
			},
			wants:    []string{"def-b", "gcp", "false"},
			notWants: []string{"<nil>"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke helper and capture stdout
			defs := tc.definitions
			out, err := captureStdout(t, func() error {
				return outputGetv0KubernetesRuntimeDefinitionsCmd(&defs)
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

// TestOutputGetv0KubernetesRuntimeInstancesCmdRendersRows covers every
// nil-guard branch in the instances helper: nested definition pointer, nested
// definition name, and age each fall back to empty independently.
func TestOutputGetv0KubernetesRuntimeInstancesCmdRendersRows(t *testing.T) {
	cases := []struct {
		name      string
		instances []config_v0.KubernetesRuntimeInstanceConfig
		wants     []string
		notWants  []string
	}{
		{
			name:      "empty slice renders header only",
			instances: []config_v0.KubernetesRuntimeInstanceConfig{},
			wants: []string{
				"NAME",
				"KUBERNETES RUNTIME DEFINITION",
				"LOCATION",
				"DEFAULT RUNTIME",
				"AGE",
			},
		},
		{
			name: "populated row with nested definition prints all fields",
			instances: []config_v0.KubernetesRuntimeInstanceConfig{
				{KubernetesRuntimeInstance: config_v0.KubernetesRuntimeInstanceValues{
					Name:           util.Ptr("inst-a"),
					Location:       util.Ptr("us-west-2"),
					DefaultRuntime: util.Ptr(true),
					KubernetesRuntimeDefinition: &config_v0.KubernetesRuntimeDefinitionValues{
						Name: util.Ptr("def-a"),
					},
					Age: util.Ptr("3d"),
				}},
			},
			wants: []string{"inst-a", "def-a", "us-west-2", "true", "3d"},
		},
		{
			name: "nil nested definition pointer falls back to empty",
			instances: []config_v0.KubernetesRuntimeInstanceConfig{
				{KubernetesRuntimeInstance: config_v0.KubernetesRuntimeInstanceValues{
					Name:                        util.Ptr("inst-b"),
					Location:                    util.Ptr("us-east-1"),
					DefaultRuntime:              util.Ptr(false),
					KubernetesRuntimeDefinition: nil,
					Age:                         nil,
				}},
			},
			wants:    []string{"inst-b", "us-east-1", "false"},
			notWants: []string{"<nil>"},
		},
		{
			name: "nested definition non-nil but inner name nil falls back",
			instances: []config_v0.KubernetesRuntimeInstanceConfig{
				{KubernetesRuntimeInstance: config_v0.KubernetesRuntimeInstanceValues{
					Name:           util.Ptr("inst-c"),
					Location:       util.Ptr("us-central1"),
					DefaultRuntime: util.Ptr(false),
					KubernetesRuntimeDefinition: &config_v0.KubernetesRuntimeDefinitionValues{
						Name: nil,
					},
					Age: util.Ptr("1h"),
				}},
			},
			wants:    []string{"inst-c", "us-central1", "1h"},
			notWants: []string{"<nil>"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke helper and capture stdout
			instances := tc.instances
			out, err := captureStdout(t, func() error {
				return outputGetv0KubernetesRuntimeInstancesCmd(&instances)
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
