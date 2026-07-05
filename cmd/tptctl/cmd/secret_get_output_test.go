package cmd

import (
	"strings"
	"testing"

	config_v0 "github.com/threeport/threeport/pkg/config/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestOutputGetv0SecretsCmdRendersRows covers the happy path and the
// nil-pointer branches for each optional related object (workload instance,
// helm workload instance, kubernetes runtime instance) and age.
func TestOutputGetv0SecretsCmdRendersRows(t *testing.T) {
	cases := []struct {
		// name identifies the sub-test in output
		name string
		// secrets is the input slice passed as a pointer to the helper
		secrets []config_v0.SecretConfig
		// wants is the set of substrings expected in the rendered output
		wants []string
		// notWants is the set of substrings that must not appear
		notWants []string
	}{
		{
			// empty input still emits the header, no data rows
			name:     "empty slice renders header only",
			secrets:  []config_v0.SecretConfig{},
			wants:    []string{"NAME", "SECRET DEFINITION", "SECRET INSTANCE", "WORKLOAD INSTANCE", "HELM WORKLOAD INSTANCE", "KUBERNETES RUNTIME INSTANCE", "AGE"},
			notWants: []string{"secret-a"},
		},
		{
			// fully populated row: every optional relation and age is set
			name: "populated row with all related names and age",
			secrets: []config_v0.SecretConfig{
				{Secret: config_v0.SecretValues{
					Name: util.Ptr("secret-a"),
					KubernetesWorkloadInstance: &config_v0.KubernetesWorkloadInstanceValues{
						Name: util.Ptr("workload-x"),
					},
					HelmWorkloadInstance: &config_v0.HelmWorkloadInstanceValues{
						Name: util.Ptr("helm-y"),
					},
					KubernetesRuntimeInstance: &config_v0.KubernetesRuntimeInstanceValues{
						Name: util.Ptr("runtime-z"),
					},
					Age: util.Ptr("3d"),
				}},
			},
			wants: []string{"secret-a", "workload-x", "helm-y", "runtime-z", "3d"},
		},
		{
			// all optional pointers nil: helper must not panic and empty
			// fields must not surface as literal "<nil>"
			name: "nil optional relations render empty without panic",
			secrets: []config_v0.SecretConfig{
				{Secret: config_v0.SecretValues{
					Name:                       util.Ptr("secret-b"),
					KubernetesWorkloadInstance: nil,
					HelmWorkloadInstance:       nil,
					KubernetesRuntimeInstance:  nil,
					Age:                        nil,
				}},
			},
			wants:    []string{"secret-b"},
			notWants: []string{"<nil>"},
		},
		{
			// related object is non-nil but its Name is nil: guard covers
			// both halves of the &&-chain
			name: "non-nil relation with nil Name renders empty",
			secrets: []config_v0.SecretConfig{
				{Secret: config_v0.SecretValues{
					Name: util.Ptr("secret-c"),
					KubernetesWorkloadInstance: &config_v0.KubernetesWorkloadInstanceValues{
						Name: nil,
					},
					HelmWorkloadInstance: &config_v0.HelmWorkloadInstanceValues{
						Name: nil,
					},
					KubernetesRuntimeInstance: &config_v0.KubernetesRuntimeInstanceValues{
						Name: nil,
					},
					Age: util.Ptr("1h"),
				}},
			},
			wants:    []string{"secret-c", "1h"},
			notWants: []string{"<nil>"},
		},
		{
			// multiple rows: each row's identifying values appear
			name: "multiple rows render distinct names",
			secrets: []config_v0.SecretConfig{
				{Secret: config_v0.SecretValues{Name: util.Ptr("row-1"), Age: util.Ptr("1d")}},
				{Secret: config_v0.SecretValues{Name: util.Ptr("row-2"), Age: util.Ptr("2d")}},
			},
			wants: []string{"row-1", "row-2", "1d", "2d"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke helper and capture stdout
			secrets := tc.secrets
			out, err := captureStdout(t, func() error {
				return outputGetv0SecretsCmd(&secrets)
			})
			// verify the helper's contract of a nil error return
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
		})
	}
}

// TestOutputGetv0SecretDefinitionsCmdRendersRows covers the happy path
// and the nil-Age branch for the secret-definitions listing.
func TestOutputGetv0SecretDefinitionsCmdRendersRows(t *testing.T) {
	cases := []struct {
		name        string
		definitions []config_v0.SecretDefinitionConfig
		wants       []string
		notWants    []string
	}{
		{
			// empty input still emits the header
			name:        "empty slice renders header only",
			definitions: []config_v0.SecretDefinitionConfig{},
			wants:       []string{"NAME", "AGE"},
			notWants:    []string{"def-a"},
		},
		{
			// each definition contributes one row with name and age
			name: "populated rows with age",
			definitions: []config_v0.SecretDefinitionConfig{
				{SecretDefinition: config_v0.SecretDefinitionValues{
					Name: util.Ptr("def-a"), Age: util.Ptr("5d"),
				}},
				{SecretDefinition: config_v0.SecretDefinitionValues{
					Name: util.Ptr("def-b"), Age: util.Ptr("6d"),
				}},
			},
			wants: []string{"def-a", "def-b", "5d", "6d"},
		},
		{
			// nil Age must render empty, not "<nil>"
			name: "nil age renders as empty",
			definitions: []config_v0.SecretDefinitionConfig{
				{SecretDefinition: config_v0.SecretDefinitionValues{
					Name: util.Ptr("def-c"), Age: nil,
				}},
			},
			wants:    []string{"def-c"},
			notWants: []string{"<nil>"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke helper and capture stdout
			defs := tc.definitions
			out, err := captureStdout(t, func() error {
				return outputGetv0SecretDefinitionsCmd(&defs)
			})
			// verify nil-error contract
			if err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			// verify expected substrings appear
			for _, want := range tc.wants {
				if !strings.Contains(out, want) {
					t.Errorf("expected output to contain %q; got:\n%s", want, out)
				}
			}
			// verify absent substrings are absent
			for _, nw := range tc.notWants {
				if strings.Contains(out, nw) {
					t.Errorf("expected output NOT to contain %q; got:\n%s", nw, out)
				}
			}
		})
	}
}

// TestOutputGetv0SecretInstancesCmdRendersRows covers the happy path and
// the nil-pointer branches for each optional related object and age on
// the secret-instances listing.
func TestOutputGetv0SecretInstancesCmdRendersRows(t *testing.T) {
	cases := []struct {
		name      string
		instances []config_v0.SecretInstanceConfig
		wants     []string
		notWants  []string
	}{
		{
			// empty input still emits the header
			name:      "empty slice renders header only",
			instances: []config_v0.SecretInstanceConfig{},
			wants:     []string{"NAME", "SECRET DEFINITION", "WORKLOAD INSTANCE", "HELM WORKLOAD INSTANCE", "KUBERNETES RUNTIME INSTANCE", "AGE"},
			notWants:  []string{"inst-a"},
		},
		{
			// fully populated row: every optional relation and age is set
			name: "populated row with all related names and age",
			instances: []config_v0.SecretInstanceConfig{
				{SecretInstance: config_v0.SecretInstanceValues{
					Name: util.Ptr("inst-a"),
					SecretDefinition: &config_v0.SecretDefinitionValues{
						Name: util.Ptr("def-a"),
					},
					KubernetesWorkloadInstance: &config_v0.KubernetesWorkloadInstanceValues{
						Name: util.Ptr("workload-a"),
					},
					HelmWorkloadInstance: &config_v0.HelmWorkloadInstanceValues{
						Name: util.Ptr("helm-a"),
					},
					KubernetesRuntimeInstance: &config_v0.KubernetesRuntimeInstanceValues{
						Name: util.Ptr("runtime-a"),
					},
					Age: util.Ptr("7d"),
				}},
			},
			wants: []string{"inst-a", "def-a", "workload-a", "helm-a", "runtime-a", "7d"},
		},
		{
			// all optional pointers nil: helper must not panic and no
			// literal "<nil>" surfaces in the output
			name: "nil optional relations render empty without panic",
			instances: []config_v0.SecretInstanceConfig{
				{SecretInstance: config_v0.SecretInstanceValues{
					Name:                       util.Ptr("inst-b"),
					SecretDefinition:           nil,
					KubernetesWorkloadInstance: nil,
					HelmWorkloadInstance:       nil,
					KubernetesRuntimeInstance:  nil,
					Age:                        nil,
				}},
			},
			wants:    []string{"inst-b"},
			notWants: []string{"<nil>"},
		},
		{
			// each relation is non-nil but its inner Name is nil: exercises
			// the second half of every &&-guard
			name: "non-nil relations with nil inner Name render empty",
			instances: []config_v0.SecretInstanceConfig{
				{SecretInstance: config_v0.SecretInstanceValues{
					Name: util.Ptr("inst-c"),
					SecretDefinition: &config_v0.SecretDefinitionValues{
						Name: nil,
					},
					KubernetesWorkloadInstance: &config_v0.KubernetesWorkloadInstanceValues{
						Name: nil,
					},
					HelmWorkloadInstance: &config_v0.HelmWorkloadInstanceValues{
						Name: nil,
					},
					KubernetesRuntimeInstance: &config_v0.KubernetesRuntimeInstanceValues{
						Name: nil,
					},
					Age: util.Ptr("1h"),
				}},
			},
			wants:    []string{"inst-c", "1h"},
			notWants: []string{"<nil>"},
		},
		{
			// multiple rows: each row's identifying values appear
			name: "multiple rows render distinct names",
			instances: []config_v0.SecretInstanceConfig{
				{SecretInstance: config_v0.SecretInstanceValues{Name: util.Ptr("i-1"), Age: util.Ptr("1d")}},
				{SecretInstance: config_v0.SecretInstanceValues{Name: util.Ptr("i-2"), Age: util.Ptr("2d")}},
			},
			wants: []string{"i-1", "i-2", "1d", "2d"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke helper and capture stdout
			insts := tc.instances
			out, err := captureStdout(t, func() error {
				return outputGetv0SecretInstancesCmd(&insts)
			})
			// verify nil-error contract
			if err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			// verify expected substrings appear
			for _, want := range tc.wants {
				if !strings.Contains(out, want) {
					t.Errorf("expected output to contain %q; got:\n%s", want, out)
				}
			}
			// verify absent substrings are absent
			for _, nw := range tc.notWants {
				if strings.Contains(out, nw) {
					t.Errorf("expected output NOT to contain %q; got:\n%s", nw, out)
				}
			}
		})
	}
}
