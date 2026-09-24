package v0

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeGoMod writes a go.mod with the given module path into dir. Tests use
// this to control the outcome of util.IsModule(), which reads ./go.mod from
// the current working directory. Passing the threeport module path yields
// "not a module"; any other path yields "is a module".
func writeGoMod(t *testing.T, dir, modulePath string) {
	t.Helper()
	content := "module " + modulePath + "\n\ngo 1.22\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(content), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
}

// chdirWithGoMod places the test's cwd inside a temp dir containing a
// controlled go.mod. Callers pick the module path to steer IsModule().
func chdirWithGoMod(t *testing.T, modulePath string) string {
	t.Helper()
	dir := t.TempDir()
	writeGoMod(t, dir, modulePath)
	t.Chdir(dir)
	return dir
}

// strPtr and boolPtr keep the test literals compact.
func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }

// TestIsOfDefinedInstance_NoSuffix covers an object whose name has neither
// the "Definition" nor "Instance" suffix. The function's switch falls
// through and returns (false, "", "").
func TestIsOfDefinedInstance_NoSuffix(t *testing.T) {
	// setup: a group containing a single non-defined-instance object
	objs := []*ApiObject{{Name: strPtr("Widget")}}

	// action: query IsOfDefinedInstance for the plain name
	got, defName, instName := IsOfDefinedInstance("Widget", objs)

	// assertion: not part of a defined-instance abstraction and both
	// returned names are empty
	if got || defName != "" || instName != "" {
		t.Fatalf("want (false,\"\",\"\"), got (%v,%q,%q)", got, defName, instName)
	}
}

// TestIsOfDefinedInstance_DefinitionWithInstance covers the happy path for
// a Definition-suffixed object whose matching Instance sibling exists.
func TestIsOfDefinedInstance_DefinitionWithInstance(t *testing.T) {
	// setup: a definition/instance pair with no explicit DefinedInstance flag
	objs := []*ApiObject{
		{Name: strPtr("WidgetDefinition")},
		{Name: strPtr("WidgetInstance")},
	}

	// action: query for the definition name
	got, defName, instName := IsOfDefinedInstance("WidgetDefinition", objs)

	// assertion: recognized as defined-instance with correct pair names
	if !got {
		t.Fatal("want defined-instance true, got false")
	}
	if defName != "WidgetDefinition" || instName != "WidgetInstance" {
		t.Fatalf("want (WidgetDefinition,WidgetInstance), got (%q,%q)", defName, instName)
	}
}

// TestIsOfDefinedInstance_InstanceWithDefinition covers the mirror of the
// happy path: an Instance-suffixed object whose Definition sibling exists.
func TestIsOfDefinedInstance_InstanceWithDefinition(t *testing.T) {
	// setup: a definition/instance pair
	objs := []*ApiObject{
		{Name: strPtr("WidgetDefinition")},
		{Name: strPtr("WidgetInstance")},
	}

	// action: query for the instance name
	got, defName, instName := IsOfDefinedInstance("WidgetInstance", objs)

	// assertion: recognized as defined-instance with correct pair names
	if !got {
		t.Fatal("want defined-instance true, got false")
	}
	if defName != "WidgetDefinition" || instName != "WidgetInstance" {
		t.Fatalf("want (WidgetDefinition,WidgetInstance), got (%q,%q)", defName, instName)
	}
}

// TestIsOfDefinedInstance_DefinitionMissingInstance covers a
// Definition-suffixed object with no corresponding Instance sibling: the
// function returns false because the abstraction cannot form.
func TestIsOfDefinedInstance_DefinitionMissingInstance(t *testing.T) {
	// setup: only the Definition side exists in the group
	objs := []*ApiObject{{Name: strPtr("WidgetDefinition")}}

	// action: query for the definition name
	got, defName, instName := IsOfDefinedInstance("WidgetDefinition", objs)

	// assertion: not defined-instance, empty pair names
	if got || defName != "" || instName != "" {
		t.Fatalf("want (false,\"\",\"\"), got (%v,%q,%q)", got, defName, instName)
	}
}

