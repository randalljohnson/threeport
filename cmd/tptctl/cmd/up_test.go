package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	v0 "github.com/threeport/threeport/pkg/api/v0"
)

// TestUpCmdMetadata asserts UpCmd's Use, Short, Long, Example, and behavior flags match the documented values.
func TestUpCmdMetadata(t *testing.T) {
	// verify Use string matches CLI invocation
	if UpCmd.Use != "up" {
		t.Errorf("UpCmd.Use = %q, want %q", UpCmd.Use, "up")
	}
	// verify Short description matches the documented help string
	if UpCmd.Short != "Spin up a new deployment of the Threeport control plane" {
		t.Errorf("UpCmd.Short = %q, want documented short description", UpCmd.Short)
	}
	// verify Long description is set for cobra help output
	if UpCmd.Long == "" {
		t.Errorf("UpCmd.Long is empty, want non-empty description")
	}
	// verify Example demonstrates the required --name flag
	if !strings.Contains(UpCmd.Example, "--name") {
		t.Errorf("UpCmd.Example = %q, want mention of --name", UpCmd.Example)
	}
	// verify usage is silenced on error so failures don't dump the help block
	if !UpCmd.SilenceUsage {
		t.Errorf("UpCmd.SilenceUsage = false, want true")
	}
	// verify PreRun hook wired so provider-specific flag requirements apply
	if UpCmd.PreRun == nil {
		t.Errorf("UpCmd.PreRun = nil, want a function")
	}
	// verify Run hook wired so cobra dispatch has a target
	if UpCmd.Run == nil {
		t.Errorf("UpCmd.Run = nil, want a function")
	}
}

// TestUpCmdRegisteredOnRoot asserts UpCmd is a subcommand of rootCmd via init().
func TestUpCmdRegisteredOnRoot(t *testing.T) {
	// verify subcommand registration so `tptctl up` resolves at the top level
	if !hasSubcommand(rootCmd, UpCmd) {
		t.Errorf("UpCmd not registered under rootCmd")
	}
}

// TestUpCmdFlags asserts UpCmd registers each documented flag from the up.go init().
func TestUpCmdFlags(t *testing.T) {
	// verify every documented flag exists on the command
	assertFlags(t, UpCmd, []string{
		"name",
		"provider",
		"kind-kubeconfig",
		"aws-config-profile",
		"aws-config-env",
		"aws-region",
		"oci-region",
		"oci-config-profile",
		"gcp-project-id",
		"gcp-region",
		"force-overwrite-config",
		"auth-enabled",
		"root-domain",
		"admin-email",
		"control-plane-image-namespace",
		"control-plane-image-tag",
		"num-worker-nodes",
		"debug",
		"teardown-on-failure",
		"control-plane-only",
		"cluster-name",
		"infra-only",
		"local-registry",
		"kind-port-mappings",
	})
}

// TestUpCmdFlagDefaults asserts every registered flag defaults to the value documented in init().
func TestUpCmdFlagDefaults(t *testing.T) {
	// verify defaults so behavior only changes on explicit opt-in
	cases := []struct {
		flag string
		want string
	}{
		{"name", ""},
		{"provider", "kind"},
		{"kind-kubeconfig", ""},
		{"aws-config-profile", "default"},
		{"aws-config-env", "false"},
		{"aws-region", ""},
		{"oci-region", ""},
		{"oci-config-profile", "DEFAULT"},
		{"gcp-project-id", ""},
		{"gcp-region", ""},
		{"force-overwrite-config", "false"},
		{"auth-enabled", "true"},
		{"root-domain", ""},
		{"admin-email", ""},
		{"control-plane-image-namespace", ""},
		{"control-plane-image-tag", ""},
		{"num-worker-nodes", "0"},
		{"debug", "false"},
		{"teardown-on-failure", "false"},
		{"control-plane-only", "false"},
		{"cluster-name", ""},
		{"infra-only", "false"},
		{"local-registry", "false"},
		{"kind-port-mappings", "[]"},
	}
	for _, tc := range cases {
		t.Run(tc.flag, func(t *testing.T) {
			// verify flag default matches value documented in init()
			f := UpCmd.Flags().Lookup(tc.flag)
			if f == nil {
				t.Fatalf("flag %q missing on UpCmd", tc.flag)
			}
			if f.DefValue != tc.want {
				t.Errorf("flag %q default = %q, want %q", tc.flag, f.DefValue, tc.want)
			}
		})
	}
}

// TestUpCmdShorthandFlags asserts every flag registered with a shorthand exposes the documented letter.
func TestUpCmdShorthandFlags(t *testing.T) {
	// verify shorthand letters so short-form invocations resolve to the intended flag
	cases := []struct {
		flag string
		want string
	}{
		{"name", "n"},
		{"provider", "p"},
		{"control-plane-image-namespace", "r"},
		{"control-plane-image-tag", "t"},
	}
	for _, tc := range cases {
		t.Run(tc.flag, func(t *testing.T) {
			f := UpCmd.Flags().Lookup(tc.flag)
			if f == nil {
				t.Fatalf("flag %q missing on UpCmd", tc.flag)
			}
			if f.Shorthand != tc.want {
				t.Errorf("flag %q shorthand = %q, want %q", tc.flag, f.Shorthand, tc.want)
			}
		})
	}
}

