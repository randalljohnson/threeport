package cmd

import (
	"strings"
	"testing"

	config_v0 "github.com/threeport/threeport/pkg/config/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestOutputGetv0ModuleApisCmd_RendersHeaderAndRows covers the happy path
// where each module api in the slice contributes a NAME + CORE MODULE + AGE
// row under the tabwriter header.
func TestOutputGetv0ModuleApisCmd_RendersHeaderAndRows(t *testing.T) {
	// arrange two module apis with distinct core flags and ages
	moduleApis := []config_v0.ModuleApiConfig{
		{ModuleApi: config_v0.ModuleApiValues{
			Name: util.Ptr("core-api"),
			Core: util.Ptr(true),
			Age:  util.Ptr("2d"),
		}},
		{ModuleApi: config_v0.ModuleApiValues{
			Name: util.Ptr("extension-api"),
			Core: util.Ptr(false),
			Age:  util.Ptr("7d"),
		}},
	}

	// act: invoke with stdout captured
	out, err := captureStdout(t, func() error {
		return outputGetv0ModuleApisCmd(&moduleApis)
	})

	// assert: nil error, three-column header, per-row content, and order
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	for _, want := range []string{"NAME", "CORE MODULE", "AGE", "core-api", "true", "2d", "extension-api", "false", "7d"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got %q", want, out)
		}
	}
	// input order is preserved: first entry precedes second
	if strings.Index(out, "core-api") > strings.Index(out, "extension-api") {
		t.Errorf("expected core-api row before extension-api row, got %q", out)
	}
}

// TestOutputGetv0ModuleApisCmd_EmptySlice covers the boundary where the
// caller hands in an empty slice: only the header row should print.
func TestOutputGetv0ModuleApisCmd_EmptySlice(t *testing.T) {
	// arrange: an empty slice so the range body never executes
	moduleApis := []config_v0.ModuleApiConfig{}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0ModuleApisCmd(&moduleApis)
	})

	// assert: header renders, no per-row output
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(out, "NAME") || !strings.Contains(out, "CORE MODULE") || !strings.Contains(out, "AGE") {
		t.Errorf("expected header row, got %q", out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("expected exactly one line for empty input, got %d: %q", len(lines), out)
	}
}

// TestOutputGetv0ModuleApisCmd_NilAge covers the subtle branch where a
// module api has no age set: the age column is rendered as an empty string
// rather than dereferencing a nil pointer.
func TestOutputGetv0ModuleApisCmd_NilAge(t *testing.T) {
	// arrange one module api with Age nil to trigger the nil-guard branch
	moduleApis := []config_v0.ModuleApiConfig{
		{ModuleApi: config_v0.ModuleApiValues{
			Name: util.Ptr("no-age-api"),
			Core: util.Ptr(false),
			Age:  nil,
		}},
	}

	// act: must not panic even with nil Age
	out, err := captureStdout(t, func() error {
		return outputGetv0ModuleApisCmd(&moduleApis)
	})

	// assert: name and core still render; no panic occurred
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(out, "no-age-api") {
		t.Errorf("expected output to contain module api name, got %q", out)
	}
	if !strings.Contains(out, "false") {
		t.Errorf("expected output to contain core flag, got %q", out)
	}
}

