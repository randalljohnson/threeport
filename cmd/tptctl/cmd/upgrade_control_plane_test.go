package cmd

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	installer "github.com/threeport/threeport/pkg/threeport-installer/v0"
)

// TestUpgradeControlPlaneCmdMetadata asserts UpgradeControlPlaneCmd's Use,
// Example, Short, Long, SilenceUsage, PreRun, and Run wiring match the intended
// CLI shape.
func TestUpgradeControlPlaneCmdMetadata(t *testing.T) {
	// verify Use matches the CLI invocation token
	if UpgradeControlPlaneCmd.Use != "control-plane" {
		t.Errorf("UpgradeControlPlaneCmd.Use = %q, want %q", UpgradeControlPlaneCmd.Use, "control-plane")
	}
	// verify Example carries the documented invocation
	if !strings.Contains(UpgradeControlPlaneCmd.Example, "tptctl upgrade control-plane") {
		t.Errorf("UpgradeControlPlaneCmd.Example = %q, want to reference documented invocation", UpgradeControlPlaneCmd.Example)
	}
	// verify Short and Long descriptions are populated for cobra help
	if UpgradeControlPlaneCmd.Short == "" {
		t.Errorf("UpgradeControlPlaneCmd.Short is empty, want non-empty description")
	}
	if UpgradeControlPlaneCmd.Long == "" {
		t.Errorf("UpgradeControlPlaneCmd.Long is empty, want non-empty description")
	}
	// verify SilenceUsage suppresses usage on error
	if !UpgradeControlPlaneCmd.SilenceUsage {
		t.Errorf("UpgradeControlPlaneCmd.SilenceUsage = false, want true")
	}
	// verify PreRun and Run are wired
	if UpgradeControlPlaneCmd.PreRun == nil {
		t.Errorf("UpgradeControlPlaneCmd.PreRun = nil, want CommandPreRunFunc")
	}
	if UpgradeControlPlaneCmd.Run == nil {
		t.Errorf("UpgradeControlPlaneCmd.Run = nil, want a function")
	}
}

// TestUpgradeControlPlaneCmdRegistered asserts UpgradeControlPlaneCmd is
// attached under UpgradeCmd so `tptctl upgrade control-plane` resolves.
func TestUpgradeControlPlaneCmdRegistered(t *testing.T) {
	// verify subcommand registration on the upgrade parent
	if !hasSubcommand(UpgradeCmd, UpgradeControlPlaneCmd) {
		t.Errorf("UpgradeControlPlaneCmd not registered under UpgradeCmd")
	}
}

// TestUpgradeControlPlaneCmdVersionFlag asserts the --version flag is
// registered with the -t shorthand and marked required.
func TestUpgradeControlPlaneCmdVersionFlag(t *testing.T) {
	// verify the flag is present
	flag := UpgradeControlPlaneCmd.Flags().Lookup("version")
	if flag == nil {
		t.Fatalf("expected --version flag on UpgradeControlPlaneCmd, not found")
	}
	// verify the -t shorthand is bound
	if flag.Shorthand != "t" {
		t.Errorf("--version shorthand = %q, want %q", flag.Shorthand, "t")
	}
	// verify cobra marked the flag required via BashCompOneRequiredFlag annotation
	required, ok := flag.Annotations["cobra_annotation_bash_completion_one_required_flag"]
	if !ok || len(required) == 0 || required[0] != "true" {
		t.Errorf("--version flag not marked required, annotations = %v", flag.Annotations)
	}
}

// newDeployment builds an *unstructured.Unstructured populated with the given
// spec content so table-driven cases can vary just the interesting fields.
func newDeployment(spec map[string]interface{}) *unstructured.Unstructured {
	d := &unstructured.Unstructured{}
	obj := map[string]interface{}{}
	if spec != nil {
		obj["spec"] = spec
	}
	d.SetUnstructuredContent(obj)
	return d
}