// TestUpCmdNameRequired asserts init() marks --name as required so cobra rejects invocations omitting it.
func TestUpCmdNameRequired(t *testing.T) {
	// verify name flag has the required annotation applied via MarkFlagRequired
	name := UpCmd.Flags().Lookup("name")
	if name == nil {
		t.Fatalf("name flag missing on UpCmd")
	}
	req, ok := name.Annotations[cobra.BashCompOneRequiredFlag]
	if !ok || len(req) == 0 || req[0] != "true" {
		t.Errorf("name flag not marked required on UpCmd (annotations=%v)", name.Annotations)
	}
}

// TestUpCmdProviderHelpMentionsSupported asserts the --provider flag usage lists the supported providers.
func TestUpCmdProviderHelpMentionsSupported(t *testing.T) {
	// verify usage names each supported provider so `--help` output stays in sync with the enum
	provider := UpCmd.Flags().Lookup("provider")
	if provider == nil {
		t.Fatalf("provider flag missing on UpCmd")
	}
	// verify every supported provider is named in the flag usage
	for _, p := range v0.SupportedInfraProviders() {
		if !strings.Contains(provider.Usage, string(p)) {
			t.Errorf("provider flag usage missing %q; got %q", string(p), provider.Usage)
		}
	}
}

// TestUpCmdPreRunMarksRegionRequired covers PreRun's provider-branching behavior and asserts
// EKS/OKE selection marks the matching region flag required while other providers leave it optional.
func TestUpCmdPreRunMarksRegionRequired(t *testing.T) {
	cases := []struct {
		// name identifies the branch under test
		name string
		// provider is written to cliArgs.InfraProvider before PreRun runs
		provider string
		// wantRequired lists flags whose required annotation must be set after PreRun
		wantRequired []string
		// wantNotRequired lists flags whose required annotation must remain unset after PreRun
		wantNotRequired []string
	}{
		{
			// eks branch marks aws-region required
			name:            "eks marks aws-region required",
			provider:        v0.KubernetesRuntimeInfraProviderEKS,
			wantRequired:    []string{"aws-region"},
			wantNotRequired: []string{"oci-region"},
		},
		{
			// oke branch marks oci-region required
			name:            "oke marks oci-region required",
			provider:        v0.KubernetesRuntimeInfraProviderOKE,
			wantRequired:    []string{"oci-region"},
			wantNotRequired: []string{"aws-region"},
		},
		{
			// kind branch leaves both region flags optional
			name:            "kind leaves both region flags optional",
			provider:        v0.KubernetesRuntimeInfraProviderKind,
			wantNotRequired: []string{"aws-region", "oci-region"},
		},
		{
			// gke branch leaves both aws-region and oci-region optional; gcp uses different flags
			name:            "gke leaves aws and oci regions optional",
			provider:        v0.KubernetesRuntimeInfraProviderGKE,
			wantNotRequired: []string{"aws-region", "oci-region"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// arrange: rebuild a fresh command sharing UpCmd's PreRun and matching flags so the
			// annotation mutation the branch performs isn't observed by sibling test cases
			cmd := newUpCmdForPreRunTest(t)

			// arrange: point the shared cliArgs at the branch under test and restore afterward
			prev := cliArgs.InfraProvider
			cliArgs.InfraProvider = tc.provider
			t.Cleanup(func() { cliArgs.InfraProvider = prev })

			// act: invoke PreRun to trigger the branch-specific MarkFlagRequired call
			cmd.PreRun(cmd, nil)

			// assert: flags the branch marks required carry the annotation
			for _, name := range tc.wantRequired {
				f := cmd.Flags().Lookup(name)
				if f == nil {
					t.Fatalf("flag %q missing on cloned up command", name)
				}
				req, ok := f.Annotations[cobra.BashCompOneRequiredFlag]
				if !ok || len(req) == 0 || req[0] != "true" {
					t.Errorf("flag %q not marked required for provider %q (annotations=%v)", name, tc.provider, f.Annotations)
				}
			}
			// assert: flags the branch does not touch remain unmarked
			for _, name := range tc.wantNotRequired {
				f := cmd.Flags().Lookup(name)
				if f == nil {
					t.Fatalf("flag %q missing on cloned up command", name)
				}
				if req, ok := f.Annotations[cobra.BashCompOneRequiredFlag]; ok && len(req) > 0 && req[0] == "true" {
					t.Errorf("flag %q unexpectedly marked required for provider %q", name, tc.provider)
				}
			}
		})
	}
}

// newUpCmdForPreRunTest builds a cobra command that shares UpCmd's PreRun and mirrors the
// aws-region/oci-region flag pair; each test case gets its own instance so PreRun's
// MarkFlagRequired mutation on one branch doesn't leak into sibling cases.
func newUpCmdForPreRunTest(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{
		Use:    UpCmd.Use,
		PreRun: UpCmd.PreRun,
	}
	// register a fresh copy of the two region flags PreRun may mark required
	var awsRegion, ociRegion string
	cmd.Flags().StringVar(&awsRegion, "aws-region", "", "AWS region")
	cmd.Flags().StringVar(&ociRegion, "oci-region", "", "OCI region")
	return cmd
}