// TestOutputGetv0ModuleApiRoutesCmd_RendersHeaderAndRows covers the happy
// path for module-api-route output: PATH + MODULE API + AGE columns plus a
// row per route.
func TestOutputGetv0ModuleApiRoutesCmd_RendersHeaderAndRows(t *testing.T) {
	// arrange two routes each linked to a named module api
	routes := []config_v0.ModuleApiRouteConfig{
		{ModuleApiRoute: config_v0.ModuleApiRouteValues{
			Path:      util.Ptr("/v0/widgets"),
			ModuleApi: &config_v0.ModuleApiValues{Name: util.Ptr("widget-api")},
			Age:       util.Ptr("1d"),
		}},
		{ModuleApiRoute: config_v0.ModuleApiRouteValues{
			Path:      util.Ptr("/v0/gadgets"),
			ModuleApi: &config_v0.ModuleApiValues{Name: util.Ptr("gadget-api")},
			Age:       util.Ptr("3d"),
		}},
	}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0ModuleApiRoutesCmd(&routes)
	})

	// assert: header + row-level content, input order preserved
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	for _, want := range []string{"PATH", "MODULE API", "AGE", "/v0/widgets", "widget-api", "1d", "/v0/gadgets", "gadget-api", "3d"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got %q", want, out)
		}
	}
	if strings.Index(out, "/v0/widgets") > strings.Index(out, "/v0/gadgets") {
		t.Errorf("expected widgets row before gadgets row, got %q", out)
	}
}

// TestOutputGetv0ModuleApiRoutesCmd_NilModuleApiAndAge covers the two
// nil-guard branches on a route: ModuleApi pointer nil and Age pointer nil.
// Both columns must render as empty strings without panicking.
func TestOutputGetv0ModuleApiRoutesCmd_NilModuleApiAndAge(t *testing.T) {
	// arrange three routes exercising the nil-guard combinations
	routes := []config_v0.ModuleApiRouteConfig{
		// ModuleApi pointer nil: outer guard trips
		{ModuleApiRoute: config_v0.ModuleApiRouteValues{
			Path:      util.Ptr("/no-api"),
			ModuleApi: nil,
			Age:       util.Ptr("1d"),
		}},
		// ModuleApi set but Name nil: inner guard trips
		{ModuleApiRoute: config_v0.ModuleApiRouteValues{
			Path:      util.Ptr("/no-api-name"),
			ModuleApi: &config_v0.ModuleApiValues{Name: nil},
			Age:       util.Ptr("2d"),
		}},
		// Age nil branch
		{ModuleApiRoute: config_v0.ModuleApiRouteValues{
			Path:      util.Ptr("/no-age"),
			ModuleApi: &config_v0.ModuleApiValues{Name: util.Ptr("some-api")},
			Age:       nil,
		}},
	}

	// act: must not panic on any of the nil-guard branches
	out, err := captureStdout(t, func() error {
		return outputGetv0ModuleApiRoutesCmd(&routes)
	})

	// assert: paths render for every row; some-api still visible for row three
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	for _, want := range []string{"/no-api", "/no-api-name", "/no-age", "some-api"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got %q", want, out)
		}
	}
}

// TestOutputGetv0ModuleApiRoutesCmd_EmptySlice covers the boundary where
// the caller passes an empty slice: only the header row prints.
func TestOutputGetv0ModuleApiRoutesCmd_EmptySlice(t *testing.T) {
	// arrange: empty slice
	routes := []config_v0.ModuleApiRouteConfig{}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0ModuleApiRoutesCmd(&routes)
	})

	// assert: header only, no data rows
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(out, "PATH") {
		t.Errorf("expected header row with PATH, got %q", out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("expected exactly one line for empty input, got %d: %q", len(lines), out)
	}
}

// TestOutputGetv0ModuleControllersCmd_RendersHeaderAndRows covers the happy
// path: NAME + MODULE API + AGE header, and a row per controller with its
// linked api name.
func TestOutputGetv0ModuleControllersCmd_RendersHeaderAndRows(t *testing.T) {
	// arrange two controllers linked to distinct module apis
	controllers := []config_v0.ModuleControllerConfig{
		{ModuleController: config_v0.ModuleControllerValues{
			Name:      util.Ptr("widget-controller"),
			ModuleApi: &config_v0.ModuleApiValues{Name: util.Ptr("widget-api")},
			Age:       util.Ptr("4d"),
		}},
		{ModuleController: config_v0.ModuleControllerValues{
			Name:      util.Ptr("gadget-controller"),
			ModuleApi: &config_v0.ModuleApiValues{Name: util.Ptr("gadget-api")},
			Age:       util.Ptr("9d"),
		}},
	}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0ModuleControllersCmd(&controllers)
	})

	// assert: header + per-row values + preserved input order
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	for _, want := range []string{"NAME", "MODULE API", "AGE", "widget-controller", "widget-api", "4d", "gadget-controller", "gadget-api", "9d"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got %q", want, out)
		}
	}
	if strings.Index(out, "widget-controller") > strings.Index(out, "gadget-controller") {
		t.Errorf("expected widget-controller row before gadget-controller row, got %q", out)
	}
}

