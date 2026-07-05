/*
Copyright © 2023 Threeport admin@threeport.io
*/
package cmd

import (
	"testing"
)

// TestConfigAwsCloudAccountCmdMetadata asserts the command carries the
// expected use, short summary, silence-usage flag, and pre-run hook.
func TestConfigAwsCloudAccountCmdMetadata(t *testing.T) {
	// verify cobra fields wired at package init
	if got, want := ConfigAwsCloudAccountCmd.Use, "aws-provider"; got != want {
		t.Errorf("Use = %q, want %q", got, want)
	}
	if ConfigAwsCloudAccountCmd.Short == "" {
		t.Error("Short is empty; expected a short description")
	}
	if ConfigAwsCloudAccountCmd.Long == "" {
		t.Error("Long is empty; expected a long description")
	}
	if ConfigAwsCloudAccountCmd.Example == "" {
		t.Error("Example is empty; expected an example invocation")
	}

	// verify silence-usage stays on so cobra doesn't print usage on error
	if !ConfigAwsCloudAccountCmd.SilenceUsage {
		t.Error("SilenceUsage = false, want true")
	}

	// verify PreRun hook is registered so the pre-run command runs
	if ConfigAwsCloudAccountCmd.PreRun == nil {
		t.Error("PreRun is nil; expected CommandPreRunFunc")
	}

	// verify Run is registered
	if ConfigAwsCloudAccountCmd.Run == nil {
		t.Error("Run is nil; expected a Run function")
	}
}

// TestConfigAwsCloudAccountCmdFlags asserts every documented flag is
// registered with the expected default value.
func TestConfigAwsCloudAccountCmdFlags(t *testing.T) {
	// enumerate the flags the init() function should register
	cases := []struct {
		name       string
		defaultVal string
	}{
		{"aws-provider-name", ""},
		{"aws-region", ""},
		{"aws-profile", ""},
		{"runtime-manager-role-name", ""},
		{"external-runtime-manager-role-name", ""},
		{"aws-account-id", ""},
	}

	// verify each string flag is present with the expected default
	flags := ConfigAwsCloudAccountCmd.Flags()
	for _, tc := range cases {
		f := flags.Lookup(tc.name)
		if f == nil {
			t.Errorf("flag %q not registered", tc.name)
			continue
		}
		if f.DefValue != tc.defaultVal {
			t.Errorf("flag %q default = %q, want %q", tc.name, f.DefValue, tc.defaultVal)
		}
	}

	// verify the default-account boolean flag has the expected default
	f := flags.Lookup("default-account")
	if f == nil {
		t.Fatal("flag \"default-account\" not registered")
	}
	if f.DefValue != "false" {
		t.Errorf("flag \"default-account\" default = %q, want \"false\"", f.DefValue)
	}
}

// TestConfigAwsCloudAccountCmdRequiredFlags asserts the required flags
// carry the cobra_annotation_bash_completion_one_required_flag annotation.
func TestConfigAwsCloudAccountCmdRequiredFlags(t *testing.T) {
	// required flags carry the cobra "required" annotation
	required := []string{
		"aws-provider-name",
		"aws-region",
		"aws-profile",
	}

	// verify each required flag is annotated as required
	flags := ConfigAwsCloudAccountCmd.Flags()
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

// TestConfigAwsCloudAccountCmdRegisteredOnParent asserts the command is
// wired as a subcommand of ConfigCmd via init().
func TestConfigAwsCloudAccountCmdRegisteredOnParent(t *testing.T) {
	// walk the parent command's children looking for aws-provider
	var found bool
	for _, sub := range ConfigCmd.Commands() {
		if sub == ConfigAwsCloudAccountCmd {
			found = true
			break
		}
	}
	if !found {
		t.Error("ConfigAwsCloudAccountCmd not registered under ConfigCmd")
	}
}
