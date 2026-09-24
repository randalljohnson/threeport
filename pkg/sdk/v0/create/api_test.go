package create

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/threeport/threeport/pkg/sdk/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// readGenFile returns the file contents at pkg/api/<version>/<group>.go under
// the current working directory, or fails the test if the file is missing. The
// production path in api.go writes to this exact relative location, so tests
// run under t.Chdir(tmp) to isolate the write from the real repo tree.
func readGenFile(t *testing.T, version, group string) string {
	t.Helper()
	path := filepath.Join("pkg", "api", version, group+".go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated file %s: %v", path, err)
	}
	return string(data)
}

// TestCreateApiObjects_EmptyConfig covers the top-level entry point when the
// SDK config carries no API object groups: the loops fall through and the
// function returns nil without touching the filesystem.
func TestCreateApiObjects_EmptyConfig(t *testing.T) {
	// isolate any accidental writes to a temp cwd
	t.Chdir(t.TempDir())

	// invoke the top-level entry point with an empty config
	cfg := &sdk.SdkConfig{
		ApiObjectConfig: sdk.ApiObjectConfig{
			ApiObjectGroups: nil,
		},
	}

	// expect a clean nil return
	if err := CreateApiObjects(cfg, false); err != nil {
		t.Fatalf("CreateApiObjects on empty config: %v", err)
	}

	// verify nothing was written under pkg/api
	if _, err := os.Stat("pkg"); !os.IsNotExist(err) {
		t.Errorf("expected no pkg dir to be created, stat err = %v", err)
	}
}

// TestCreateApiObjects_PlainObject covers the base case: a single non-reconcilable
// object that is not part of a defined-instance abstraction. The generated file
// must declare the struct with an embedded Common field carrying the
// swaggerignore + mapstructure tag, and must NOT include Reconciliation, Definition,
// Instance, or a foreign key field.
func TestCreateApiObjects_PlainObject(t *testing.T) {
	t.Chdir(t.TempDir())

	// build a config with one group carrying a single plain v0 object
	cfg := &sdk.SdkConfig{
		ApiObjectConfig: sdk.ApiObjectConfig{
			ApiObjectGroups: []*sdk.ApiObjectGroup{
				{
					Name: util.Ptr("widgets"),
					Objects: []*sdk.ApiObject{
						{
							Name:     util.Ptr("Widget"),
							Versions: []*string{util.Ptr("v0")},
						},
					},
				},
			},
		},
	}

	// invoke the top-level entry point
	if err := CreateApiObjects(cfg, false); err != nil {
		t.Fatalf("CreateApiObjects: %v", err)
	}

	// verify the generated file declares the struct with the Common embed
	src := readGenFile(t, "v0", "widgets")
	if !strings.Contains(src, "type Widget struct") {
		t.Errorf("expected 'type Widget struct' in generated file, got:\n%s", src)
	}
	if !strings.Contains(src, "Common ") {
		t.Errorf("expected embedded Common field in generated file, got:\n%s", src)
	}
	if !strings.Contains(src, `swaggerignore:"true"`) {
		t.Errorf("expected swaggerignore tag on Common, got:\n%s", src)
	}
	if !strings.Contains(src, `mapstructure:",squash"`) {
		t.Errorf("expected mapstructure squash tag on Common, got:\n%s", src)
	}

	// verify none of the reconcile / defined-instance surfaces leaked in
	for _, unwanted := range []string{"Reconciliation", "Definition", "Instance"} {
		if strings.Contains(src, unwanted) {
			t.Errorf("did not expect %q in plain-object output, got:\n%s", unwanted, src)
		}
	}
}

// TestCreateApiObjects_Reconcilable covers the branch where an object has
// Reconcilable set to true: the Reconciliation embed with mapstructure squash
// tag is appended to the struct.
func TestCreateApiObjects_Reconcilable(t *testing.T) {
	t.Chdir(t.TempDir())

	// object flagged reconcilable but not part of a defined-instance pair
	cfg := &sdk.SdkConfig{
		ApiObjectConfig: sdk.ApiObjectConfig{
			ApiObjectGroups: []*sdk.ApiObjectGroup{
				{
					Name: util.Ptr("gadgets"),
					Objects: []*sdk.ApiObject{
						{
							Name:         util.Ptr("Gadget"),
							Versions:     []*string{util.Ptr("v0")},
							Reconcilable: util.Ptr(true),
						},
					},
				},
			},
		},
	}

	if err := CreateApiObjects(cfg, false); err != nil {
		t.Fatalf("CreateApiObjects: %v", err)
	}

	// verify Reconciliation embed appears alongside Common
	src := readGenFile(t, "v0", "gadgets")
	if !strings.Contains(src, "Reconciliation ") {
		t.Errorf("expected embedded Reconciliation field, got:\n%s", src)
	}
	if !strings.Contains(src, "Common ") {
		t.Errorf("expected embedded Common field, got:\n%s", src)
	}
}

