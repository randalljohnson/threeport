package cmd

import (
	"strings"
	"testing"

	config_v0 "github.com/threeport/threeport/pkg/config/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestOutputGetv0K8sWorkloadsCmdRendersRows covers the happy path plus every
// nil-guard branch in the workloads helper: nested runtime instance, status,
// and age each fall back to empty independently, and the header appears once.
func TestOutputGetv0K8sWorkloadsCmdRendersRows(t *testing.T) {
	cases := []struct {
		// name identifies the sub-test in output
		name string
		// workloads is the input slice passed as a pointer to the helper
		workloads []config_v0.KubernetesWorkloadConfig
		// wants is the set of substrings expected in the rendered output
		wants []string
		// notWants is the set of substrings that must not appear
		notWants []string
	}{
		{
			name:      "empty slice renders header only",
			workloads: []config_v0.KubernetesWorkloadConfig{},
			wants:     []string{"NAME", "WORKLOAD DEFINITION", "WORKLOAD INSTANCE", "KUBERNETES RUNTIME INSTANCE", "STATUS", "AGE"},
			notWants:  []string{"workload-a", "workload-b"},
		},
		{
			name: "populated row with nested runtime instance prints all fields",
			workloads: []config_v0.KubernetesWorkloadConfig{
				{Workload: config_v0.KubernetesWorkloadValues{
					Name: util.Ptr("workload-a"),
					KubernetesRuntimeInstance: &config_v0.KubernetesRuntimeInstanceValues{
						Name: util.Ptr("kri-a"),
					},
					Status: util.Ptr("Healthy"),
					Age:    util.Ptr("2d"),
				}},
			},
			wants: []string{"workload-a", "kri-a", "Healthy", "2d"},
		},
		{
			name: "nil nested pointers fall back to empty without panic",
			workloads: []config_v0.KubernetesWorkloadConfig{
				{Workload: config_v0.KubernetesWorkloadValues{
					Name:                      util.Ptr("workload-b"),
					KubernetesRuntimeInstance: nil,
					Status:                    nil,
					Age:                       nil,
				}},
			},
			wants:    []string{"workload-b"},
			notWants: []string{"<nil>"},
		},
		{
			name: "nested runtime instance present but Name nil falls back to empty",
			workloads: []config_v0.KubernetesWorkloadConfig{
				{Workload: config_v0.KubernetesWorkloadValues{
					Name:                      util.Ptr("workload-c"),
					KubernetesRuntimeInstance: &config_v0.KubernetesRuntimeInstanceValues{Name: nil},
					Status:                    util.Ptr("Reconciling"),
					Age:                       util.Ptr("1h"),
				}},
			},
			wants:    []string{"workload-c", "Reconciling", "1h"},
			notWants: []string{"<nil>"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke helper and capture stdout
			workloads := tc.workloads
			out, err := captureStdout(t, func() error {
				return outputGetv0K8sWorkloadsCmd(&workloads)
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
			// verify header appears exactly once regardless of row count
			if got := strings.Count(out, "NAME"); got != 1 {
				t.Errorf("expected header NAME once, got %d occurrences in:\n%s", got, out)
			}
		})
	}
}

// TestOutputGetv0K8sWorkloadDefinitionsCmdRendersRows covers the definitions
// helper: header always prints, nil age falls back to empty, and populated
// rows print all fields.
func TestOutputGetv0K8sWorkloadDefinitionsCmdRendersRows(t *testing.T) {
	cases := []struct {
		name        string
		definitions []config_v0.KubernetesWorkloadDefinitionConfig
		wants       []string
		notWants    []string
	}{
		{
			name:        "empty slice renders header only",
			definitions: []config_v0.KubernetesWorkloadDefinitionConfig{},
			wants:       []string{"NAME", "AGE"},
		},
		{
			name: "populated row prints name and age",
			definitions: []config_v0.KubernetesWorkloadDefinitionConfig{
				{KubernetesWorkloadDefinition: config_v0.KubernetesWorkloadDefinitionValues{
					Name: util.Ptr("def-a"),
					Age:  util.Ptr("7d"),
				}},
			},
			wants: []string{"def-a", "7d"},
		},
		{
			name: "nil age renders as empty and does not panic",
			definitions: []config_v0.KubernetesWorkloadDefinitionConfig{
				{KubernetesWorkloadDefinition: config_v0.KubernetesWorkloadDefinitionValues{
					Name: util.Ptr("def-b"),
					Age:  nil,
				}},
			},
			wants:    []string{"def-b"},
			notWants: []string{"<nil>"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke helper and capture stdout
			defs := tc.definitions
			out, err := captureStdout(t, func() error {
				return outputGetv0K8sWorkloadDefinitionsCmd(&defs)
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

// TestOutputGetv0K8sWorkloadInstancesCmdRendersRows covers every nil-guard
// branch in the instances helper: nested definition, nested runtime instance,
// status, and age each fall back to their zero value independently.
func TestOutputGetv0K8sWorkloadInstancesCmdRendersRows(t *testing.T) {
	cases := []struct {
		name      string
		instances []config_v0.KubernetesWorkloadInstanceConfig
		wants     []string
		notWants  []string
	}{
		{
			name:      "empty slice renders header only",
			instances: []config_v0.KubernetesWorkloadInstanceConfig{},
			wants:     []string{"NAME", "WORKLOAD DEFINITION", "KUBERNETES RUNTIME INSTANCE", "STATUS", "AGE"},
		},
		{
			name: "populated row with nested definition and runtime prints all fields",
			instances: []config_v0.KubernetesWorkloadInstanceConfig{
				{KubernetesWorkloadInstance: config_v0.KubernetesWorkloadInstanceValues{
					Name: util.Ptr("inst-a"),
					KubernetesWorkloadDefinition: &config_v0.KubernetesWorkloadDefinitionValues{
						Name: util.Ptr("def-a"),
					},
					KubernetesRuntimeInstance: &config_v0.KubernetesRuntimeInstanceValues{
						Name: util.Ptr("kri-a"),
					},
					Status: util.Ptr("Healthy"),
					Age:    util.Ptr("3d"),
				}},
			},
			wants: []string{"inst-a", "def-a", "kri-a", "Healthy", "3d"},
		},
		{
			name: "nil nested pointers fall back to empty without panic",
			instances: []config_v0.KubernetesWorkloadInstanceConfig{
				{KubernetesWorkloadInstance: config_v0.KubernetesWorkloadInstanceValues{
					Name:                         util.Ptr("inst-b"),
					KubernetesWorkloadDefinition: nil,
					KubernetesRuntimeInstance:    nil,
					Status:                       nil,
					Age:                          nil,
				}},
			},
			wants:    []string{"inst-b"},
			notWants: []string{"<nil>"},
		},
		{
			name: "nested pointers non-nil but inner Name nil still falls back",
			instances: []config_v0.KubernetesWorkloadInstanceConfig{
				{KubernetesWorkloadInstance: config_v0.KubernetesWorkloadInstanceValues{
					Name:                         util.Ptr("inst-c"),
					KubernetesWorkloadDefinition: &config_v0.KubernetesWorkloadDefinitionValues{Name: nil},
					KubernetesRuntimeInstance:    &config_v0.KubernetesRuntimeInstanceValues{Name: nil},
					Status:                       util.Ptr("Reconciling"),
					Age:                          util.Ptr("1h"),
				}},
			},
			wants:    []string{"inst-c", "Reconciling", "1h"},
			notWants: []string{"<nil>"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke helper and capture stdout
			instances := tc.instances
			out, err := captureStdout(t, func() error {
				return outputGetv0K8sWorkloadInstancesCmd(&instances)
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
