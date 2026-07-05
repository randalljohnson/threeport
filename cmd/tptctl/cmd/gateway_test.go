package cmd

import (
	"testing"

	cobra "github.com/spf13/cobra"
)

// TestGatewayCommandsCoverExpectedMetadata asserts that every exported cobra
// command variable in gateway.go carries the metadata fields (Use, Short,
// Long, Example, PreRun, Run, SilenceUsage) callers rely on for tptctl help
// output and dispatch.
func TestGatewayCommandsCoverExpectedMetadata(t *testing.T) {
	// each entry pairs a command with the expected Use token and Short description;
	// the fixture list is the surface gateway.go publishes.
	tests := []struct {
		name  string
		cmd   *cobra.Command
		use   string
		short string
	}{
		{"GetDomainNamesCmd", GetDomainNamesCmd, "domain-names", "Get domain names from the system"},
		{"CreateDomainNameCmd", CreateDomainNameCmd, "domain-name", "Create a new domain name"},
		{"DeleteDomainNameCmd", DeleteDomainNameCmd, "domain-name", "Delete an existing domain name"},
		{"GetDomainNameDefinitionsCmd", GetDomainNameDefinitionsCmd, "domain-name-definitions", "Get domain name definitions from the system"},
		{"CreateDomainNameDefinitionCmd", CreateDomainNameDefinitionCmd, "domain-name-definition", "Create a new domain name definition"},
		{"ReplaceDomainNameDefinitionCmd", ReplaceDomainNameDefinitionCmd, "domain-name-definition", "Replace an existing domain name definition"},
		{"DeleteDomainNameDefinitionCmd", DeleteDomainNameDefinitionCmd, "domain-name-definition", "Delete an existing domain name definition"},
		{"GetDomainNameInstancesCmd", GetDomainNameInstancesCmd, "domain-name-instances", "Get domain name instances from the system"},
		{"CreateDomainNameInstanceCmd", CreateDomainNameInstanceCmd, "domain-name-instance", "Create a new domain name instance"},
		{"ReplaceDomainNameInstanceCmd", ReplaceDomainNameInstanceCmd, "domain-name-instance", "Replace an existing domain name instance"},
		{"DeleteDomainNameInstanceCmd", DeleteDomainNameInstanceCmd, "domain-name-instance", "Delete an existing domain name instance"},
		{"GetGatewaysCmd", GetGatewaysCmd, "gateways", "Get gateways from the system"},
		{"CreateGatewayCmd", CreateGatewayCmd, "gateway", "Create a new gateway"},
		{"DeleteGatewayCmd", DeleteGatewayCmd, "gateway", "Delete an existing gateway"},
		{"GetGatewayDefinitionsCmd", GetGatewayDefinitionsCmd, "gateway-definitions", "Get gateway definitions from the system"},
		{"CreateGatewayDefinitionCmd", CreateGatewayDefinitionCmd, "gateway-definition", "Create a new gateway definition"},
		{"ReplaceGatewayDefinitionCmd", ReplaceGatewayDefinitionCmd, "gateway-definition", "Replace an existing gateway definition"},
		{"DeleteGatewayDefinitionCmd", DeleteGatewayDefinitionCmd, "gateway-definition", "Delete an existing gateway definition"},
		{"GetGatewayInstancesCmd", GetGatewayInstancesCmd, "gateway-instances", "Get gateway instances from the system"},
		{"CreateGatewayInstanceCmd", CreateGatewayInstanceCmd, "gateway-instance", "Create a new gateway instance"},
		{"ReplaceGatewayInstanceCmd", ReplaceGatewayInstanceCmd, "gateway-instance", "Replace an existing gateway instance"},
		{"DeleteGatewayInstanceCmd", DeleteGatewayInstanceCmd, "gateway-instance", "Delete an existing gateway instance"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// command variable exists.
			if tt.cmd == nil {
				t.Fatalf("command %s is nil", tt.name)
			}
			// Use verb matches expected token.
			if tt.cmd.Use != tt.use {
				t.Errorf("Use = %q, want %q", tt.cmd.Use, tt.use)
			}
			// Short description matches.
			if tt.cmd.Short != tt.short {
				t.Errorf("Short = %q, want %q", tt.cmd.Short, tt.short)
			}
			// Long is populated so `--help` produces the full description.
			if tt.cmd.Long == "" {
				t.Errorf("Long is empty")
			}
			// Example text is populated so users see a runnable sample.
			if tt.cmd.Example == "" {
				t.Errorf("Example is empty")
			}
			// SilenceUsage is true so failures do not dump usage on top of the error.
			if !tt.cmd.SilenceUsage {
				t.Errorf("SilenceUsage = false, want true")
			}
			// PreRun and Run must be wired so cobra can dispatch.
			if tt.cmd.PreRun == nil {
				t.Errorf("PreRun is nil")
			}
			if tt.cmd.Run == nil {
				t.Errorf("Run is nil")
			}
		})
	}
}