// TestCreateApiObjects_DefinedInstancePair covers the defined-instance branch:
// a group containing both a Definition and an Instance object triggers extra
// fields on each. The Definition file gets a slice of pluralized instances;
// the Instance file gets a foreign-key uint pointer back to the definition.
func TestCreateApiObjects_DefinedInstancePair(t *testing.T) {
	t.Chdir(t.TempDir())

	// group with a matched Definition + Instance pair
	cfg := &sdk.SdkConfig{
		ApiObjectConfig: sdk.ApiObjectConfig{
			ApiObjectGroups: []*sdk.ApiObjectGroup{
				{
					Name: util.Ptr("gizmos"),
					Objects: []*sdk.ApiObject{
						{
							Name:     util.Ptr("GizmoDefinition"),
							Versions: []*string{util.Ptr("v0")},
						},
						{
							Name:     util.Ptr("GizmoInstance"),
							Versions: []*string{util.Ptr("v0")},
						},
					},
				},
			},
		},
	}

	if err := CreateApiObjects(cfg, false); err != nil {
		t.Fatalf("CreateApiObjects: %v", err)
	}

	// both structs land in the same group file; read once and assert both branches
	src := readGenFile(t, "v0", "gizmos")

	// definition branch: Definition embed and pluralized slice back to instance
	if !strings.Contains(src, "type GizmoDefinition struct") {
		t.Errorf("expected GizmoDefinition struct, got:\n%s", src)
	}
	if !strings.Contains(src, "Definition ") {
		t.Errorf("expected embedded Definition field on GizmoDefinition, got:\n%s", src)
	}
	if !strings.Contains(src, "GizmoInstances []*GizmoInstance") {
		t.Errorf("expected pluralized GizmoInstances slice, got:\n%s", src)
	}
	if !strings.Contains(src, `json:"GizmoInstances,omitempty"`) {
		t.Errorf("expected GizmoInstances json tag, got:\n%s", src)
	}

	// instance branch: Instance embed and foreign-key uint pointer to definition
	if !strings.Contains(src, "type GizmoInstance struct") {
		t.Errorf("expected GizmoInstance struct, got:\n%s", src)
	}
	if !strings.Contains(src, "Instance ") {
		t.Errorf("expected embedded Instance field on GizmoInstance, got:\n%s", src)
	}
	if !strings.Contains(src, "GizmoDefinitionID *uint") {
		t.Errorf("expected GizmoDefinitionID *uint FK, got:\n%s", src)
	}
	if !strings.Contains(src, `json:"GizmoDefinitionID,omitempty"`) {
		t.Errorf("expected GizmoDefinitionID json tag, got:\n%s", src)
	}
	if !strings.Contains(src, `gorm:"not null"`) {
		t.Errorf("expected gorm not-null tag on FK, got:\n%s", src)
	}
}

// TestCreateApiObjects_DefinedInstanceOptOut covers the override where an
// object with a Definition/Instance suffix explicitly opts out of the
// defined-instance abstraction via DefinedInstance=false. The generated
// struct should carry only Common, no Definition/Instance embed, and no
// FK or instances slice.
func TestCreateApiObjects_DefinedInstanceOptOut(t *testing.T) {
	t.Chdir(t.TempDir())

	// both objects opt out, so no defined-instance fields should be emitted
	cfg := &sdk.SdkConfig{
		ApiObjectConfig: sdk.ApiObjectConfig{
			ApiObjectGroups: []*sdk.ApiObjectGroup{
				{
					Name: util.Ptr("optouts"),
					Objects: []*sdk.ApiObject{
						{
							Name:            util.Ptr("ThingDefinition"),
							Versions:        []*string{util.Ptr("v0")},
							DefinedInstance: util.Ptr(false),
						},
						{
							Name:            util.Ptr("ThingInstance"),
							Versions:        []*string{util.Ptr("v0")},
							DefinedInstance: util.Ptr(false),
						},
					},
				},
			},
		},
	}

	if err := CreateApiObjects(cfg, false); err != nil {
		t.Fatalf("CreateApiObjects: %v", err)
	}

	// verify neither struct picked up any defined-instance embeds or FK; the
	// embed emissions are tab-indented struct fields, so check for the
	// tab-prefixed forms so the type-name occurrences of these tokens
	// (ThingDefinition, ThingInstance) do not false-match.
	src := readGenFile(t, "v0", "optouts")
	for _, unwanted := range []string{
		"ThingDefinitionID",
		"ThingInstances",
		"\tDefinition ",
		"\tInstance ",
	} {
		if strings.Contains(src, unwanted) {
			t.Errorf("did not expect %q under opt-out, got:\n%s", unwanted, src)
		}
	}
}

