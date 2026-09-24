package v0

import (
	"errors"
	"testing"

	v0 "github.com/threeport/threeport/pkg/api/v0"
)

// newTestInstaller builds a ControlPlaneInstaller with fresh component pointers
// so mutations do not leak into the package-level default components.
func newTestInstaller(controllerCount int) *ControlPlaneInstaller {
	controllers := make([]*v0.ControlPlaneComponent, controllerCount)
	for i := range controllers {
		controllers[i] = &v0.ControlPlaneComponent{
			Name: "controller",
		}
	}
	return &ControlPlaneInstaller{
		Opts: Options{
			ControllerList:       controllers,
			RestApiInfo:          &v0.ControlPlaneComponent{Name: "rest-api"},
			AgentInfo:            &v0.ControlPlaneComponent{Name: "agent"},
			DatabaseMigratorInfo: &v0.ControlPlaneComponent{Name: "migrator"},
		},
	}
}

// snapshotDefaults captures the mutable pointer targets in defaultInstallerOptions
// so tests that call NewInstaller can restore the package-level defaults.
func snapshotDefaults(t *testing.T) {
	t.Helper()
	savedControllers := append([]*v0.ControlPlaneComponent{}, defaultInstallerOptions.ControllerList...)
	saved := defaultInstallerOptions
	savedRestApi := *defaultInstallerOptions.RestApiInfo
	savedAgent := *defaultInstallerOptions.AgentInfo
	savedMigrator := *defaultInstallerOptions.DatabaseMigratorInfo
	t.Cleanup(func() {
		defaultInstallerOptions = saved
		defaultInstallerOptions.ControllerList = savedControllers
		*defaultInstallerOptions.RestApiInfo = savedRestApi
		*defaultInstallerOptions.AgentInfo = savedAgent
		*defaultInstallerOptions.DatabaseMigratorInfo = savedMigrator
	})
}

// TestSetAllImageRepo asserts that SetAllImageRepo() writes the given image
// namespace onto every controller, the rest api, the agent, and the database
// migrator components.
func TestSetAllImageRepo(t *testing.T) {
	tests := []struct {
		name  string
		count int
		repo  string
	}{
		{name: "sets image repo across multiple controllers", count: 3, repo: "example.io/threeport"},
		{name: "sets image repo with empty controller list", count: 0, repo: "example.io/threeport"},
		{name: "accepts empty image repo string", count: 2, repo: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// build a fresh installer so component pointers are test-local
			cpi := newTestInstaller(tc.count)

			// apply the repo change to every managed component
			cpi.SetAllImageRepo(tc.repo)

			// each controller must carry the new namespace
			for i, c := range cpi.Opts.ControllerList {
				if c.ImageNamespace != tc.repo {
					t.Errorf("controller[%d] ImageNamespace = %q, want %q", i, c.ImageNamespace, tc.repo)
				}
			}
			// the rest api, agent and migrator must also carry it
			if cpi.Opts.RestApiInfo.ImageNamespace != tc.repo {
				t.Errorf("RestApiInfo.ImageNamespace = %q, want %q", cpi.Opts.RestApiInfo.ImageNamespace, tc.repo)
			}
			if cpi.Opts.AgentInfo.ImageNamespace != tc.repo {
				t.Errorf("AgentInfo.ImageNamespace = %q, want %q", cpi.Opts.AgentInfo.ImageNamespace, tc.repo)
			}
			if cpi.Opts.DatabaseMigratorInfo.ImageNamespace != tc.repo {
				t.Errorf("DatabaseMigratorInfo.ImageNamespace = %q, want %q", cpi.Opts.DatabaseMigratorInfo.ImageNamespace, tc.repo)
			}
		})
	}
}

// TestSetAllImageTags asserts that SetAllImageTags() writes the given image
// tag onto every controller, the rest api, the agent, and the database
// migrator components.
func TestSetAllImageTags(t *testing.T) {
	tests := []struct {
		name  string
		count int
		tag   string
	}{
		{name: "sets image tag across multiple controllers", count: 3, tag: "v1.2.3"},
		{name: "sets image tag with empty controller list", count: 0, tag: "v1.2.3"},
		{name: "accepts empty image tag string", count: 2, tag: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// build a fresh installer so component pointers are test-local
			cpi := newTestInstaller(tc.count)

			// apply the tag change to every managed component
			cpi.SetAllImageTags(tc.tag)

			// each controller must carry the new tag
			for i, c := range cpi.Opts.ControllerList {
				if c.ImageTag != tc.tag {
					t.Errorf("controller[%d] ImageTag = %q, want %q", i, c.ImageTag, tc.tag)
				}
			}
			// the rest api, agent and migrator must also carry it
			if cpi.Opts.RestApiInfo.ImageTag != tc.tag {
				t.Errorf("RestApiInfo.ImageTag = %q, want %q", cpi.Opts.RestApiInfo.ImageTag, tc.tag)
			}
			if cpi.Opts.AgentInfo.ImageTag != tc.tag {
				t.Errorf("AgentInfo.ImageTag = %q, want %q", cpi.Opts.AgentInfo.ImageTag, tc.tag)
			}
			if cpi.Opts.DatabaseMigratorInfo.ImageTag != tc.tag {
				t.Errorf("DatabaseMigratorInfo.ImageTag = %q, want %q", cpi.Opts.DatabaseMigratorInfo.ImageTag, tc.tag)
			}
		})
	}
}