// TestGatewayGetCommandsExposeSingularAlias asserts every get-style command
// in gateway.go accepts the singular alias so users can type `gateway` in
// place of `gateways`.
func TestGatewayGetCommandsExposeSingularAlias(t *testing.T) {
	// each entry maps a plural-form get command to its expected singular alias.
	tests := []struct {
		name  string
		cmd   *cobra.Command
		alias string
	}{
		{"GetDomainNamesCmd", GetDomainNamesCmd, "domain-name"},
		{"GetDomainNameDefinitionsCmd", GetDomainNameDefinitionsCmd, "domain-name-definition"},
		{"GetDomainNameInstancesCmd", GetDomainNameInstancesCmd, "domain-name-instance"},
		{"GetGatewaysCmd", GetGatewaysCmd, "gateway"},
		{"GetGatewayDefinitionsCmd", GetGatewayDefinitionsCmd, "gateway-definition"},
		{"GetGatewayInstancesCmd", GetGatewayInstancesCmd, "gateway-instance"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// walk the Aliases slice to confirm the singular alias is present.
			found := false
			for _, a := range tt.cmd.Aliases {
				if a == tt.alias {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Aliases = %v, want to contain %q", tt.cmd.Aliases, tt.alias)
			}
		})
	}
}

// TestGatewayCommandsRegisterExpectedFlags asserts each gateway command
// declares the flag surface consumers rely on: --config, --version, and
// --control-plane-name on every command, plus --name / --output / --stdin
// where they appear in gateway.go's init blocks.
func TestGatewayCommandsRegisterExpectedFlags(t *testing.T) {
	// each entry names a command and the flags that must be present on it.
	tests := []struct {
		name  string
		cmd   *cobra.Command
		flags []string
	}{
		{"GetDomainNamesCmd", GetDomainNamesCmd, []string{"name", "config", "version", "output", "control-plane-name"}},
		{"CreateDomainNameCmd", CreateDomainNameCmd, []string{"config", "stdin", "control-plane-name", "version"}},
		{"DeleteDomainNameCmd", DeleteDomainNameCmd, []string{"config", "control-plane-name", "version"}},
		{"GetDomainNameDefinitionsCmd", GetDomainNameDefinitionsCmd, []string{"name", "config", "version", "output", "control-plane-name"}},
		{"CreateDomainNameDefinitionCmd", CreateDomainNameDefinitionCmd, []string{"config", "stdin", "control-plane-name", "version"}},
		{"ReplaceDomainNameDefinitionCmd", ReplaceDomainNameDefinitionCmd, []string{"config", "stdin", "name", "control-plane-name", "version"}},
		{"DeleteDomainNameDefinitionCmd", DeleteDomainNameDefinitionCmd, []string{"config", "name", "control-plane-name", "version"}},
		{"GetDomainNameInstancesCmd", GetDomainNameInstancesCmd, []string{"name", "config", "version", "output", "control-plane-name"}},
		{"CreateDomainNameInstanceCmd", CreateDomainNameInstanceCmd, []string{"config", "stdin", "control-plane-name", "version"}},
		{"ReplaceDomainNameInstanceCmd", ReplaceDomainNameInstanceCmd, []string{"config", "stdin", "name", "control-plane-name", "version"}},
		{"DeleteDomainNameInstanceCmd", DeleteDomainNameInstanceCmd, []string{"config", "name", "control-plane-name", "version"}},
		{"GetGatewaysCmd", GetGatewaysCmd, []string{"name", "config", "version", "output", "control-plane-name"}},
		{"CreateGatewayCmd", CreateGatewayCmd, []string{"config", "stdin", "control-plane-name", "version"}},
		{"DeleteGatewayCmd", DeleteGatewayCmd, []string{"config", "control-plane-name", "version"}},
		{"GetGatewayDefinitionsCmd", GetGatewayDefinitionsCmd, []string{"name", "config", "version", "output", "control-plane-name"}},
		{"CreateGatewayDefinitionCmd", CreateGatewayDefinitionCmd, []string{"config", "stdin", "control-plane-name", "version"}},
		{"ReplaceGatewayDefinitionCmd", ReplaceGatewayDefinitionCmd, []string{"config", "stdin", "name", "control-plane-name", "version"}},
		{"DeleteGatewayDefinitionCmd", DeleteGatewayDefinitionCmd, []string{"config", "name", "control-plane-name", "version"}},
		{"GetGatewayInstancesCmd", GetGatewayInstancesCmd, []string{"name", "config", "version", "output", "control-plane-name"}},
		{"CreateGatewayInstanceCmd", CreateGatewayInstanceCmd, []string{"config", "stdin", "control-plane-name", "version"}},
		{"ReplaceGatewayInstanceCmd", ReplaceGatewayInstanceCmd, []string{"config", "stdin", "name", "control-plane-name", "version"}},
		{"DeleteGatewayInstanceCmd", DeleteGatewayInstanceCmd, []string{"config", "name", "control-plane-name", "version"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// each expected flag must be discoverable on the command.
			for _, f := range tt.flags {
				if tt.cmd.Flags().Lookup(f) == nil {
					t.Errorf("flag %q is not registered", f)
				}
			}
		})
	}
}

