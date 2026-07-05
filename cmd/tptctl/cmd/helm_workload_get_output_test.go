package cmd

import (
	"strings"
	"testing"

	config_v0 "github.com/threeport/threeport/pkg/config/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestOutputGetv0HelmWorkloadsCmdRendersRows covers the happy path for the
// helm-workloads helper: header prints once, populated rows print every field,
// and nil runtime-instance / status / age fall back to empty without panic.
func TestOutputGetv0HelmWorkloadsCmdRendersRows(t *testing.T) {
	cases := []struct {
		name          string
		helmWorkloads []config_v0.HelmWorkloadConfig
		wants         []string
		notWants      []string
	}{
		{
			// empty slice still emits the header row
			name:          "empty slice renders header only",
			helmWorkloads: []config_v0.HelmWorkloadConfig{},
			wants: []string{
				"NAME", "HELM WORKLOAD DEFINITION", "HELM WORKLOAD INSTANCE",
				"REPO", "CHART", "KUBERNETES RUNTIME INSTANCE", "STATUS", "AGE",
			},
			notWants: []string{"workload-a"},
		},
		{
			// populated row with every optional pointer set prints all fields
			name: "populated row prints all fields",
			helmWorkloads: []config_v0.HelmWorkloadConfig{
				{HelmWorkload: config_v0.HelmWorkloadValues{
					Name:  util.Ptr("workload-a"),
					Repo:  util.Ptr("https://charts.example"),
					Chart: util.Ptr("nginx"),
					KubernetesRuntimeInstance: &config_v0.KubernetesRuntimeInstanceValues{
						Name: util.Ptr("kri-a"),
					},
					Status: util.Ptr("Healthy"),
					Age:    util.Ptr("2d"),
				}},
			},
			wants: []string{
				"workload-a", "https://charts.example", "nginx", "kri-a", "Healthy", "2d",
			},
		},
		{
			// each nil-guard branch (runtime instance, status, age) falls back independently
			name: "nil pointers fall back to empty without panic",
			helmWorkloads: []config_v0.HelmWorkloadConfig{
				{HelmWorkload: config_v0.HelmWorkloadValues{
					Name:                      util.Ptr("workload-b"),
					Repo:                      util.Ptr("https://charts.example"),
					Chart:                     util.Ptr("redis"),
					KubernetesRuntimeInstance: nil,
					Status:                    nil,
					Age:                       nil,
				}},
			},
			wants:    []string{"workload-b", "redis"},
			notWants: []string{"<nil>"},
		},
		{
			// runtime instance struct present but inner Name nil still falls back to empty
			name: "nested runtime instance with nil name falls back",
			helmWorkloads: []config_v0.HelmWorkloadConfig{
				{HelmWorkload: config_v0.HelmWorkloadValues{
					Name:                      util.Ptr("workload-c"),
					Repo:                      util.Ptr("repo-c"),
					Chart:                     util.Ptr("chart-c"),
					KubernetesRuntimeInstance: &config_v0.KubernetesRuntimeInstanceValues{Name: nil},
					Status:                    util.Ptr("Reconciling"),
				}},
			},
			wants:    []string{"workload-c", "Reconciling"},
			notWants: []string{"<nil>"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke helper and capture stdout
			workloads := tc.helmWorkloads
			out, err := captureStdout(t, func() error {
				return outputGetv0HelmWorkloadsCmd(&workloads)
			})
			// verify contract: helper never returns an error
			if err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			// verify expected substrings present
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

// TestOutputGetv0HelmWorkloadDefinitionsCmdRendersRows covers the
// helm-workload-definitions helper: header prints, populated rows print all
// fields, and a nil Age falls back to empty without printing "<nil>".
func TestOutputGetv0HelmWorkloadDefinitionsCmdRendersRows(t *testing.T) {
	cases := []struct {
		name        string
		definitions []config_v0.HelmWorkloadDefinitionConfig
		wants       []string
		notWants    []string
	}{
		{
			// empty slice still emits the header row
			name:        "empty slice renders header only",
			definitions: []config_v0.HelmWorkloadDefinitionConfig{},
			wants:       []string{"NAME", "REPO", "CHART", "AGE"},
			notWants:    []string{"def-a"},
		},
		{
			// populated row with every optional pointer set prints all fields
			name: "populated row prints all fields",
			definitions: []config_v0.HelmWorkloadDefinitionConfig{
				{HelmWorkloadDefinition: config_v0.HelmWorkloadDefinitionValues{
					Name:  util.Ptr("def-a"),
					Repo:  util.Ptr("https://charts.example"),
					Chart: util.Ptr("nginx"),
					Age:   util.Ptr("5d"),
				}},
			},
			wants: []string{"def-a", "https://charts.example", "nginx", "5d"},
		},
		{
			// nil age branch: falls back to empty and does not print "<nil>"
			name: "nil age falls back to empty",
			definitions: []config_v0.HelmWorkloadDefinitionConfig{
				{HelmWorkloadDefinition: config_v0.HelmWorkloadDefinitionValues{
					Name:  util.Ptr("def-b"),
					Repo:  util.Ptr("repo-b"),
					Chart: util.Ptr("chart-b"),
					Age:   nil,
				}},
			},
			wants:    []string{"def-b", "repo-b", "chart-b"},
			notWants: []string{"<nil>"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke helper and capture stdout
			defs := tc.definitions
			out, err := captureStdout(t, func() error {
				return outputGetv0HelmWorkloadDefinitionsCmd(&defs)
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
			if got := strings.Count(out, "NAME"); got != 1 {
				t.Errorf("expected header NAME once, got %d occurrences in:\n%s", got, out)
			}
		})
	}
}

// TestOutputGetv0HelmWorkloadInstancesCmdRendersRows covers every nil-guard
// branch in the helm-workload-instances helper: nested definition, nested
// runtime instance, status, and age each fall back independently, and an inner
// nil Name on a non-nil nested struct still falls back to empty.
func TestOutputGetv0HelmWorkloadInstancesCmdRendersRows(t *testing.T) {
	cases := []struct {
		name      string
		instances []config_v0.HelmWorkloadInstanceConfig
		wants     []string
		notWants  []string
	}{
		{
			// empty slice still emits the header row
			name:      "empty slice renders header only",
			instances: []config_v0.HelmWorkloadInstanceConfig{},
			wants: []string{
				"NAME", "HELM WORKLOAD DEFINITION", "KUBERNETES RUNTIME INSTANCE", "STATUS", "AGE",
			},
		},
		{
			// populated row with both nested pointers set prints all fields
			name: "populated row with nested definition and runtime prints all fields",
			instances: []config_v0.HelmWorkloadInstanceConfig{
				{HelmWorkloadInstance: config_v0.HelmWorkloadInstanceValues{
					Name: util.Ptr("inst-a"),
					HelmWorkloadDefinition: &config_v0.HelmWorkloadDefinitionValues{
						Name: util.Ptr("def-a"),
					},
					KubernetesRuntimeInstance: &config_v0.KubernetesRuntimeInstanceValues{
						Name: util.Ptr("kri-a"),
					},
					Status: util.Ptr("Healthy"),
					Age:    util.Ptr("1h"),
				}},
			},
			wants: []string{"inst-a", "def-a", "kri-a", "Healthy", "1h"},
		},
		{
			// each nil-guard branch (definition, runtime, status, age) falls back independently
			name: "nil nested pointers fall back to empty without panic",
			instances: []config_v0.HelmWorkloadInstanceConfig{
				{HelmWorkloadInstance: config_v0.HelmWorkloadInstanceValues{
					Name:                      util.Ptr("inst-b"),
					HelmWorkloadDefinition:    nil,
					KubernetesRuntimeInstance: nil,
					Status:                    nil,
					Age:                       nil,
				}},
			},
			wants:    []string{"inst-b"},
			notWants: []string{"<nil>"},
		},
		{
			// nested pointers non-nil but inner Name nil still falls back to empty
			name: "nested pointers non-nil with nil inner name fall back",
			instances: []config_v0.HelmWorkloadInstanceConfig{
				{HelmWorkloadInstance: config_v0.HelmWorkloadInstanceValues{
					Name:                      util.Ptr("inst-c"),
					HelmWorkloadDefinition:    &config_v0.HelmWorkloadDefinitionValues{Name: nil},
					KubernetesRuntimeInstance: &config_v0.KubernetesRuntimeInstanceValues{Name: nil},
					Status:                    util.Ptr("Reconciling"),
					Age:                       util.Ptr("3m"),
				}},
			},
			wants:    []string{"inst-c", "Reconciling", "3m"},
			notWants: []string{"<nil>"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke helper and capture stdout
			instances := tc.instances
			out, err := captureStdout(t, func() error {
				return outputGetv0HelmWorkloadInstancesCmd(&instances)
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
