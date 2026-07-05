/*
Copyright © 2023 Threeport admin@threeport.io
*/
package cmd

import (
	"testing"

	"github.com/spf13/pflag"
)

// TestConfigGetControlPlanesCmdMetadata asserts the command carries the
// expected use, short summary, silence-usage flag, and pre-run hook.
func TestConfigGetControlPlanesCmdMetadata(t *testing.T) {
	// verify cobra fields wired at package init
	if got, want := ConfigGetControlPlanesCmd.Use, "get-control-planes"; got != want {
		t.Errorf("Use = %q, want %q", got, want)
	}
	if ConfigGetControlPlanesCmd.Short == "" {
		t.Error("Short is empty; expected a short description")
	}
	if ConfigGetControlPlanesCmd.Long == "" {
		t.Error("Long is empty; expected a long description")
	}
	if ConfigGetControlPlanesCmd.Example == "" {
		t.Error("Example is empty; expected an example invocation")
	}

	// verify silence-usage stays on so cobra doesn't print usage on error
	if !ConfigGetControlPlanesCmd.SilenceUsage {
		t.Error("SilenceUsage = false, want true")
	}

	// verify PreRun hook is registered so pre-run setup runs before Run
	if ConfigGetControlPlanesCmd.PreRun == nil {
		t.Error("PreRun is nil; expected CommandPreRunFunc")
	}

	// verify Run is registered
	if ConfigGetControlPlanesCmd.Run == nil {
		t.Error("Run is nil; expected a Run function")
	}
}

// TestConfigGetControlPlanesCmdNoFlags asserts get-control-planes registers
// no local flags; it's a pure list command.
func TestConfigGetControlPlanesCmdNoFlags(t *testing.T) {
	// count local flags registered on the command
	var count int
	ConfigGetControlPlanesCmd.LocalFlags().VisitAll(func(_ *pflag.Flag) {
		count++
	})
	if count != 0 {
		t.Errorf("expected zero local flags, got %d", count)
	}
}

// TestConfigGetControlPlanesCmdRegisteredOnParent asserts the command is
// wired as a subcommand of ConfigCmd via init().
func TestConfigGetControlPlanesCmdRegisteredOnParent(t *testing.T) {
	// walk the parent command's children looking for get-control-planes
	var found bool
	for _, sub := range ConfigCmd.Commands() {
		if sub == ConfigGetControlPlanesCmd {
			found = true
			break
		}
	}
	if !found {
		t.Error("ConfigGetControlPlanesCmd not registered under ConfigCmd")
	}
}
