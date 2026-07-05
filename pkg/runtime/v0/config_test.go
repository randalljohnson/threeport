package v0

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// TestLoadRuntimeConfigEmptyPath asserts that an empty configFile short-circuits
// with no error and no viper state changes.
func TestLoadRuntimeConfigEmptyPath(t *testing.T) {
	// reset viper so a prior test can't influence this one
	viper.Reset()

	// call with empty string; should return nil without touching viper
	if err := LoadRuntimeConfig(""); err != nil {
		t.Fatalf("expected nil error for empty configFile, got %v", err)
	}

	// verify viper never received a config file path
	if viper.ConfigFileUsed() != "" {
		t.Fatalf("expected no config file set, got %q", viper.ConfigFileUsed())
	}
}

// TestLoadRuntimeConfigValidFile asserts that a well-formed config file is read
// and its keys become available via viper.
func TestLoadRuntimeConfigValidFile(t *testing.T) {
	viper.Reset()

	// write a minimal yaml config into a temp dir
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "runtime.yaml")
	contents := "sample_key: sample_value\n"
	if err := os.WriteFile(cfgPath, []byte(contents), 0o600); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	// load the config; should succeed
	if err := LoadRuntimeConfig(cfgPath); err != nil {
		t.Fatalf("expected nil error for valid config, got %v", err)
	}

	// verify viper picked the file up and parsed keys
	if viper.ConfigFileUsed() != cfgPath {
		t.Fatalf("expected ConfigFileUsed=%q, got %q", cfgPath, viper.ConfigFileUsed())
	}
	if got := viper.GetString("sample_key"); got != "sample_value" {
		t.Fatalf("expected sample_key=sample_value, got %q", got)
	}
}

// TestLoadRuntimeConfigMissingFile asserts that a missing config path returns a
// wrapped error rather than panicking or silently succeeding.
func TestLoadRuntimeConfigMissingFile(t *testing.T) {
	viper.Reset()

	// point at a path that does not exist
	missing := filepath.Join(t.TempDir(), "does-not-exist.yaml")

	// load should return a wrapped error
	err := LoadRuntimeConfig(missing)
	if err == nil {
		t.Fatal("expected error for missing config file, got nil")
	}
	// verify wrap prefix from the function
	if !strings.Contains(err.Error(), "failed to read api server config") {
		t.Fatalf("expected wrapped error prefix, got %v", err)
	}
}