// TestCreateApiObjects_Extension covers the extension flag path: fields that
// would embed local identifiers (Common, Reconciliation, Definition, Instance)
// switch to qualified references into pkg/api/v0 so an out-of-tree module
// pulls them from the upstream package.
func TestCreateApiObjects_Extension(t *testing.T) {
	t.Chdir(t.TempDir())

	// reconcilable object plus a defined-instance pair, all under extension mode
	cfg := &sdk.SdkConfig{
		ApiObjectConfig: sdk.ApiObjectConfig{
			ApiObjectGroups: []*sdk.ApiObjectGroup{
				{
					Name: util.Ptr("extgroup"),
					Objects: []*sdk.ApiObject{
						{
							Name:         util.Ptr("ExtWidget"),
							Versions:     []*string{util.Ptr("v0")},
							Reconcilable: util.Ptr(true),
						},
						{
							Name:     util.Ptr("ExtGizmoDefinition"),
							Versions: []*string{util.Ptr("v0")},
						},
						{
							Name:     util.Ptr("ExtGizmoInstance"),
							Versions: []*string{util.Ptr("v0")},
						},
					},
				},
			},
		},
	}

	if err := CreateApiObjects(cfg, true); err != nil {
		t.Fatalf("CreateApiObjects extension: %v", err)
	}

	// verify the import alias declared in createNewApiFile appears
	src := readGenFile(t, "v0", "extgroup")
	if !strings.Contains(src, "tpapi_v0 \"github.com/threeport/threeport/pkg/api/v0\"") {
		t.Errorf("expected tpapi_v0 import alias, got:\n%s", src)
	}

	// qualified Common and Reconciliation on the reconcilable object
	if !strings.Contains(src, "tpapi_v0.Common") {
		t.Errorf("expected qualified tpapi_v0.Common, got:\n%s", src)
	}
	if !strings.Contains(src, "tpapi_v0.Reconciliation") {
		t.Errorf("expected qualified tpapi_v0.Reconciliation, got:\n%s", src)
	}

	// qualified Definition + Instance embeds on the paired objects
	if !strings.Contains(src, "tpapi_v0.Definition") {
		t.Errorf("expected qualified tpapi_v0.Definition, got:\n%s", src)
	}
	if !strings.Contains(src, "tpapi_v0.Instance") {
		t.Errorf("expected qualified tpapi_v0.Instance, got:\n%s", src)
	}

	// FK back to the definition still uses local identifiers (no qualification)
	if !strings.Contains(src, "ExtGizmoDefinitionID *uint") {
		t.Errorf("expected ExtGizmoDefinitionID FK on instance, got:\n%s", src)
	}
}

// TestCreateApiObjects_MultipleVersions covers the outer loop over versions:
// one object emitted for two versions writes into pkg/api/v0/... and
// pkg/api/v1/... under the shared group name.
func TestCreateApiObjects_MultipleVersions(t *testing.T) {
	t.Chdir(t.TempDir())

	// single object declared for both v0 and v1
	cfg := &sdk.SdkConfig{
		ApiObjectConfig: sdk.ApiObjectConfig{
			ApiObjectGroups: []*sdk.ApiObjectGroup{
				{
					Name: util.Ptr("multi"),
					Objects: []*sdk.ApiObject{
						{
							Name:     util.Ptr("Widget"),
							Versions: []*string{util.Ptr("v0"), util.Ptr("v1")},
						},
					},
				},
			},
		},
	}

	if err := CreateApiObjects(cfg, false); err != nil {
		t.Fatalf("CreateApiObjects: %v", err)
	}

	// verify a file was written under each version directory
	for _, version := range []string{"v0", "v1"} {
		src := readGenFile(t, version, "multi")
		if !strings.Contains(src, "package "+version) {
			t.Errorf("expected 'package %s' header, got:\n%s", version, src)
		}
		if !strings.Contains(src, "type Widget struct") {
			t.Errorf("expected Widget struct in %s file, got:\n%s", version, src)
		}
	}
}

// TestCreateApiObjects_ExistingFileNotOverwritten covers the presence check
// inside WriteCodeToFile: when the target file already exists, the generator
// leaves it untouched (this is the "scaffold once" contract for these files).
func TestCreateApiObjects_ExistingFileNotOverwritten(t *testing.T) {
	t.Chdir(t.TempDir())

	// pre-create the target file with sentinel content so we can detect any overwrite
	sentinel := "// sentinel: pre-existing scaffold content\n"
	dir := filepath.Join("pkg", "api", "v0")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sentinel dir: %v", err)
	}
	path := filepath.Join(dir, "widgets.go")
	if err := os.WriteFile(path, []byte(sentinel), 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	// invoke the generator against the pre-existing target
	cfg := &sdk.SdkConfig{
		ApiObjectConfig: sdk.ApiObjectConfig{
			ApiObjectGroups: []*sdk.ApiObjectGroup{
				{
					Name: util.Ptr("widgets"),
					Objects: []*sdk.ApiObject{
						{
							Name:     util.Ptr("Widget"),
							Versions: []*string{util.Ptr("v0")},
						},
					},
				},
			},
		},
	}
	if err := CreateApiObjects(cfg, false); err != nil {
		t.Fatalf("CreateApiObjects on pre-existing target: %v", err)
	}

	// verify the sentinel content survives; the file was not overwritten
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after generate: %v", err)
	}
	if string(got) != sentinel {
		t.Errorf("expected sentinel content preserved, got:\n%s", string(got))
	}
}