// TestUpdateImageTagInDeploymentRestApiHappyPath covers the rest-api branch
// where both the main container and the db-migrator init container image tags
// are rewritten to the package-level updateImageTag.
func TestUpdateImageTagInDeploymentRestApiHappyPath(t *testing.T) {
	// arrange: set the package-var image tag the function reads
	prev := updateImageTag
	updateImageTag = "v9.9.9"
	defer func() { updateImageTag = prev }()

	deployment := newDeployment(map[string]interface{}{
		"template": map[string]interface{}{
			"spec": map[string]interface{}{
				"initContainers": []interface{}{
					map[string]interface{}{"name": "init-0", "image": "init0-img:old"},
					map[string]interface{}{"name": "db-migrator", "image": "registry.example/db-migrator:old"},
				},
				"containers": []interface{}{
					map[string]interface{}{"name": "rest-api", "image": "registry.example/rest-api:old"},
				},
			},
		},
	})

	// act: rewrite rest-api container image plus db-migrator init image
	if err := updateImageTagInDeployment(deployment, "ignored-because-func-reads-package-var", installer.ThreeportRestApi.Name); err != nil {
		t.Fatalf("updateImageTagInDeployment returned unexpected error: %v", err)
	}

	// assert: main container image was retagged to the new version
	containers := deployment.Object["spec"].(map[string]interface{})["template"].(map[string]interface{})["spec"].(map[string]interface{})["containers"].([]interface{})
	mainImage := containers[0].(map[string]interface{})["image"].(string)
	if mainImage != "registry.example/rest-api:v9.9.9" {
		t.Errorf("main container image = %q, want %q", mainImage, "registry.example/rest-api:v9.9.9")
	}
	// assert: db-migrator init container (index 1) was retagged
	inits := deployment.Object["spec"].(map[string]interface{})["template"].(map[string]interface{})["spec"].(map[string]interface{})["initContainers"].([]interface{})
	dbImage := inits[1].(map[string]interface{})["image"].(string)
	if dbImage != "registry.example/db-migrator:v9.9.9" {
		t.Errorf("db-migrator init image = %q, want %q", dbImage, "registry.example/db-migrator:v9.9.9")
	}
}

// TestUpdateImageTagInDeploymentAgentUsesSecondContainer verifies the
// containerIndex=1 branch that fires for the agent deployment.
func TestUpdateImageTagInDeploymentAgentUsesSecondContainer(t *testing.T) {
	// arrange: set the package-var image tag
	prev := updateImageTag
	updateImageTag = "v1.2.3"
	defer func() { updateImageTag = prev }()

	deployment := newDeployment(map[string]interface{}{
		"template": map[string]interface{}{
			"spec": map[string]interface{}{
				"containers": []interface{}{
					map[string]interface{}{"name": "kube-rbac-proxy", "image": "proxy:old"},
					map[string]interface{}{"name": "agent", "image": "agent:old"},
				},
			},
		},
	})

	// act: agent path should target containerSpec[1]
	if err := updateImageTagInDeployment(deployment, "unused", installer.ThreeportAgent.Name); err != nil {
		t.Fatalf("updateImageTagInDeployment returned unexpected error: %v", err)
	}

	// assert: only the second container's image was rewritten
	containers := deployment.Object["spec"].(map[string]interface{})["template"].(map[string]interface{})["spec"].(map[string]interface{})["containers"].([]interface{})
	if got := containers[0].(map[string]interface{})["image"].(string); got != "proxy:old" {
		t.Errorf("proxy container image = %q, want unchanged %q", got, "proxy:old")
	}
	if got := containers[1].(map[string]interface{})["image"].(string); got != "agent:v1.2.3" {
		t.Errorf("agent container image = %q, want %q", got, "agent:v1.2.3")
	}
}

// TestUpdateImageTagInDeploymentControllerFirstContainer covers the default
// containerIndex=0 branch used for every non-agent, non-rest-api component.
func TestUpdateImageTagInDeploymentControllerFirstContainer(t *testing.T) {
	// arrange: set the package-var image tag
	prev := updateImageTag
	updateImageTag = "v0.0.42"
	defer func() { updateImageTag = prev }()

	deployment := newDeployment(map[string]interface{}{
		"template": map[string]interface{}{
			"spec": map[string]interface{}{
				"containers": []interface{}{
					map[string]interface{}{"name": "workload-controller", "image": "ctrl:old"},
				},
			},
		},
	})

	// act: default path retags containerSpec[0]
	if err := updateImageTagInDeployment(deployment, "unused", "workload-controller"); err != nil {
		t.Fatalf("updateImageTagInDeployment returned unexpected error: %v", err)
	}

	// assert: controller image was retagged and initContainers was reset to empty slice
	containers := deployment.Object["spec"].(map[string]interface{})["template"].(map[string]interface{})["spec"].(map[string]interface{})["containers"].([]interface{})
	if got := containers[0].(map[string]interface{})["image"].(string); got != "ctrl:v0.0.42" {
		t.Errorf("controller image = %q, want %q", got, "ctrl:v0.0.42")
	}
	inits := deployment.Object["spec"].(map[string]interface{})["template"].(map[string]interface{})["spec"].(map[string]interface{})["initContainers"]
	initSlice, ok := inits.([]interface{})
	if !ok {
		t.Fatalf("initContainers not a slice, got %T", inits)
	}
	if len(initSlice) != 0 {
		t.Errorf("initContainers = %v, want empty slice on non-rest-api path", initSlice)
	}
}