// TestOutputGetv0ModuleControllersCmd_NilLinkedApiAndAge covers the two
// nil-guard branches on a controller row: linked ModuleApi absent and Age
// absent. Both columns render as empty strings without panicking.
func TestOutputGetv0ModuleControllersCmd_NilLinkedApiAndAge(t *testing.T) {
	// arrange three controllers exercising the nil-guard combinations
	controllers := []config_v0.ModuleControllerConfig{
		// ModuleApi pointer nil: outer guard trips
		{ModuleController: config_v0.ModuleControllerValues{
			Name:      util.Ptr("no-api-controller"),
			ModuleApi: nil,
			Age:       util.Ptr("1d"),
		}},
		// ModuleApi set but Name nil: inner guard trips
		{ModuleController: config_v0.ModuleControllerValues{
			Name:      util.Ptr("no-api-name-controller"),
			ModuleApi: &config_v0.ModuleApiValues{Name: nil},
			Age:       util.Ptr("2d"),
		}},
		// Age nil branch
		{ModuleController: config_v0.ModuleControllerValues{
			Name:      util.Ptr("no-age-controller"),
			ModuleApi: &config_v0.ModuleApiValues{Name: util.Ptr("some-api")},
			Age:       nil,
		}},
	}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0ModuleControllersCmd(&controllers)
	})

	// assert: every controller name renders, some-api still visible
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	for _, want := range []string{"no-api-controller", "no-api-name-controller", "no-age-controller", "some-api"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got %q", want, out)
		}
	}
}

// TestOutputGetv0ModuleControllersCmd_EmptySlice covers the boundary where
// the caller passes an empty slice: only the header row prints.
func TestOutputGetv0ModuleControllersCmd_EmptySlice(t *testing.T) {
	// arrange
	controllers := []config_v0.ModuleControllerConfig{}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0ModuleControllersCmd(&controllers)
	})

	// assert
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(out, "NAME") || !strings.Contains(out, "MODULE API") {
		t.Errorf("expected header row, got %q", out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("expected exactly one line for empty input, got %d: %q", len(lines), out)
	}
}

// TestOutputGetv0ModuleObjectsCmd_RendersHeaderAndRows covers the happy
// path for module-object output: the six-column header and a fully-populated
// row per object.
func TestOutputGetv0ModuleObjectsCmd_RendersHeaderAndRows(t *testing.T) {
	// arrange two module objects with all optional fields populated
	objects := []config_v0.ModuleObjectConfig{
		{ModuleObject: config_v0.ModuleObjectValues{
			Name:             util.Ptr("Widget"),
			Version:          util.Ptr("v0"),
			Description:      util.Ptr("a widget"),
			ModuleController: &config_v0.ModuleControllerValues{Name: util.Ptr("widget-controller")},
			ModuleApi:        &config_v0.ModuleApiValues{Name: util.Ptr("widget-api")},
			Age:              util.Ptr("6h"),
		}},
		{ModuleObject: config_v0.ModuleObjectValues{
			Name:             util.Ptr("Gadget"),
			Version:          util.Ptr("v1"),
			Description:      util.Ptr("a gadget"),
			ModuleController: &config_v0.ModuleControllerValues{Name: util.Ptr("gadget-controller")},
			ModuleApi:        &config_v0.ModuleApiValues{Name: util.Ptr("gadget-api")},
			Age:              util.Ptr("12h"),
		}},
	}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0ModuleObjectsCmd(&objects)
	})

	// assert: six-column header, per-row values, and preserved input order
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	for _, want := range []string{
		"NAME", "VERSION", "DESCRIPTION", "MODULE CONTROLLER", "MODULE API", "AGE",
		"Widget", "v0", "a widget", "widget-controller", "widget-api", "6h",
		"Gadget", "v1", "a gadget", "gadget-controller", "gadget-api", "12h",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got %q", want, out)
		}
	}
	if strings.Index(out, "Widget") > strings.Index(out, "Gadget") {
		t.Errorf("expected Widget row before Gadget row, got %q", out)
	}
}

