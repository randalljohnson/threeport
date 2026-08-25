package apiserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/threeport/threeport/pkg/sdk/v0"
	"github.com/threeport/threeport/pkg/sdk/v0/gen"
)

// moduleFixtureGenerator returns a generator populated the way the SDK
// populates one for a module: a module path that is not the threeport project,
// and a single object group carrying a reconciled definition and instance.
func moduleFixtureGenerator() *gen.Generator {
	return &gen.Generator{
		Module:     true,
		ModulePath: "example.com/widget-module",
		ApiObjectGroups: []gen.ApiObjectGroup{
			{
				ControllerDomain: "Widget",
				ControllerName:   "widget-controller",
				ReconciledObjects: []gen.ReconciledObject{
					{Name: "WidgetDefinition", Versions: []string{"v0"}},
					{Name: "WidgetInstance", Versions: []string{"v0"}},
				},
				ApiObjects: []*gen.ApiObject{
					{TypeName: "WidgetDefinition", Version: "v0", Reconciler: true},
					{TypeName: "WidgetInstance", Version: "v0", Reconciler: true},
				},
			},
		},
	}
}

// moduleFixtureSdkConfig returns the SDK config fields the module registration
// generator reads.
func moduleFixtureSdkConfig() *sdk.SdkConfig {
	return &sdk.SdkConfig{
		ModuleName:   "Widget",
		ApiNamespace: "widget.example.com",
	}
}

// generateModuleRegistration runs the module registration generator with the
// working directory pointed at a scratch tree, then returns what it wrote.
// The generator writes to a path relative to the working directory, so the
// chdir keeps the run out of the repository.
func generateModuleRegistration(t *testing.T) string {
	t.Helper()

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to read working directory: %v", err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("failed to change to scratch directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Fatalf("failed to restore working directory: %v", err)
		}
	})

	if err := GenModuleRegistration(moduleFixtureGenerator(), moduleFixtureSdkConfig()); err != nil {
		t.Fatalf("GenModuleRegistration returned an error: %v", err)
	}

	generated, err := os.ReadFile(filepath.Join("pkg", "api-server", "v0", "module_gen.go"))
	if err != nil {
		t.Fatalf("failed to read generated module registration: %v", err)
	}

	return string(generated)
}

// TestGenModuleRegistrationEmitsModuleName asserts that the registration code a
// module gets carries the module name the control plane registers it under,
// built from the API namespace and the kebab-cased module name.
func TestGenModuleRegistrationEmitsModuleName(t *testing.T) {
	generated := generateModuleRegistration(t)

	// the name is what a second install of the same module looks itself up by,
	// so a change here silently orphans the record the first install created
	if want := `"widget.example.com/widget-module-api"`; !strings.Contains(generated, want) {
		t.Errorf("generated module registration does not declare module name %s", want)
	}
}

// TestGenModuleRegistrationImportsModuleRoutes asserts that the generated code
// reaches the module's own route package rather than the core one, which is
// what makes the output specific to the module it was generated for.
func TestGenModuleRegistrationImportsModuleRoutes(t *testing.T) {
	generated := generateModuleRegistration(t)

	if want := `"example.com/widget-module/pkg/api-server/v0/routes"`; !strings.Contains(generated, want) {
		t.Errorf("generated module registration does not import %s", want)
	}
}

// TestGenModuleRegistrationRegistersReconciledObjects asserts that every
// reconciled object and its controller get a registration block, since an
// object missing one is invisible to the control plane while still compiling.
func TestGenModuleRegistrationRegistersReconciledObjects(t *testing.T) {
	generated := generateModuleRegistration(t)

	// each lookup is the query the module makes against the control plane on
	// startup, so its absence means the object or controller never registers
	for _, want := range []string{
		`fmt.Sprintf("name=%s&moduleapiid=%d", "widget-controller"`,
		`"name=WidgetDefinition&moduleapiid=%d"`,
		`"name=WidgetInstance&moduleapiid=%d"`,
	} {
		if !strings.Contains(generated, want) {
			t.Errorf("generated module registration does not look up %s", want)
		}
	}
}

// TestGenModuleRegistrationHonorsExcludeFiles asserts that a project excluding
// the registration file gets no file written, which is how a module supplies
// its own registration by hand.
func TestGenModuleRegistrationHonorsExcludeFiles(t *testing.T) {
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to read working directory: %v", err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("failed to change to scratch directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Fatalf("failed to restore working directory: %v", err)
		}
	})

	generatedPath := filepath.Join("pkg", "api-server", "v0", "module_gen.go")
	sdkConfig := moduleFixtureSdkConfig()
	sdkConfig.ExcludeFiles = []string{generatedPath}

	if err := GenModuleRegistration(moduleFixtureGenerator(), sdkConfig); err != nil {
		t.Fatalf("GenModuleRegistration returned an error: %v", err)
	}

	if _, err := os.Stat(generatedPath); !os.IsNotExist(err) {
		t.Errorf("excluded file %s was written anyway", generatedPath)
	}
}
