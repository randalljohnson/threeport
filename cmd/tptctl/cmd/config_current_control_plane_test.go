/*
Copyright © 2023 Threeport admin@threeport.io
*/
package cmd

import (
	"testing"
)

// TestConfigCurrentControlPlaneCmdMetadata asserts the command carries the
// expected use, short summary, silence-usage flag, and pre-run hook.
func TestConfigCurrentControlPlaneCmdMetadata(t *testing.T) {
	// verify cobra fields wired at package init
	if got, want := ConfigCurrentControlPlaneCmd.Use, "current-control-plane"; got != want {
		t.Errorf("Use = %q, want %q", got, want)
	}
	if ConfigCurrentControlPlaneCmd.Short == "" {
		t.Error("Short is empty; expected a short description")
	}
	if ConfigCurrentControlPlaneCmd.Long == "" {
		t.Error("Long is empty; expected a long description")
	}
	if ConfigCurrentControlPlaneCmd.Example == "" {
		t.Error("Example is empty; expected an example invocation")
	}

	// verify silence-usage stays on so cobra doesn't print usage on error
	if !ConfigCurrentControlPlaneCmd.SilenceUsage {
		t.Error("SilenceUsage = false, want true")
	}

	// verify PreRun hook is registered so pre-run setup runs before Run
	if ConfigCurrentControlPlaneCmd.PreRun == nil {
		t.Error("PreRun is nil; expected CommandPreRunFunc")
	}

	// verify Run is registered
	if ConfigCurrentControlPlaneCmd.Run == nil {
		t.Error("Run is nil; expected a Run function")
	}
}

// TestConfigCurrentControlPlaneCmdFlags asserts every documented flag is
// registered with the expected default value and shorthand.
func TestConfigCurrentControlPlaneCmdFlags(t *testing.T) {
	// enumerate the flags the init() function should register
	cases := []struct {
		name       string
		shorthand  string
		defaultVal string
	}{
		{"control-plane-name", "n", ""},
	}

	// verify each string flag is present with the expected default and shorthand
	flags := ConfigCurrentControlPlaneCmd.Flags()
	for _, tc := range cases {
		f := flags.Lookup(tc.name)
		if f == nil {
			t.Errorf("flag %q not registered", tc.name)
			continue
		}
		if f.DefValue != tc.defaultVal {
			t.Errorf("flag %q default = %q, want %q", tc.name, f.DefValue, tc.defaultVal)
		}
		if f.Shorthand != tc.shorthand {
			t.Errorf("flag %q shorthand = %q, want %q", tc.name, f.Shorthand, tc.shorthand)
		}
	}
}

// TestConfigCurrentControlPlaneCmdRequiredFlags asserts the required flags
// carry the cobra required-flag annotation so cobra rejects invocations
// missing the flag.
func TestConfigCurrentControlPlaneCmdRequiredFlags(t *testing.T) {
	// required flags carry the cobra "required" annotation
	required := []string{
		"control-plane-name",
	}

	// verify each required flag is annotated as required
	flags := ConfigCurrentControlPlaneCmd.Flags()
	for _, name := range required {
		f := flags.Lookup(name)
		if f == nil {
			t.Errorf("flag %q not registered", name)
			continue
		}
		if _, ok := f.Annotations["cobra_annotation_bash_completion_one_required_flag"]; !ok {
			t.Errorf("flag %q missing required annotation", name)
		}
	}
}

// TestConfigCurrentControlPlaneCmdRegisteredOnParent asserts the command is
// wired as a subcommand of ConfigCmd via init().
func TestConfigCurrentControlPlaneCmdRegisteredOnParent(t *testing.T) {
	// walk the parent command's children looking for current-control-plane
	var found bool
	for _, sub := range ConfigCmd.Commands() {
		if sub == ConfigCurrentControlPlaneCmd {
			found = true
			break
		}
	}
	if !found {
		t.Error("ConfigCurrentControlPlaneCmd not registered under ConfigCmd")
	}
}
