package cmd

import (
	"strings"
	"testing"

	config_v0 "github.com/threeport/threeport/pkg/config/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestOutputGetv0AwsProvidersCmdRendersRows covers the happy path: every row's
// fields land in the tabular output, a nil Age is rendered as empty, and the
// header appears exactly once.
func TestOutputGetv0AwsProvidersCmdRendersRows(t *testing.T) {
	cases := []struct {
		// name identifies the sub-test in output
		name string
		// providers is the input slice passed as a pointer to the helper
		providers []config_v0.AwsProviderConfig
		// wants is the set of substrings expected in the rendered output
		wants []string
		// notWants is the set of substrings that must not appear
		notWants []string
	}{
		{
			name:      "empty slice renders header only",
			providers: []config_v0.AwsProviderConfig{},
			wants:     []string{"NAME", "DEFAULT ACCOUNT", "DEFAULT REGION", "ACCOUNT ID", "AGE"},
			notWants:  []string{"provider-a", "provider-b"},
		},
		{
			name: "populated rows with age",
			providers: []config_v0.AwsProviderConfig{
				{AwsProvider: config_v0.AwsProviderValues{
					Name:            util.Ptr("provider-a"),
					AccountID:       util.Ptr("111111111111"),
					DefaultProvider: util.Ptr(true),
					DefaultRegion:   util.Ptr("us-west-2"),
					Age:             util.Ptr("2d"),
				}},
			},
			wants: []string{"provider-a", "111111111111", "true", "us-west-2", "2d"},
		},
		{
			name: "nil age renders as empty and does not panic",
			providers: []config_v0.AwsProviderConfig{
				{AwsProvider: config_v0.AwsProviderValues{
					Name:            util.Ptr("provider-b"),
					AccountID:       util.Ptr("222222222222"),
					DefaultProvider: util.Ptr(false),
					DefaultRegion:   util.Ptr("us-east-1"),
					Age:             nil,
				}},
			},
			wants:    []string{"provider-b", "222222222222", "false", "us-east-1"},
			notWants: []string{"<nil>"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke helper and capture stdout
			providers := tc.providers
			out, err := captureStdout(t, func() error {
				return outputGetv0AwsProvidersCmd(&providers)
			})
			// verify no error is ever returned; the helper's contract is nil
			if err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			// verify each expected substring is present
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
			if got := strings.Count(out, "NAME"); got != 1 {
				t.Errorf("expected header NAME once, got %d occurrences in:\n%s", got, out)
			}
		})
	}
}

// TestOutputGetv0AwsEksKubernetesRuntimesCmdRendersRows covers the happy path
// for the runtimes helper: header always prints, region/reconciled/age fall
// back to zero-values when nil, and populated rows print all fields.
func TestOutputGetv0AwsEksKubernetesRuntimesCmdRendersRows(t *testing.T) {
	cases := []struct {
		name     string
		runtimes []config_v0.AwsEksKubernetesRuntimeConfig
		wants    []string
		notWants []string
	}{
		{
			name:     "empty slice renders header only",
			runtimes: []config_v0.AwsEksKubernetesRuntimeConfig{},
			wants:    []string{"NAME", "REGION", "RECONCILED", "AGE"},
		},
		{
			name: "populated row prints all fields",
			runtimes: []config_v0.AwsEksKubernetesRuntimeConfig{
				{AwsEksKubernetesRuntime: config_v0.AwsEksKubernetesRuntimeValues{
					Name:       util.Ptr("runtime-a"),
					Region:     util.Ptr("us-west-2"),
					Reconciled: util.Ptr(true),
					Age:        util.Ptr("7d"),
				}},
			},
			wants: []string{"runtime-a", "us-west-2", "true", "7d"},
		},
		{
			name: "nil region reconciled and age fall back to zero values",
			runtimes: []config_v0.AwsEksKubernetesRuntimeConfig{
				{AwsEksKubernetesRuntime: config_v0.AwsEksKubernetesRuntimeValues{
					Name:       util.Ptr("runtime-b"),
					Region:     nil,
					Reconciled: nil,
					Age:        nil,
				}},
			},
			wants:    []string{"runtime-b", "false"},
			notWants: []string{"<nil>"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke helper and capture stdout
			runtimes := tc.runtimes
			out, err := captureStdout(t, func() error {
				return outputGetv0AwsEksKubernetesRuntimesCmd(&runtimes)
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
			// verify absent substrings
			for _, nw := range tc.notWants {
				if strings.Contains(out, nw) {
					t.Errorf("expected output NOT to contain %q; got:\n%s", nw, out)
				}
			}
		})
	}
}

// TestOutputGetv0AwsEksKubernetesRuntimeDefinitionsCmdRendersRows covers the
// happy path for the definitions helper: header prints, nil age falls back to
// empty, and populated rows print all node-group sizing fields.
func TestOutputGetv0AwsEksKubernetesRuntimeDefinitionsCmdRendersRows(t *testing.T) {
	cases := []struct {
		name        string
		definitions []config_v0.AwsEksKubernetesRuntimeDefinitionConfig
		wants       []string
		notWants    []string
	}{
		{
			name:        "empty slice renders header only",
			definitions: []config_v0.AwsEksKubernetesRuntimeDefinitionConfig{},
			wants:       []string{"NAME", "ZONE COUNT", "DEFAULT NODE GROUP INSTANCE TYPE", "AGE"},
		},
		{
			name: "populated row prints all fields",
			definitions: []config_v0.AwsEksKubernetesRuntimeDefinitionConfig{
				{AwsEksKubernetesRuntimeDefinition: config_v0.AwsEksKubernetesRuntimeDefinitionValues{
					Name:                         util.Ptr("def-a"),
					ZoneCount:                    util.Ptr(3),
					DefaultNodeGroupInstanceType: util.Ptr("t3.medium"),
					DefaultNodeGroupMinimumSize:  util.Ptr(1),
					DefaultNodeGroupMaximumSize:  util.Ptr(10),
					Age:                          util.Ptr("1h"),
				}},
			},
			wants: []string{"def-a", "3", "t3.medium", "1", "10", "1h"},
		},
		{
			name: "nil age renders as empty and does not panic",
			definitions: []config_v0.AwsEksKubernetesRuntimeDefinitionConfig{
				{AwsEksKubernetesRuntimeDefinition: config_v0.AwsEksKubernetesRuntimeDefinitionValues{
					Name:                         util.Ptr("def-b"),
					ZoneCount:                    util.Ptr(2),
					DefaultNodeGroupInstanceType: util.Ptr("t3.small"),
					DefaultNodeGroupMinimumSize:  util.Ptr(1),
					DefaultNodeGroupMaximumSize:  util.Ptr(5),
					Age:                          nil,
				}},
			},
			wants:    []string{"def-b", "t3.small"},
			notWants: []string{"<nil>"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke helper and capture stdout
			defs := tc.definitions
			out, err := captureStdout(t, func() error {
				return outputGetv0AwsEksKubernetesRuntimeDefinitionsCmd(&defs)
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

// TestOutputGetv0AwsEksKubernetesRuntimeInstancesCmdRendersRows covers every
// nil-guard branch in the instances helper: provider, region, nested runtime
// instance, nested definition, reconciled, and age each fall back to their
// zero value independently.
func TestOutputGetv0AwsEksKubernetesRuntimeInstancesCmdRendersRows(t *testing.T) {
	cases := []struct {
		name      string
		instances []config_v0.AwsEksKubernetesRuntimeInstanceConfig
		wants     []string
		notWants  []string
	}{
		{
			name:      "empty slice renders header only",
			instances: []config_v0.AwsEksKubernetesRuntimeInstanceConfig{},
			wants:     []string{"NAME", "AWS PROVIDER", "REGION", "RECONCILED", "AGE"},
		},
		{
			name: "populated row with nested runtime and definition prints all fields",
			instances: []config_v0.AwsEksKubernetesRuntimeInstanceConfig{
				{AwsEksKubernetesRuntimeInstance: config_v0.AwsEksKubernetesRuntimeInstanceValues{
					Name:            util.Ptr("inst-a"),
					AwsProviderName: util.Ptr("provider-a"),
					Region:          util.Ptr("us-west-2"),
					AwsEksKubernetesRuntimeDefinition: &config_v0.AwsEksKubernetesRuntimeDefinitionValues{
						Name: util.Ptr("def-a"),
					},
					KubernetesRuntimeInstance: &config_v0.KubernetesRuntimeInstanceValues{
						Name: util.Ptr("kri-a"),
					},
					Reconciled: util.Ptr(true),
					Age:        util.Ptr("3d"),
				}},
			},
			wants: []string{"inst-a", "provider-a", "us-west-2", "kri-a", "def-a", "true", "3d"},
		},
		{
			name: "nil nested pointers fall back to empty without panic",
			instances: []config_v0.AwsEksKubernetesRuntimeInstanceConfig{
				{AwsEksKubernetesRuntimeInstance: config_v0.AwsEksKubernetesRuntimeInstanceValues{
					Name:                              util.Ptr("inst-b"),
					AwsProviderName:                   nil,
					Region:                            nil,
					AwsEksKubernetesRuntimeDefinition: nil,
					KubernetesRuntimeInstance:         nil,
					Reconciled:                        nil,
					Age:                               nil,
				}},
			},
			wants:    []string{"inst-b", "false"},
			notWants: []string{"<nil>"},
		},
		{
			name: "nested pointers non-nil but inner name nil still falls back",
			instances: []config_v0.AwsEksKubernetesRuntimeInstanceConfig{
				{AwsEksKubernetesRuntimeInstance: config_v0.AwsEksKubernetesRuntimeInstanceValues{
					Name:                              util.Ptr("inst-c"),
					AwsEksKubernetesRuntimeDefinition: &config_v0.AwsEksKubernetesRuntimeDefinitionValues{Name: nil},
					KubernetesRuntimeInstance:         &config_v0.KubernetesRuntimeInstanceValues{Name: nil},
					Reconciled:                        util.Ptr(false),
				}},
			},
			wants:    []string{"inst-c", "false"},
			notWants: []string{"<nil>"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke helper and capture stdout
			instances := tc.instances
			out, err := captureStdout(t, func() error {
				return outputGetv0AwsEksKubernetesRuntimeInstancesCmd(&instances)
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