// TestIsOfDefinedInstance_InstanceMissingDefinition covers the mirror gap:
// an Instance object with no Definition sibling in the group.
func TestIsOfDefinedInstance_InstanceMissingDefinition(t *testing.T) {
	// setup: only the Instance side exists in the group
	objs := []*ApiObject{{Name: strPtr("WidgetInstance")}}

	// action: query for the instance name
	got, defName, instName := IsOfDefinedInstance("WidgetInstance", objs)

	// assertion: not defined-instance, empty pair names
	if got || defName != "" || instName != "" {
		t.Fatalf("want (false,\"\",\"\"), got (%v,%q,%q)", got, defName, instName)
	}
}

// TestIsOfDefinedInstance_ExplicitOptOut covers an object whose config
// explicitly sets DefinedInstance:false. The early return in the pre-switch
// loop must fire even when Definition/Instance siblings would otherwise
// pair up.
func TestIsOfDefinedInstance_ExplicitOptOut(t *testing.T) {
	// setup: a Definition/Instance pair where the Definition side opts out
	objs := []*ApiObject{
		{Name: strPtr("WidgetDefinition"), DefinedInstance: boolPtr(false)},
		{Name: strPtr("WidgetInstance")},
	}

	// action: query for the opted-out Definition name
	got, defName, instName := IsOfDefinedInstance("WidgetDefinition", objs)

	// assertion: opt-out short-circuits before the suffix switch
	if got || defName != "" || instName != "" {
		t.Fatalf("want (false,\"\",\"\"), got (%v,%q,%q)", got, defName, instName)
	}
}

// TestApiObjectFromGroup_Found covers the lookup happy path: a single
// matching object gets returned by value.
func TestApiObjectFromGroup_Found(t *testing.T) {
	// setup: a group with two distinctly-named objects
	group := &ApiObjectGroup{
		Name: strPtr("things"),
		Objects: []*ApiObject{
			{Name: strPtr("Widget")},
			{Name: strPtr("Gadget")},
		},
	}

	// action: look up one of them by name
	obj, err := ApiObjectFromGroup("Gadget", group)

	// assertion: no error and the returned object carries the queried name
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if obj == nil || obj.Name == nil || *obj.Name != "Gadget" {
		t.Fatalf("want Gadget, got %+v", obj)
	}
}

// TestApiObjectFromGroup_NotFound covers the not-found branch: the group
// contains no object with the queried name.
func TestApiObjectFromGroup_NotFound(t *testing.T) {
	// setup: a group with one object, name distinct from the query
	group := &ApiObjectGroup{
		Objects: []*ApiObject{{Name: strPtr("Widget")}},
	}

	// action: look up an absent name
	obj, err := ApiObjectFromGroup("Missing", group)

	// assertion: nil object and a descriptive error
	if obj != nil {
		t.Fatalf("want nil obj, got %+v", obj)
	}
	if err == nil || !strings.Contains(err.Error(), "no objects with name Missing") {
		t.Fatalf("want no-objects error, got %v", err)
	}
}

// TestApiObjectFromGroup_Duplicate covers the failure mode where the group
// contains multiple objects with the same name. ApiObjectFromGroup treats
// this as a config error.
func TestApiObjectFromGroup_Duplicate(t *testing.T) {
	// setup: two objects sharing a name in the same group
	group := &ApiObjectGroup{
		Objects: []*ApiObject{
			{Name: strPtr("Widget")},
			{Name: strPtr("Widget")},
		},
	}

	// action: look up the duplicated name
	obj, err := ApiObjectFromGroup("Widget", group)

	// assertion: nil object and a duplicate error
	if obj != nil {
		t.Fatalf("want nil obj, got %+v", obj)
	}
	if err == nil || !strings.Contains(err.Error(), "multiple objects with name Widget") {
		t.Fatalf("want multiple-objects error, got %v", err)
	}
}

// TestValidateSdkConfig_NotModuleEmptyNamespaceOk covers running the SDK
// against the core threeport repo (not a module). The ApiNamespace
// requirement is waived, so an empty namespace passes.
func TestValidateSdkConfig_NotModuleEmptyNamespaceOk(t *testing.T) {
	// setup: chdir to a temp workspace whose go.mod claims to BE the core
	// threeport module, so util.IsModule() returns false
	chdirWithGoMod(t, "github.com/threeport/threeport")

	// action: validate a minimal config with no ApiNamespace
	err := ValidateSdkConfig(&SdkConfig{})

	// assertion: validation succeeds
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
}

