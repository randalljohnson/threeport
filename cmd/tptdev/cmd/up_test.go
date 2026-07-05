package cmd

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/threeport/threeport/pkg/threeport-installer/v0/tptdev"
)

// findSubcommand walks parent.Commands() for a child whose Use field starts
// with the given name, so tests can find upCmd after init() attaches it to
// rootCmd without exporting the variable.
func findSubcommand(parent *cobra.Command, name string) *cobra.Command {
	for _, c := range parent.Commands() {
		if c.Name() == name {
			return c
		}
	}
	return nil
}

// TestUpCmd_RegisteredOnRoot asserts upCmd is attached to rootCmd during init()
// so the CLI actually surfaces "tptdev up".
func TestUpCmd_RegisteredOnRoot(t *testing.T) {
	// action under test: init() attached upCmd to rootCmd
	up := findSubcommand(rootCmd, "up")

	// assert the command exists so the CLI exposes the "up" verb
	if up == nil {
		t.Fatalf("expected rootCmd to have an 'up' subcommand, got nil")
	}

	// assert the Short description is set so cobra help output is non-empty
	if up.Short == "" {
		t.Fatalf("expected upCmd.Short to be non-empty")
	}
}

// TestUpCmd_FlagsRegistered asserts the flags init() registers exist with the
// expected defaults, so a rename or removed flag surfaces here rather than at
// runtime.
func TestUpCmd_FlagsRegistered(t *testing.T) {
	// resolve upCmd via its registration on rootCmd
	up := findSubcommand(rootCmd, "up")
	if up == nil {
		t.Fatalf("expected 'up' subcommand on rootCmd")
	}

	// each entry pairs a flag name with the default init() should set on it
	tests := []struct {
		flag        string
		wantDefault string
	}{
		// kubeconfig defaults to empty so cli.InitConfig resolves it later
		{flag: "kubeconfig", wantDefault: ""},
		// force-overwrite-config defaults to false to protect existing configs
		{flag: "force-overwrite-config", wantDefault: "false"},
		// auth-enabled defaults to false per the dev environment convention
		{flag: "auth-enabled", wantDefault: "false"},
		// name defaults to tptdev.DefaultInstanceName so the dev flow works with no args
		{flag: "name", wantDefault: tptdev.DefaultInstanceName},
		// threeport-path defaults to empty and is resolved by the installer
		{flag: "threeport-path", wantDefault: ""},
		// num-worker-nodes defaults to 0 for the smallest kind cluster
		{flag: "num-worker-nodes", wantDefault: "0"},
		// control-plane-image-namespace defaults to empty; the installer picks the default
		{flag: "control-plane-image-namespace", wantDefault: ""},
		// control-plane-image-tag defaults to empty; the installer picks the default
		{flag: "control-plane-image-tag", wantDefault: ""},
		// control-plane-only defaults to false; full install by default
		{flag: "control-plane-only", wantDefault: "false"},
		// infra-only defaults to false; full install by default
		{flag: "infra-only", wantDefault: "false"},
		// debug defaults to false so dev deployments use release images
		{flag: "debug", wantDefault: "false"},
		// verbose defaults to false to keep control plane logs quiet
		{flag: "verbose", wantDefault: "false"},
		// teardown-on-failure defaults to false so operators can inspect a failed run
		{flag: "teardown-on-failure", wantDefault: "false"},
		// local-registry defaults to false; only kind users opt in
		{flag: "local-registry", wantDefault: "false"},
	}

	for _, tt := range tests {
		t.Run(tt.flag, func(t *testing.T) {
			// action under test: look up the flag registered by init()
			f := up.Flags().Lookup(tt.flag)

			// assert the flag was registered so the CLI exposes it
			if f == nil {
				t.Fatalf("expected --%s flag on upCmd", tt.flag)
			}

			// assert the default matches so a silent regression on defaults trips this test
			if f.DefValue != tt.wantDefault {
				t.Errorf("--%s default = %q, want %q", tt.flag, f.DefValue, tt.wantDefault)
			}
		})
	}
}

// TestUpCmd_PersistentFlagsOnRoot asserts the persistent flags init() attaches
// to rootCmd survive on rootCmd itself, so every subcommand inherits them.
func TestUpCmd_PersistentFlagsOnRoot(t *testing.T) {
	// each entry pairs a persistent-flag name with its default
	tests := []struct {
		flag        string
		wantDefault string
	}{
		// threeport-config is persistent so every subcommand honors the same config path
		{flag: "threeport-config", wantDefault: ""},
		// provider-config is persistent for the same reason
		{flag: "provider-config", wantDefault: ""},
	}

	for _, tt := range tests {
		t.Run(tt.flag, func(t *testing.T) {
			// action under test: look up the persistent flag on rootCmd
			f := rootCmd.PersistentFlags().Lookup(tt.flag)

			// assert the persistent flag exists so subcommands inherit it
			if f == nil {
				t.Fatalf("expected persistent --%s flag on rootCmd", tt.flag)
			}

			// assert the default matches so a silent regression on defaults trips this test
			if f.DefValue != tt.wantDefault {
				t.Errorf("--%s default = %q, want %q", tt.flag, f.DefValue, tt.wantDefault)
			}
		})
	}
}

// TestUpCmd_ShorthandFlags asserts the single-letter shorthands init() wires
// up, so a rename that drops a shorthand breaks here rather than in a user's
// muscle memory.
func TestUpCmd_ShorthandFlags(t *testing.T) {
	// resolve upCmd via its registration on rootCmd
	up := findSubcommand(rootCmd, "up")
	if up == nil {
		t.Fatalf("expected 'up' subcommand on rootCmd")
	}

	// each entry pairs a flag name with its expected shorthand letter
	tests := []struct {
		flag      string
		shorthand string
	}{
		// kubeconfig's -k is the common shell convention
		{flag: "kubeconfig", shorthand: "k"},
		// name's -n is the common shell convention
		{flag: "name", shorthand: "n"},
		// threeport-path uses -p
		{flag: "threeport-path", shorthand: "p"},
		// control-plane-image-namespace uses -r
		{flag: "control-plane-image-namespace", shorthand: "r"},
		// control-plane-image-tag uses -t
		{flag: "control-plane-image-tag", shorthand: "t"},
	}

	for _, tt := range tests {
		t.Run(tt.flag, func(t *testing.T) {
			// action under test: look up the flag registered by init()
			f := up.Flags().Lookup(tt.flag)
			if f == nil {
				t.Fatalf("expected --%s flag on upCmd", tt.flag)
			}

			// assert the shorthand matches so a rename that drops -k, -n etc. trips this
			if f.Shorthand != tt.shorthand {
				t.Errorf("--%s shorthand = %q, want %q", tt.flag, f.Shorthand, tt.shorthand)
			}
		})
	}
}