// TestUpdateImageTagInDeploymentErrors covers every early-return error branch:
// missing keys, wrong types, and the rest-api-specific initContainers guard.
func TestUpdateImageTagInDeploymentErrors(t *testing.T) {
	cases := []struct {
		// name identifies the failure branch under test
		name string
		// build produces the malformed deployment
		build func() *unstructured.Unstructured
		// componentName drives the code path (rest-api vs generic)
		componentName string
		// wantSubstr must appear in the returned error message
		wantSubstr string
	}{
		{
			// spec key absent from unstructured content
			name:          "missing spec",
			build:         func() *unstructured.Unstructured { return newDeployment(nil) },
			componentName: "any",
			wantSubstr:    "could not find spec",
		},
		{
			// spec value is not a map[string]interface{}
			name: "spec wrong type",
			build: func() *unstructured.Unstructured {
				d := &unstructured.Unstructured{}
				d.SetUnstructuredContent(map[string]interface{}{"spec": "not-a-map"})
				return d
			},
			componentName: "any",
			wantSubstr:    "could not type convert deployment spec",
		},
		{
			// template key absent from spec
			name: "missing template",
			build: func() *unstructured.Unstructured {
				return newDeployment(map[string]interface{}{})
			},
			componentName: "any",
			wantSubstr:    "could not find template",
		},
		{
			// template value is not a map
			name: "template wrong type",
			build: func() *unstructured.Unstructured {
				return newDeployment(map[string]interface{}{"template": "nope"})
			},
			componentName: "any",
			wantSubstr:    "could not type convert template",
		},
		{
			// template.spec key absent
			name: "missing template spec",
			build: func() *unstructured.Unstructured {
				return newDeployment(map[string]interface{}{"template": map[string]interface{}{}})
			},
			componentName: "any",
			wantSubstr:    "could not find spec in template",
		},
		{
			// template.spec value is not a map
			name: "template spec wrong type",
			build: func() *unstructured.Unstructured {
				return newDeployment(map[string]interface{}{
					"template": map[string]interface{}{"spec": 42},
				})
			},
			componentName: "any",
			wantSubstr:    "could not type convert template spec",
		},
		{
			// containers key absent from template.spec
			name: "missing containers",
			build: func() *unstructured.Unstructured {
				return newDeployment(map[string]interface{}{
					"template": map[string]interface{}{"spec": map[string]interface{}{}},
				})
			},
			componentName: "any",
			wantSubstr:    "could not find containers",
		},
		{
			// rest-api path with a non-slice initContainers value
			name: "rest-api init containers wrong type",
			build: func() *unstructured.Unstructured {
				return newDeployment(map[string]interface{}{
					"template": map[string]interface{}{
						"spec": map[string]interface{}{
							"containers":     []interface{}{map[string]interface{}{"image": "x:old"}},
							"initContainers": "not-a-slice",
						},
					},
				})
			},
			componentName: installer.ThreeportRestApi.Name,
			wantSubstr:    "could not type convert init container list",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// arrange: pin a valid image tag so the error is about structure, not tag
			prev := updateImageTag
			updateImageTag = "v-test"
			defer func() { updateImageTag = prev }()

			// act: exercise the specific error branch
			err := updateImageTagInDeployment(tc.build(), "ignored", tc.componentName)

			// assert: the branch reported an error whose message identifies it
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantSubstr)
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Errorf("error message = %q, want to contain %q", err.Error(), tc.wantSubstr)
			}
		})
	}
}