// TestGatewayFlagDefaults asserts the default values for --version and
// --output so tptctl behaves consistently when the user omits them.
func TestGatewayFlagDefaults(t *testing.T) {
	// each entry declares an expected default for a specific (command, flag).
	tests := []struct {
		name        string
		cmd         *cobra.Command
		flag        string
		wantDefault string
	}{
		{"GetDomainNamesCmd version default", GetDomainNamesCmd, "version", "v0"},
		{"GetDomainNamesCmd output default", GetDomainNamesCmd, "output", "tabular"},
		{"CreateDomainNameCmd version default", CreateDomainNameCmd, "version", "v0"},
		{"DeleteDomainNameCmd version default", DeleteDomainNameCmd, "version", "v0"},
		{"GetDomainNameDefinitionsCmd version default", GetDomainNameDefinitionsCmd, "version", "v0"},
		{"GetDomainNameDefinitionsCmd output default", GetDomainNameDefinitionsCmd, "output", "tabular"},
		{"ReplaceDomainNameDefinitionCmd version default", ReplaceDomainNameDefinitionCmd, "version", "v0"},
		{"GetDomainNameInstancesCmd output default", GetDomainNameInstancesCmd, "output", "tabular"},
		{"ReplaceDomainNameInstanceCmd version default", ReplaceDomainNameInstanceCmd, "version", "v0"},
		{"GetGatewaysCmd version default", GetGatewaysCmd, "version", "v0"},
		{"GetGatewaysCmd output default", GetGatewaysCmd, "output", "tabular"},
		{"CreateGatewayCmd version default", CreateGatewayCmd, "version", "v0"},
		{"GetGatewayDefinitionsCmd output default", GetGatewayDefinitionsCmd, "output", "tabular"},
		{"ReplaceGatewayDefinitionCmd version default", ReplaceGatewayDefinitionCmd, "version", "v0"},
		{"GetGatewayInstancesCmd output default", GetGatewayInstancesCmd, "output", "tabular"},
		{"ReplaceGatewayInstanceCmd version default", ReplaceGatewayInstanceCmd, "version", "v0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// look up the flag, then compare DefValue against expectation.
			f := tt.cmd.Flags().Lookup(tt.flag)
			if f == nil {
				t.Fatalf("flag %q not registered", tt.flag)
			}
			if f.DefValue != tt.wantDefault {
				t.Errorf("flag %q DefValue = %q, want %q", tt.flag, f.DefValue, tt.wantDefault)
			}
		})
	}
}

// TestGatewayReplaceCommandsRequireName asserts every replace command marks
// --name as required, matching MarkFlagRequired("name") in gateway.go.
func TestGatewayReplaceCommandsRequireName(t *testing.T) {
	// every replace command in gateway.go marks --name as required.
	tests := []struct {
		name string
		cmd  *cobra.Command
	}{
		{"ReplaceDomainNameDefinitionCmd", ReplaceDomainNameDefinitionCmd},
		{"ReplaceDomainNameInstanceCmd", ReplaceDomainNameInstanceCmd},
		{"ReplaceGatewayDefinitionCmd", ReplaceGatewayDefinitionCmd},
		{"ReplaceGatewayInstanceCmd", ReplaceGatewayInstanceCmd},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// cobra records required flags in an annotation on the flag itself.
			f := tt.cmd.Flags().Lookup("name")
			if f == nil {
				t.Fatalf("flag \"name\" not registered")
			}
			required, ok := f.Annotations[cobra.BashCompOneRequiredFlag]
			if !ok || len(required) == 0 || required[0] != "true" {
				t.Errorf("--name is not marked required (annotations = %v)", f.Annotations)
			}
		})
	}
}

