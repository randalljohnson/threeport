package cmd

import (
	"strings"
	"testing"

	config_v0 "github.com/threeport/threeport/pkg/config/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestOutputGetv0GcpProvidersCmdRendersRows covers the happy path plus nil
// optional pointers: header prints once, populated pointer fields land in the
// output, and nil ProjectID / DefaultProvider / DefaultRegion / Age fall back
// to their zero-values without panicking.
func TestOutputGetv0GcpProvidersCmdRendersRows(t *testing.T) {
	cases := []struct {
		// name identifies the sub-test in output
		name string
		// providers is the input slice passed as a pointer to the helper
		providers []config_v0.GcpProviderConfig
		// wants is the set of substrings expected in the rendered output
		wants []string
		// notWants is the set of substrings that must not appear
		notWants []string
	}{
		{
			name:      "empty slice renders header only",
			providers: []config_v0.GcpProviderConfig{},
			wants:     []string{"VERSION", "NAME", "PROJECT ID", "DEFAULT PROVIDER", "DEFAULT REGION", "AGE"},
			notWants:  []string{"provider-a", "provider-b"},
		},
		{
			name: "populated row prints every field",
			providers: []config_v0.GcpProviderConfig{
				{GcpProvider: config_v0.GcpProviderValues{
					Name:            util.Ptr("provider-a"),
					ProjectID:       util.Ptr("my-project-123"),
					DefaultProvider: util.Ptr(true),
					DefaultRegion:   util.Ptr("us-central1"),
					Age:             util.Ptr("2d"),
				}},
			},
			wants: []string{"v0", "provider-a", "my-project-123", "true", "us-central1", "2d"},
		},
		{
			name: "nil optional fields fall back to empty and false",
			providers: []config_v0.GcpProviderConfig{
				{GcpProvider: config_v0.GcpProviderValues{
					Name:            util.Ptr("provider-b"),
					ProjectID:       nil,
					DefaultProvider: nil,
					DefaultRegion:   nil,
					Age:             nil,
				}},
			},
			wants:    []string{"provider-b", "false"},
			notWants: []string{"<nil>"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke helper and capture stdout
			providers := tc.providers
			out, err := captureStdout(t, func() error {
				return outputGetv0GcpProvidersCmd(&providers)
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
			if got := strings.Count(out, "PROJECT ID"); got != 1 {
				t.Errorf("expected PROJECT ID header once, got %d in:\n%s", got, out)
			}
		})
	}
}

// TestOutputGetv0GcpGkeKubernetesRuntimesCmdRendersRows covers happy path and
// nil-pointer fallbacks for the runtimes helper: every numeric field is
// rendered via strconv when set, empty when nil; string pointers fall back to
// empty; Reconciled defaults to false.
func TestOutputGetv0GcpGkeKubernetesRuntimesCmdRendersRows(t *testing.T) {
	cases := []struct {
		name     string
		runtimes []config_v0.GcpGkeKubernetesRuntimeConfig
		wants    []string
		notWants []string
	}{
		{
			name:     "empty slice renders header only",
			runtimes: []config_v0.GcpGkeKubernetesRuntimeConfig{},
			wants:    []string{"VERSION", "NAME", "GCP PROVIDER", "REGION", "ZONE COUNT", "RECONCILED", "AGE"},
			notWants: []string{"runtime-a"},
		},
		{
			name: "populated row prints all fields",
			runtimes: []config_v0.GcpGkeKubernetesRuntimeConfig{
				{GcpGkeKubernetesRuntime: config_v0.GcpGkeKubernetesRuntimeValues{
					Name:                         util.Ptr("runtime-a"),
					GcpProviderName:              util.Ptr("provider-a"),
					Region:                       util.Ptr("us-central1"),
					ZoneCount:                    util.Ptr(3),
					DefaultNodeGroupInstanceType: util.Ptr("e2-standard-4"),
					DefaultNodeGroupInitialSize:  util.Ptr(2),
					DefaultNodeGroupMinimumSize:  util.Ptr(1),
					DefaultNodeGroupMaximumSize:  util.Ptr(5),
					Reconciled:                   util.Ptr(true),
					Age:                          util.Ptr("1h"),
				}},
			},
			wants: []string{"runtime-a", "provider-a", "us-central1", "3", "e2-standard-4", "2", "1", "5", "true", "1h"},
		},
		{
			name: "nil optional fields render as empty and reconciled false",
			runtimes: []config_v0.GcpGkeKubernetesRuntimeConfig{
				{GcpGkeKubernetesRuntime: config_v0.GcpGkeKubernetesRuntimeValues{
					Name: util.Ptr("runtime-b"),
					// all other fields nil
				}},
			},
			wants:    []string{"runtime-b", "false"},
			notWants: []string{"<nil>"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runtimes := tc.runtimes
			// invoke helper and capture stdout
			out, err := captureStdout(t, func() error {
				return outputGetv0GcpGkeKubernetesRuntimesCmd(&runtimes)
			})
			// verify no error is ever returned
			if err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			// verify each expected substring appears in the output
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
			// verify GCP PROVIDER header appears exactly once
			if got := strings.Count(out, "GCP PROVIDER"); got != 1 {
				t.Errorf("expected GCP PROVIDER header once, got %d in:\n%s", got, out)
			}
		})
	}
}

// TestOutputGetv0GcpGkeKubernetesRuntimeDefinitionsCmdRendersRows covers happy
// path and nil-pointer fallbacks for the runtime-definitions helper.
func TestOutputGetv0GcpGkeKubernetesRuntimeDefinitionsCmdRendersRows(t *testing.T) {
	cases := []struct {
		name        string
		definitions []config_v0.GcpGkeKubernetesRuntimeDefinitionConfig
		wants       []string
		notWants    []string
	}{
		{
			name:        "empty slice renders header only",
			definitions: []config_v0.GcpGkeKubernetesRuntimeDefinitionConfig{},
			wants:       []string{"VERSION", "NAME", "ZONE COUNT", "AGE"},
			notWants:    []string{"def-a"},
		},
		{
			name: "populated row prints all fields",
			definitions: []config_v0.GcpGkeKubernetesRuntimeDefinitionConfig{
				{GcpGkeKubernetesRuntimeDefinition: config_v0.GcpGkeKubernetesRuntimeDefinitionValues{
					Name:                         util.Ptr("def-a"),
					ZoneCount:                    util.Ptr(3),
					DefaultNodeGroupInstanceType: util.Ptr("e2-medium"),
					DefaultNodeGroupInitialSize:  util.Ptr(2),
					DefaultNodeGroupMinimumSize:  util.Ptr(1),
					DefaultNodeGroupMaximumSize:  util.Ptr(4),
					Age:                          util.Ptr("3h"),
				}},
			},
			wants: []string{"def-a", "3", "e2-medium", "2", "1", "4", "3h"},
		},
		{
			name: "nil optional fields render as empty",
			definitions: []config_v0.GcpGkeKubernetesRuntimeDefinitionConfig{
				{GcpGkeKubernetesRuntimeDefinition: config_v0.GcpGkeKubernetesRuntimeDefinitionValues{
					Name: util.Ptr("def-b"),
					// numeric and age pointers left nil
				}},
			},
			wants:    []string{"def-b"},
			notWants: []string{"<nil>"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			definitions := tc.definitions
			// invoke helper and capture stdout
			out, err := captureStdout(t, func() error {
				return outputGetv0GcpGkeKubernetesRuntimeDefinitionsCmd(&definitions)
			})
			// verify no error is ever returned
			if err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			// verify each expected substring appears
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
			// verify ZONE COUNT header appears exactly once
			if got := strings.Count(out, "ZONE COUNT"); got != 1 {
				t.Errorf("expected ZONE COUNT header once, got %d in:\n%s", got, out)
			}
		})
	}
}

// TestOutputGetv0GcpGkeKubernetesRuntimeInstancesCmdRendersRows covers the
// happy path plus the nested-pointer branches for KubernetesRuntimeInstance
// and GcpGkeKubernetesRuntimeDefinition: outer nil, inner nil name, and both
// populated.
func TestOutputGetv0GcpGkeKubernetesRuntimeInstancesCmdRendersRows(t *testing.T) {
	cases := []struct {
		name      string
		instances []config_v0.GcpGkeKubernetesRuntimeInstanceConfig
		wants     []string
		notWants  []string
	}{
		{
			name:      "empty slice renders header only",
			instances: []config_v0.GcpGkeKubernetesRuntimeInstanceConfig{},
			wants:     []string{"VERSION", "NAME", "GCP PROVIDER", "REGION", "KUBERNETES RUNTIME INSTANCE NAME", "RECONCILED", "AGE"},
			notWants:  []string{"inst-a"},
		},
		{
			name: "populated row with nested runtime-instance and definition prints all fields",
			instances: []config_v0.GcpGkeKubernetesRuntimeInstanceConfig{
				{GcpGkeKubernetesRuntimeInstance: config_v0.GcpGkeKubernetesRuntimeInstanceValues{
					Name:            util.Ptr("inst-a"),
					GcpProviderName: util.Ptr("provider-a"),
					Region:          util.Ptr("us-central1"),
					KubernetesRuntimeInstance: &config_v0.KubernetesRuntimeInstanceValues{
						Name: util.Ptr("kri-a"),
					},
					GcpGkeKubernetesRuntimeDefinition: &config_v0.GcpGkeKubernetesRuntimeDefinitionValues{
						Name: util.Ptr("def-a"),
					},
					Reconciled: util.Ptr(true),
					Age:        util.Ptr("5m"),
				}},
			},
			wants: []string{"inst-a", "provider-a", "us-central1", "kri-a", "def-a", "true", "5m"},
		},
		{
			name: "nil nested pointers render as empty and reconciled false",
			instances: []config_v0.GcpGkeKubernetesRuntimeInstanceConfig{
				{GcpGkeKubernetesRuntimeInstance: config_v0.GcpGkeKubernetesRuntimeInstanceValues{
					Name: util.Ptr("inst-b"),
					// KubernetesRuntimeInstance and GcpGkeKubernetesRuntimeDefinition nil
				}},
			},
			wants:    []string{"inst-b", "false"},
			notWants: []string{"<nil>"},
		},
		{
			name: "nested pointer set but inner Name nil renders empty",
			instances: []config_v0.GcpGkeKubernetesRuntimeInstanceConfig{
				{GcpGkeKubernetesRuntimeInstance: config_v0.GcpGkeKubernetesRuntimeInstanceValues{
					Name: util.Ptr("inst-c"),
					KubernetesRuntimeInstance: &config_v0.KubernetesRuntimeInstanceValues{
						// Name nil
					},
					GcpGkeKubernetesRuntimeDefinition: &config_v0.GcpGkeKubernetesRuntimeDefinitionValues{
						// Name nil
					},
				}},
			},
			wants:    []string{"inst-c"},
			notWants: []string{"<nil>"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			instances := tc.instances
			// invoke helper and capture stdout
			out, err := captureStdout(t, func() error {
				return outputGetv0GcpGkeKubernetesRuntimeInstancesCmd(&instances)
			})
			// verify no error is ever returned
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
			// verify KUBERNETES RUNTIME INSTANCE NAME header appears exactly once
			if got := strings.Count(out, "KUBERNETES RUNTIME INSTANCE NAME"); got != 1 {
				t.Errorf("expected KRI header once, got %d in:\n%s", got, out)
			}
		})
	}
}