// TestValidateSdkConfig_ModuleRequiresNamespace covers the module case: a
// module-scoped SDK config must set ApiNamespace.
func TestValidateSdkConfig_ModuleRequiresNamespace(t *testing.T) {
	// setup: chdir to a temp workspace whose go.mod is a foreign path so
	// util.IsModule() returns true
	chdirWithGoMod(t, "example.com/somemodule")

	// action: validate a config missing ApiNamespace
	err := ValidateSdkConfig(&SdkConfig{})

	// assertion: reports the required field
	if err == nil || !strings.Contains(err.Error(), "ApiNamespace is a required field") {
		t.Fatalf("want ApiNamespace-required error, got %v", err)
	}
}

// TestValidateSdkConfig_ModuleWithNamespaceOk covers the module happy path:
// ApiNamespace is present and no defined-instance mismatches exist.
func TestValidateSdkConfig_ModuleWithNamespaceOk(t *testing.T) {
	// setup: pretend to be a module and supply the required namespace
	chdirWithGoMod(t, "example.com/somemodule")
	cfg := &SdkConfig{ApiNamespace: "example.com/somemodule"}

	// action: validate
	err := ValidateSdkConfig(cfg)

	// assertion: validation succeeds
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
}

// TestValidateSdkConfig_DefinedInstanceMismatchOnDefinition covers the
// cross-check that a Definition and its Instance share DefinedInstance
// values. The Definition opts out; the Instance does not, so validation
// must reject the config.
func TestValidateSdkConfig_DefinedInstanceMismatchOnDefinition(t *testing.T) {
	// setup: core-repo mode so no ApiNamespace is required. The group has
	// a Definition marked opt-in (nil defaults to true) and an Instance
	// explicitly marked opt-out.
	chdirWithGoMod(t, "github.com/threeport/threeport")
	cfg := &SdkConfig{
		ApiObjectConfig: ApiObjectConfig{
			ApiObjectGroups: []*ApiObjectGroup{{
				Name: strPtr("things"),
				Objects: []*ApiObject{
					{Name: strPtr("WidgetDefinition")},
					{Name: strPtr("WidgetInstance"), DefinedInstance: boolPtr(false)},
				},
			}},
		},
	}

	// action: validate
	err := ValidateSdkConfig(cfg)

	// assertion: reports the mismatched DefinedInstance values
	if err == nil || !strings.Contains(err.Error(), "DefinedInstance") {
		t.Fatalf("want DefinedInstance mismatch error, got %v", err)
	}
}

// TestValidateSdkConfig_DefinedInstanceMismatchOnInstance covers the
// mirror: the Instance defaults to opt-in and the Definition opts out.
// Because IsOfDefinedInstance's opt-out check short-circuits on the
// Definition side, only the Instance-side iteration flags the mismatch.
func TestValidateSdkConfig_DefinedInstanceMismatchOnInstance(t *testing.T) {
	// setup: Definition explicitly opts out; Instance is unset (defaults on)
	chdirWithGoMod(t, "github.com/threeport/threeport")
	cfg := &SdkConfig{
		ApiObjectConfig: ApiObjectConfig{
			ApiObjectGroups: []*ApiObjectGroup{{
				Name: strPtr("things"),
				Objects: []*ApiObject{
					{Name: strPtr("WidgetDefinition"), DefinedInstance: boolPtr(false)},
					{Name: strPtr("WidgetInstance")},
				},
			}},
		},
	}

	// action: validate
	err := ValidateSdkConfig(cfg)

	// assertion: reports the mismatched DefinedInstance values
	if err == nil || !strings.Contains(err.Error(), "DefinedInstance") {
		t.Fatalf("want DefinedInstance mismatch error, got %v", err)
	}
}