// TestNameOption asserts that Name() returns a functional option that sets
// Options.Name to the provided string.
func TestNameOption(t *testing.T) {
	// applying the option should overwrite the current Name
	opts := &Options{Name: "old"}
	Name("threeport-dev")(opts)
	if opts.Name != "threeport-dev" {
		t.Errorf("Name option: got %q, want %q", opts.Name, "threeport-dev")
	}

	// an empty string is a valid override
	Name("")(opts)
	if opts.Name != "" {
		t.Errorf("Name option with empty string: got %q, want %q", opts.Name, "")
	}
}

// TestNamespaceOption asserts that Namespace() returns a functional option
// that sets Options.Namespace to the provided string.
func TestNamespaceOption(t *testing.T) {
	// applying the option should overwrite the current Namespace
	opts := &Options{Namespace: "old"}
	Namespace("kube-system")(opts)
	if opts.Namespace != "kube-system" {
		t.Errorf("Namespace option: got %q, want %q", opts.Namespace, "kube-system")
	}
}

// TestRestApiOption asserts that RestApi() returns a functional option that
// assigns the given ControlPlaneComponent pointer to Options.RestApiInfo.
func TestRestApiOption(t *testing.T) {
	opts := &Options{}
	comp := &v0.ControlPlaneComponent{Name: "custom-rest-api"}

	// applying the option should install the provided pointer
	RestApi(comp)(opts)
	if opts.RestApiInfo != comp {
		t.Errorf("RestApi option: pointer was not assigned to RestApiInfo")
	}

	// nil is accepted (used to represent no override)
	RestApi(nil)(opts)
	if opts.RestApiInfo != nil {
		t.Errorf("RestApi option with nil: got non-nil pointer")
	}
}

// TestCustomControllerOption asserts that CustomController() appends the given
// component to Options.ControllerList without replacing existing entries.
func TestCustomControllerOption(t *testing.T) {
	// start with one existing controller so we can prove append semantics
	existing := &v0.ControlPlaneComponent{Name: "existing"}
	opts := &Options{ControllerList: []*v0.ControlPlaneComponent{existing}}

	added := &v0.ControlPlaneComponent{Name: "added"}
	CustomController(added)(opts)

	// the list should now contain both, in order
	if len(opts.ControllerList) != 2 {
		t.Fatalf("ControllerList length: got %d, want 2", len(opts.ControllerList))
	}
	if opts.ControllerList[0] != existing || opts.ControllerList[1] != added {
		t.Errorf("CustomController: list order/content wrong: %+v", opts.ControllerList)
	}
}

// TestCustomControllersOption asserts that CustomControllers() appends every
// component in the slice to Options.ControllerList.
func TestCustomControllersOption(t *testing.T) {
	tests := []struct {
		name     string
		existing []*v0.ControlPlaneComponent
		added    []*v0.ControlPlaneComponent
		wantLen  int
	}{
		{
			name:     "appends multiple controllers",
			existing: []*v0.ControlPlaneComponent{{Name: "a"}},
			added:    []*v0.ControlPlaneComponent{{Name: "b"}, {Name: "c"}},
			wantLen:  3,
		},
		{
			name:     "appends nothing when input slice is empty",
			existing: []*v0.ControlPlaneComponent{{Name: "a"}},
			added:    []*v0.ControlPlaneComponent{},
			wantLen:  1,
		},
		{
			name:     "appends into a nil starting list",
			existing: nil,
			added:    []*v0.ControlPlaneComponent{{Name: "b"}},
			wantLen:  1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := &Options{ControllerList: tc.existing}

			// apply the batch append option
			CustomControllers(tc.added)(opts)

			// verify total length after append
			if len(opts.ControllerList) != tc.wantLen {
				t.Errorf("ControllerList length: got %d, want %d", len(opts.ControllerList), tc.wantLen)
			}
		})
	}
}