// TestOutputGetv0ModuleObjectsCmd_NilOptionalFields covers every nil-guard
// branch on a module-object row: Description, linked ModuleController (both
// outer pointer and inner Name), linked ModuleApi (both outer pointer and
// inner Name), and Age.
func TestOutputGetv0ModuleObjectsCmd_NilOptionalFields(t *testing.T) {
	// arrange four objects, each dropping a different optional pointer
	objects := []config_v0.ModuleObjectConfig{
		// Description nil branch
		{ModuleObject: config_v0.ModuleObjectValues{
			Name:             util.Ptr("obj-no-desc"),
			Version:          util.Ptr("v0"),
			Description:      nil,
			ModuleController: &config_v0.ModuleControllerValues{Name: util.Ptr("ctrl")},
			ModuleApi:        &config_v0.ModuleApiValues{Name: util.Ptr("api")},
			Age:              util.Ptr("1h"),
		}},
		// ModuleController pointer nil and ModuleApi pointer nil: outer guards trip
		{ModuleObject: config_v0.ModuleObjectValues{
			Name:             util.Ptr("obj-no-links"),
			Version:          util.Ptr("v0"),
			Description:      util.Ptr("desc"),
			ModuleController: nil,
			ModuleApi:        nil,
			Age:              util.Ptr("2h"),
		}},
		// ModuleController and ModuleApi set but their Name nil: inner guards trip
		{ModuleObject: config_v0.ModuleObjectValues{
			Name:             util.Ptr("obj-no-link-names"),
			Version:          util.Ptr("v0"),
			Description:      util.Ptr("desc"),
			ModuleController: &config_v0.ModuleControllerValues{Name: nil},
			ModuleApi:        &config_v0.ModuleApiValues{Name: nil},
			Age:              util.Ptr("3h"),
		}},
		// Age nil branch
		{ModuleObject: config_v0.ModuleObjectValues{
			Name:             util.Ptr("obj-no-age"),
			Version:          util.Ptr("v0"),
			Description:      util.Ptr("desc"),
			ModuleController: &config_v0.ModuleControllerValues{Name: util.Ptr("ctrl")},
			ModuleApi:        &config_v0.ModuleApiValues{Name: util.Ptr("api")},
			Age:              nil,
		}},
	}

	// act: must not panic on any nil-guard branch
	out, err := captureStdout(t, func() error {
		return outputGetv0ModuleObjectsCmd(&objects)
	})

	// assert: every object name renders (no row was dropped)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	for _, want := range []string{"obj-no-desc", "obj-no-links", "obj-no-link-names", "obj-no-age"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got %q", want, out)
		}
	}
}

// TestOutputGetv0ModuleObjectsCmd_EmptySlice covers the boundary where the
// caller passes an empty slice: only the header row prints.
func TestOutputGetv0ModuleObjectsCmd_EmptySlice(t *testing.T) {
	// arrange
	objects := []config_v0.ModuleObjectConfig{}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0ModuleObjectsCmd(&objects)
	})

	// assert
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(out, "DESCRIPTION") || !strings.Contains(out, "MODULE CONTROLLER") {
		t.Errorf("expected header row, got %q", out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("expected exactly one line for empty input, got %d: %q", len(lines), out)
	}
}
