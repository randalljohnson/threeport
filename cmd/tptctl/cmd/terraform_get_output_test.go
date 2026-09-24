package cmd

import (
	"strings"
	"testing"

	config_v0 "github.com/threeport/threeport/pkg/config/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestOutputGetv0TerraformsCmdRendersRows covers the happy path and every
// nil-guard branch in the terraforms helper: aws provider, status, and age
// each fall back to their zero value independently and the header prints once.
func TestOutputGetv0TerraformsCmdRendersRows(t *testing.T) {
	cases := []struct {
		// name identifies the sub-test
		name string
		// terraforms is the input slice passed as a pointer to the helper
		terraforms []config_v0.TerraformConfig
		// wants is the set of substrings expected in the rendered output
		wants []string
		// notWants is the set of substrings that must not appear
		notWants []string
	}{
		{
			name:       "empty slice renders header only",
			terraforms: []config_v0.TerraformConfig{},
			wants:      []string{"NAME", "TERRAFORM DEFINITION", "TERRAFORM INSTANCE", "AWS PROVIDER", "STATUS", "AGE"},
			notWants:   []string{"tf-a", "tf-b"},
		},
		{
			name: "populated row prints all fields",
			terraforms: []config_v0.TerraformConfig{
				{Terraform: config_v0.TerraformValues{
					Name: util.Ptr("tf-a"),
					AwsProvider: &config_v0.AwsProviderValues{
						Name: util.Ptr("provider-a"),
					},
					Status: util.Ptr("Reconciled"),
					Age:    util.Ptr("2d"),
				}},
			},
			wants: []string{"tf-a", "provider-a", "Reconciled", "2d"},
		},
		{
			name: "nil aws provider status and age fall back to empty",
			terraforms: []config_v0.TerraformConfig{
				{Terraform: config_v0.TerraformValues{
					Name:        util.Ptr("tf-b"),
					AwsProvider: nil,
					Status:      nil,
					Age:         nil,
				}},
			},
			wants:    []string{"tf-b"},
			notWants: []string{"<nil>"},
		},
		{
			name: "aws provider non-nil but inner name nil falls back to empty",
			terraforms: []config_v0.TerraformConfig{
				{Terraform: config_v0.TerraformValues{
					Name:        util.Ptr("tf-c"),
					AwsProvider: &config_v0.AwsProviderValues{Name: nil},
					Status:      util.Ptr("Provisioning"),
					Age:         util.Ptr("1h"),
				}},
			},
			wants:    []string{"tf-c", "Provisioning", "1h"},
			notWants: []string{"<nil>"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke helper and capture stdout
			terraforms := tc.terraforms
			out, err := captureStdout(t, func() error {
				return outputGetv0TerraformsCmd(&terraforms)
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

// TestOutputGetv0TerraformDefinitionsCmdRendersRows covers the definitions
// helper: header prints, nil age falls back to empty, and populated rows print
// name and age.
func TestOutputGetv0TerraformDefinitionsCmdRendersRows(t *testing.T) {
	cases := []struct {
		name        string
		definitions []config_v0.TerraformDefinitionConfig
		wants       []string
		notWants    []string
	}{
		{
			name:        "empty slice renders header only",
			definitions: []config_v0.TerraformDefinitionConfig{},
			wants:       []string{"NAME", "AGE"},
		},
		{
			name: "populated row prints name and age",
			definitions: []config_v0.TerraformDefinitionConfig{
				{TerraformDefinition: config_v0.TerraformDefinitionValues{
					Name: util.Ptr("def-a"),
					Age:  util.Ptr("7d"),
				}},
			},
			wants: []string{"def-a", "7d"},
		},
		{
			name: "nil age falls back to empty",
			definitions: []config_v0.TerraformDefinitionConfig{
				{TerraformDefinition: config_v0.TerraformDefinitionValues{
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
				return outputGetv0TerraformDefinitionsCmd(&defs)
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

// TestOutputGetv0TerraformInstancesCmdRendersRows covers every nil-guard branch
// in the instances helper: nested definition, aws provider, status, and age
// each fall back to their zero value independently.
func TestOutputGetv0TerraformInstancesCmdRendersRows(t *testing.T) {
	cases := []struct {
		name      string
		instances []config_v0.TerraformInstanceConfig
		wants     []string
		notWants  []string
	}{
		{
			name:      "empty slice renders header only",
			instances: []config_v0.TerraformInstanceConfig{},
			wants:     []string{"NAME", "TERRAFORM DEFINITION", "AWS PROVIDER NAME", "STATUS", "AGE"},
		},
		{
			name: "populated row with nested definition and provider prints all fields",
			instances: []config_v0.TerraformInstanceConfig{
				{TerraformInstance: config_v0.TerraformInstanceValues{
					Name: util.Ptr("inst-a"),
					TerraformDefinition: &config_v0.TerraformDefinitionValues{
						Name: util.Ptr("def-a"),
					},
					AwsProvider: &config_v0.AwsProviderValues{
						Name: util.Ptr("provider-a"),
					},
					Status: util.Ptr("Reconciled"),
					Age:    util.Ptr("3d"),
				}},
			},
			wants: []string{"inst-a", "def-a", "provider-a", "Reconciled", "3d"},
		},
		{
			name: "nil nested pointers fall back to empty without panic",
			instances: []config_v0.TerraformInstanceConfig{
				{TerraformInstance: config_v0.TerraformInstanceValues{
					Name:                util.Ptr("inst-b"),
					TerraformDefinition: nil,
					AwsProvider:         nil,
					Status:              nil,
					Age:                 nil,
				}},
			},
			wants:    []string{"inst-b"},
			notWants: []string{"<nil>"},
		},
		{
			name: "nested pointers non-nil but inner name nil falls back",
			instances: []config_v0.TerraformInstanceConfig{
				{TerraformInstance: config_v0.TerraformInstanceValues{
					Name:                util.Ptr("inst-c"),
					TerraformDefinition: &config_v0.TerraformDefinitionValues{Name: nil},
					AwsProvider:         &config_v0.AwsProviderValues{Name: nil},
					Status:              util.Ptr("Provisioning"),
					Age:                 util.Ptr("1h"),
				}},
			},
			wants:    []string{"inst-c", "Provisioning", "1h"},
			notWants: []string{"<nil>"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke helper and capture stdout
			instances := tc.instances
			out, err := captureStdout(t, func() error {
				return outputGetv0TerraformInstancesCmd(&instances)
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
