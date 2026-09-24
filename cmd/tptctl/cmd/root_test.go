package cmd

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	cli "github.com/threeport/threeport/pkg/cli/v0"
)

// TestRootCmdMetadata asserts rootCmd exposes stable Use, Short, and Long
// strings so `tptctl --help` output and CLI routing do not silently change.
func TestRootCmdMetadata(t *testing.T) {
	// verify Use matches the binary name
	if rootCmd.Use != "tptctl" {
		t.Errorf("rootCmd.Use = %q, want %q", rootCmd.Use, "tptctl")
	}
	// verify Short is set for cobra's help index
	if rootCmd.Short != "Manage Threeport" {
		t.Errorf("rootCmd.Short = %q, want %q", rootCmd.Short, "Manage Threeport")
	}
	// verify Long documents the plugin directory contract
	if !strings.Contains(rootCmd.Long, "THREEPORT_PLUGIN_DIR") {
		t.Errorf("rootCmd.Long missing THREEPORT_PLUGIN_DIR reference; got %q", rootCmd.Long)
	}
	if !strings.Contains(rootCmd.Long, "https://threeport.io") {
		t.Errorf("rootCmd.Long missing project URL; got %q", rootCmd.Long)
	}
}

// TestRootCmdPersistentFlagsRegistered asserts init() wired the
// --threeport-config and --provider-config persistent flags on rootCmd so
// they propagate to every subcommand.
func TestRootCmdPersistentFlagsRegistered(t *testing.T) {
	// verify threeport-config persistent flag exists
	if flag := rootCmd.PersistentFlags().Lookup("threeport-config"); flag == nil {
		t.Error("expected --threeport-config persistent flag to be registered")
	}
	// verify provider-config persistent flag exists
	if flag := rootCmd.PersistentFlags().Lookup("provider-config"); flag == nil {
		t.Error("expected --provider-config persistent flag to be registered")
	}
	// verify the toggle local flag defined in init exists
	if flag := rootCmd.Flags().Lookup("toggle"); flag == nil {
		t.Error("expected --toggle flag to be registered")
	}
}

// TestCliArgsInitialized asserts the package-level cliArgs pointer is a
// non-nil GenesisControlPlaneCLIArgs so flag binding in init() cannot panic.
func TestCliArgsInitialized(t *testing.T) {
	// non-nil struct pointer is required for cobra's StringVar binding
	if cliArgs == nil {
		t.Fatal("expected cliArgs to be non-nil")
	}
}

// TestGetClientContextHappyPath asserts GetClientContext returns each value
// stored on the command context under the documented key.
func TestGetClientContextHappyPath(t *testing.T) {
	// build a command with all four context values set to real objects
	client := &http.Client{}
	config := &cli.ThreeportConfig{}
	endpoint := "https://example.local:443"
	controlPlane := "test-cp"

	cmd := &cobra.Command{}
	ctx := context.WithValue(context.Background(), "apiClient", client)
	ctx = context.WithValue(ctx, "config", config)
	ctx = context.WithValue(ctx, "apiEndpoint", endpoint)
	ctx = context.WithValue(ctx, "requestedControlPlane", controlPlane)
	cmd.SetContext(ctx)

	// action under test: extract all four values from the context
	gotClient, gotConfig, gotEndpoint, gotControlPlane := GetClientContext(cmd)

	// verify each returned value matches what was stored
	if gotClient != client {
		t.Errorf("apiClient = %p, want %p", gotClient, client)
	}
	if gotConfig != config {
		t.Errorf("config = %p, want %p", gotConfig, config)
	}
	if gotEndpoint != endpoint {
		t.Errorf("apiEndpoint = %q, want %q", gotEndpoint, endpoint)
	}
	if gotControlPlane != controlPlane {
		t.Errorf("requestedControlPlane = %q, want %q", gotControlPlane, controlPlane)
	}
}

// TestGetClientContextEmptyContext asserts GetClientContext returns zero
// values when the command context has none of the expected keys set.
func TestGetClientContextEmptyContext(t *testing.T) {
	// bare command carries only the background context
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	// action under test: read from an empty context
	client, config, endpoint, controlPlane := GetClientContext(cmd)

	// verify all four returns fall back to Go zero values
	if client != nil {
		t.Errorf("expected nil client, got %v", client)
	}
	if config != nil {
		t.Errorf("expected nil config, got %v", config)
	}
	if endpoint != "" {
		t.Errorf("expected empty endpoint, got %q", endpoint)
	}
	if controlPlane != "" {
		t.Errorf("expected empty controlPlane, got %q", controlPlane)
	}
}