// TestPreInstallFunctionOption asserts that PreInstallFunction() assigns the
// provided hook and that invoking the stored hook returns its error.
func TestPreInstallFunctionOption(t *testing.T) {
	opts := &Options{}
	sentinel := errors.New("pre install failure")
	hook := func(_ *v0.KubernetesRuntimeInstance, _ *ControlPlaneInstaller) error {
		return sentinel
	}

	// applying the option should store the hook
	PreInstallFunction(hook)(opts)
	if opts.PreInstallFunction == nil {
		t.Fatalf("PreInstallFunction option: hook was not stored")
	}

	// calling the stored hook should surface its return value
	if got := opts.PreInstallFunction(nil, nil); !errors.Is(got, sentinel) {
		t.Errorf("stored PreInstallFunction returned %v, want %v", got, sentinel)
	}
}

// TestPostInstallFunctionOption asserts that PostInstallFunction() assigns
// the provided hook and that invoking the stored hook returns its error.
func TestPostInstallFunctionOption(t *testing.T) {
	opts := &Options{}
	sentinel := errors.New("post install failure")
	hook := func(_ *v0.KubernetesRuntimeInstance, _ *ControlPlaneInstaller) error {
		return sentinel
	}

	// applying the option should store the hook
	PostInstallFunction(hook)(opts)
	if opts.PostInstallFunction == nil {
		t.Fatalf("PostInstallFunction option: hook was not stored")
	}

	// calling the stored hook should surface its return value
	if got := opts.PostInstallFunction(nil, nil); !errors.Is(got, sentinel) {
		t.Errorf("stored PostInstallFunction returned %v, want %v", got, sentinel)
	}
}

// TestNewInstallerDefaults asserts that NewInstaller() returns an installer
// whose Options carry the package-level defaults when no option overrides
// are supplied.
func TestNewInstallerDefaults(t *testing.T) {
	// tests below mutate defaultInstallerOptions in place; snapshot to restore
	snapshotDefaults(t)

	cpi := NewInstaller()

	// the default Name and Namespace should come from the package constants
	if cpi.Opts.Name != ControlPlaneName {
		t.Errorf("default Name: got %q, want %q", cpi.Opts.Name, ControlPlaneName)
	}
	if cpi.Opts.Namespace != ControlPlaneNamespace {
		t.Errorf("default Namespace: got %q, want %q", cpi.Opts.Namespace, ControlPlaneNamespace)
	}
	// the default pre/post install hooks must be non-nil and callable
	if cpi.Opts.PreInstallFunction == nil || cpi.Opts.PostInstallFunction == nil {
		t.Fatal("default install hooks should not be nil")
	}
	if err := cpi.Opts.PreInstallFunction(nil, nil); err != nil {
		t.Errorf("default PreInstallFunction returned %v, want nil", err)
	}
	if err := cpi.Opts.PostInstallFunction(nil, nil); err != nil {
		t.Errorf("default PostInstallFunction returned %v, want nil", err)
	}
}

// TestNewInstallerAppliesOptions asserts that NewInstaller() applies each
// supplied InstallerOption to the returned installer's Options.
func TestNewInstallerAppliesOptions(t *testing.T) {
	// NewInstaller mutates defaultInstallerOptions in place; snapshot to restore
	snapshotDefaults(t)

	customRestApi := &v0.ControlPlaneComponent{Name: "custom-rest-api"}
	cpi := NewInstaller(
		Name("custom-name"),
		Namespace("custom-ns"),
		RestApi(customRestApi),
	)

	// the options should reflect each override applied
	if cpi.Opts.Name != "custom-name" {
		t.Errorf("Name: got %q, want %q", cpi.Opts.Name, "custom-name")
	}
	if cpi.Opts.Namespace != "custom-ns" {
		t.Errorf("Namespace: got %q, want %q", cpi.Opts.Namespace, "custom-ns")
	}
	if cpi.Opts.RestApiInfo == nil || cpi.Opts.RestApiInfo.Name != "custom-rest-api" {
		t.Errorf("RestApiInfo: got %+v, want name %q", cpi.Opts.RestApiInfo, "custom-rest-api")
	}
}

// TestNewInstallerReturnsNonNil asserts NewInstaller() never returns a nil
// pointer even when called with no options.
func TestNewInstallerReturnsNonNil(t *testing.T) {
	// snapshot defaults since the call still runs option-application logic
	snapshotDefaults(t)

	// the return value must be a usable installer
	if got := NewInstaller(); got == nil {
		t.Fatal("NewInstaller returned nil")
	}
}