// TestValidateSdkConfig_BothOptOutOk covers the case where both sides of a
// Definition/Instance pair explicitly opt out. IsOfDefinedInstance returns
// false and the mismatch check is skipped entirely.
func TestValidateSdkConfig_BothOptOutOk(t *testing.T) {
	// setup: both sides explicitly opted out
	chdirWithGoMod(t, "github.com/threeport/threeport")
	cfg := &SdkConfig{
		ApiObjectConfig: ApiObjectConfig{
			ApiObjectGroups: []*ApiObjectGroup{{
				Name: strPtr("things"),
				Objects: []*ApiObject{
					{Name: strPtr("WidgetDefinition"), DefinedInstance: boolPtr(false)},
					{Name: strPtr("WidgetInstance"), DefinedInstance: boolPtr(false)},
				},
			}},
		},
	}

	// action: validate
	err := ValidateSdkConfig(cfg)

	// assertion: validation succeeds
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
}

// TestGetSdkConfig_FileMissing covers the read-error path: an absent path
// bubbles up as a "failed to read config file" error.
func TestGetSdkConfig_FileMissing(t *testing.T) {
	// setup: point at a path that does not exist
	dir := t.TempDir()

	// action: attempt to load a non-existent config
	cfg, err := GetSdkConfig(filepath.Join(dir, "nope.yaml"))

	// assertion: nil config and a read-file error
	if cfg != nil {
		t.Fatalf("want nil config, got %+v", cfg)
	}
	if err == nil || !strings.Contains(err.Error(), "failed to read config file") {
		t.Fatalf("want read-file error, got %v", err)
	}
}

// TestGetSdkConfig_InvalidYaml covers the unmarshal-error path: a YAML file
// whose shape doesn't match SdkConfig triggers strict-unmarshal failure.
func TestGetSdkConfig_InvalidYaml(t *testing.T) {
	// setup: a file with a field the strict unmarshaler will reject
	dir := t.TempDir()
	path := filepath.Join(dir, "sdk-config.yaml")
	if err := os.WriteFile(path, []byte("NotARealField: foo\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// action: attempt to load
	cfg, err := GetSdkConfig(path)

	// assertion: nil config and an unmarshal error
	if cfg != nil {
		t.Fatalf("want nil config, got %+v", cfg)
	}
	if err == nil || !strings.Contains(err.Error(), "failed to unmarshal config file yaml content") {
		t.Fatalf("want unmarshal error, got %v", err)
	}
}

// TestGetSdkConfig_ValidationFailure covers the surface where the file
// parses cleanly but ValidateSdkConfig rejects it (module mode with no
// ApiNamespace).
func TestGetSdkConfig_ValidationFailure(t *testing.T) {
	// setup: chdir to a module-shaped workspace and drop a config alongside
	// it that omits ApiNamespace
	dir := chdirWithGoMod(t, "example.com/somemodule")
	path := filepath.Join(dir, "sdk-config.yaml")
	if err := os.WriteFile(path, []byte("moduleName: foo\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// action: load the config
	cfg, err := GetSdkConfig(path)

	// assertion: validation failure surfaces with wrapping
	if cfg != nil {
		t.Fatalf("want nil config, got %+v", cfg)
	}
	if err == nil || !strings.Contains(err.Error(), "SDK config validation failed") {
		t.Fatalf("want validation-failed error, got %v", err)
	}
}

// TestGetSdkConfig_HappyPath covers a fully valid file: content unmarshals,
// validation passes (core-repo mode), and the returned struct matches.
func TestGetSdkConfig_HappyPath(t *testing.T) {
	// setup: core-repo mode plus a minimal well-formed config
	dir := chdirWithGoMod(t, "github.com/threeport/threeport")
	path := filepath.Join(dir, "sdk-config.yaml")
	yamlBody := "moduleName: mymod\nimageNamespace: docker.io/example\n"
	if err := os.WriteFile(path, []byte(yamlBody), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// action: load the config
	cfg, err := GetSdkConfig(path)

	// assertion: no error and the parsed fields match the source
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("want non-nil config")
	}
	if cfg.ModuleName != "mymod" {
		t.Errorf("ModuleName: want mymod, got %q", cfg.ModuleName)
	}
	if cfg.ImageNamespace != "docker.io/example" {
		t.Errorf("ImageNamespace: want docker.io/example, got %q", cfg.ImageNamespace)
	}
}