// TestGatewayCommandsAttachedToParents asserts each gateway command is
// registered with the appropriate parent verb command
// (Get / Create / Replace / Delete).
func TestGatewayCommandsAttachedToParents(t *testing.T) {
	// each entry maps a gateway command to the parent verb command it must live under.
	tests := []struct {
		name   string
		cmd    *cobra.Command
		parent *cobra.Command
	}{
		{"GetDomainNamesCmd", GetDomainNamesCmd, GetCmd},
		{"CreateDomainNameCmd", CreateDomainNameCmd, CreateCmd},
		{"DeleteDomainNameCmd", DeleteDomainNameCmd, DeleteCmd},
		{"GetDomainNameDefinitionsCmd", GetDomainNameDefinitionsCmd, GetCmd},
		{"CreateDomainNameDefinitionCmd", CreateDomainNameDefinitionCmd, CreateCmd},
		{"ReplaceDomainNameDefinitionCmd", ReplaceDomainNameDefinitionCmd, ReplaceCmd},
		{"DeleteDomainNameDefinitionCmd", DeleteDomainNameDefinitionCmd, DeleteCmd},
		{"GetDomainNameInstancesCmd", GetDomainNameInstancesCmd, GetCmd},
		{"CreateDomainNameInstanceCmd", CreateDomainNameInstanceCmd, CreateCmd},
		{"ReplaceDomainNameInstanceCmd", ReplaceDomainNameInstanceCmd, ReplaceCmd},
		{"DeleteDomainNameInstanceCmd", DeleteDomainNameInstanceCmd, DeleteCmd},
		{"GetGatewaysCmd", GetGatewaysCmd, GetCmd},
		{"CreateGatewayCmd", CreateGatewayCmd, CreateCmd},
		{"DeleteGatewayCmd", DeleteGatewayCmd, DeleteCmd},
		{"GetGatewayDefinitionsCmd", GetGatewayDefinitionsCmd, GetCmd},
		{"CreateGatewayDefinitionCmd", CreateGatewayDefinitionCmd, CreateCmd},
		{"ReplaceGatewayDefinitionCmd", ReplaceGatewayDefinitionCmd, ReplaceCmd},
		{"DeleteGatewayDefinitionCmd", DeleteGatewayDefinitionCmd, DeleteCmd},
		{"GetGatewayInstancesCmd", GetGatewayInstancesCmd, GetCmd},
		{"CreateGatewayInstanceCmd", CreateGatewayInstanceCmd, CreateCmd},
		{"ReplaceGatewayInstanceCmd", ReplaceGatewayInstanceCmd, ReplaceCmd},
		{"DeleteGatewayInstanceCmd", DeleteGatewayInstanceCmd, DeleteCmd},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// walk the parent's Commands() slice looking for the gateway command.
			found := false
			for _, c := range tt.parent.Commands() {
				if c == tt.cmd {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s is not registered as a subcommand of %s", tt.name, tt.parent.Use)
			}
			// parent linkage on the child must also point back.
			if tt.cmd.Parent() != tt.parent {
				t.Errorf("Parent() = %v, want %v", tt.cmd.Parent(), tt.parent)
			}
		})
	}
}

// TestGatewayFlagShorthandsMatch asserts that the shorthand flags gateway.go
// declares map to the letters users see in the command help.
func TestGatewayFlagShorthandsMatch(t *testing.T) {
	// each entry pins a (command, flag) to its expected one-letter shorthand.
	tests := []struct {
		name      string
		cmd       *cobra.Command
		flag      string
		shorthand string
	}{
		{"GetDomainNamesCmd name -n", GetDomainNamesCmd, "name", "n"},
		{"GetDomainNamesCmd config -c", GetDomainNamesCmd, "config", "c"},
		{"GetDomainNamesCmd version -v", GetDomainNamesCmd, "version", "v"},
		{"GetDomainNamesCmd output -o", GetDomainNamesCmd, "output", "o"},
		{"GetDomainNamesCmd control-plane-name -i", GetDomainNamesCmd, "control-plane-name", "i"},
		{"CreateDomainNameCmd config -c", CreateDomainNameCmd, "config", "c"},
		{"ReplaceDomainNameDefinitionCmd name -n", ReplaceDomainNameDefinitionCmd, "name", "n"},
		{"GetGatewaysCmd name -n", GetGatewaysCmd, "name", "n"},
		{"GetGatewaysCmd output -o", GetGatewaysCmd, "output", "o"},
		{"CreateGatewayCmd config -c", CreateGatewayCmd, "config", "c"},
		{"ReplaceGatewayDefinitionCmd name -n", ReplaceGatewayDefinitionCmd, "name", "n"},
		{"ReplaceGatewayInstanceCmd name -n", ReplaceGatewayInstanceCmd, "name", "n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// pflag stores the one-letter alias on the Flag itself.
			f := tt.cmd.Flags().Lookup(tt.flag)
			if f == nil {
				t.Fatalf("flag %q not registered", tt.flag)
			}
			if f.Shorthand != tt.shorthand {
				t.Errorf("flag %q shorthand = %q, want %q", tt.flag, f.Shorthand, tt.shorthand)
			}
		})
	}
}