// TestGetClientContextWrongTypes asserts GetClientContext ignores values
// stored under the expected keys when their runtime types do not match the
// type assertions inside the getter.
func TestGetClientContextWrongTypes(t *testing.T) {
	// populate every key with an int; each assertion inside the getter must reject it
	cmd := &cobra.Command{}
	ctx := context.WithValue(context.Background(), "apiClient", 42)
	ctx = context.WithValue(ctx, "config", 42)
	ctx = context.WithValue(ctx, "apiEndpoint", 42)
	ctx = context.WithValue(ctx, "requestedControlPlane", 42)
	cmd.SetContext(ctx)

	// action under test: getter should discard type-mismatched entries
	client, config, endpoint, controlPlane := GetClientContext(cmd)

	// verify each field defaulted to its zero value
	if client != nil {
		t.Errorf("expected nil client on wrong type, got %v", client)
	}
	if config != nil {
		t.Errorf("expected nil config on wrong type, got %v", config)
	}
	if endpoint != "" {
		t.Errorf("expected empty endpoint on wrong type, got %q", endpoint)
	}
	if controlPlane != "" {
		t.Errorf("expected empty controlPlane on wrong type, got %q", controlPlane)
	}
}

// TestGetClientContextPartialContext asserts GetClientContext returns each
// stored value independently: setting only two keys should leave the other
// two returns at their zero value.
func TestGetClientContextPartialContext(t *testing.T) {
	// only apiEndpoint and requestedControlPlane are set
	cmd := &cobra.Command{}
	ctx := context.WithValue(context.Background(), "apiEndpoint", "https://only.local")
	ctx = context.WithValue(ctx, "requestedControlPlane", "only")
	cmd.SetContext(ctx)

	// action under test: read a partially-populated context
	client, config, endpoint, controlPlane := GetClientContext(cmd)

	// verify unset entries stay zero
	if client != nil {
		t.Errorf("expected nil client, got %v", client)
	}
	if config != nil {
		t.Errorf("expected nil config, got %v", config)
	}
	// verify set entries pass through
	if endpoint != "https://only.local" {
		t.Errorf("apiEndpoint = %q, want %q", endpoint, "https://only.local")
	}
	if controlPlane != "only" {
		t.Errorf("requestedControlPlane = %q, want %q", controlPlane, "only")
	}
}

// TestLoadPluginsMissingDir asserts loadPlugins returns an empty slice and
// does not exit when the plugin directory does not exist.
func TestLoadPluginsMissingDir(t *testing.T) {
	// point at a path guaranteed not to exist under a temp dir
	missing := filepath.Join(t.TempDir(), "does-not-exist")

	// action under test: loadPlugins on a missing dir
	got := loadPlugins(missing)

	// verify empty slice returned (nil-safe len)
	if len(got) != 0 {
		t.Errorf("expected empty slice for missing dir, got %v", got)
	}
}

// TestLoadPluginsEmptyDir asserts loadPlugins returns an empty slice when
// the plugin directory exists but contains no files.
func TestLoadPluginsEmptyDir(t *testing.T) {
	// existing but empty directory
	dir := t.TempDir()

	// action under test: loadPlugins on an empty dir
	got := loadPlugins(dir)

	// verify empty slice returned
	if len(got) != 0 {
		t.Errorf("expected empty slice for empty dir, got %v", got)
	}
}

// TestLoadPluginsCollectsFiles asserts loadPlugins walks the directory and
// returns every file found, ignoring directory entries themselves.
func TestLoadPluginsCollectsFiles(t *testing.T) {
	// build a directory tree with two files at the top and one nested
	dir := t.TempDir()
	fileA := filepath.Join(dir, "plugin-a")
	fileB := filepath.Join(dir, "plugin-b")
	sub := filepath.Join(dir, "sub")
	fileC := filepath.Join(sub, "plugin-c")
	if err := os.WriteFile(fileA, []byte("a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileB, []byte("b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileC, []byte("c"), 0o755); err != nil {
		t.Fatal(err)
	}

	// action under test: walk the plugin dir
	got := loadPlugins(dir)

	// verify every file (including nested) is returned, none of the dir entries
	want := map[string]bool{fileA: false, fileB: false, fileC: false}
	for _, p := range got {
		if _, ok := want[p]; ok {
			want[p] = true
		}
	}
	for p, seen := range want {
		if !seen {
			t.Errorf("expected file %q in result, got %v", p, got)
		}
	}
	// verify the sub directory itself is not counted as a plugin
	for _, p := range got {
		if p == sub {
			t.Errorf("directory entry %q should not be included", sub)
		}
	}
}

// TestInitializeCommandContextConfigError asserts initializeCommandContext
// surfaces a wrapped error when the underlying threeport config cannot be
// loaded (no config file, no THREEPORT_CONFIG env var).
func TestInitializeCommandContextConfigError(t *testing.T) {
	// isolate HOME so no real ~/.threeport/config.yaml is picked up
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("THREEPORT_CONFIG", "")

	// snapshot and reset cliArgs.ControlPlaneName so a stray previous test cannot pin it
	prev := cliArgs.ControlPlaneName
	cliArgs.ControlPlaneName = ""
	t.Cleanup(func() { cliArgs.ControlPlaneName = prev })

	// bare command with a background context
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	// action under test: call the helper directly so os.Exit inside
	// CommandPreRunFunc is not on the path
	err := initializeCommandContext(cmd)

	// verify an error is returned and wraps a description of the failure
	if err == nil {
		t.Fatal("expected error when threeport config cannot be resolved")
	}
	if !strings.Contains(err.Error(), "failed to") {
		t.Errorf("expected wrapped %q-style error, got %v", "failed to", err)
	}
}
