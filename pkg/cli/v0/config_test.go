package v0

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mitchellh/go-homedir"
	"github.com/spf13/viper"

	util "github.com/threeport/threeport/pkg/util/v0"
)

// setHome points homedir.Dir() at a scratch path and resets its cache so
// consecutive tests are not affected by a previously cached lookup.
func setHome(t *testing.T, path string) {
	t.Helper()
	t.Setenv("HOME", path)
	homedir.Reset()
	t.Cleanup(homedir.Reset)
}

// sampleConfig returns a ThreeportConfig populated with a few control planes
// for use across cases that only need to read fields.
func sampleConfig() *ThreeportConfig {
	return &ThreeportConfig{
		CurrentControlPlane: "alpha",
		ControlPlanes: []ControlPlane{
			{
				Name:          "alpha",
				AuthEnabled:   true,
				Genesis:       true,
				APIServer:     "https://alpha.example.com",
				Provider:      "eks",
				EncryptionKey: "alpha-key",
			},
			{
				Name:          "beta",
				AuthEnabled:   false,
				Genesis:       false,
				APIServer:     "https://beta.example.com",
				Provider:      "kind",
				EncryptionKey: "beta-key",
			},
		},
	}
}

// TestGetAllControlPlaneNames covers extracting every control plane name and
// the empty-slice boundary.
func TestGetAllControlPlaneNames(t *testing.T) {
	tests := []struct {
		name string
		cfg  *ThreeportConfig
		want []string
	}{
		{
			name: "returns every configured name",
			cfg:  sampleConfig(),
			want: []string{"alpha", "beta"},
		},
		{
			name: "returns nil when no control planes",
			cfg:  &ThreeportConfig{},
			want: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// invoke the accessor
			got := tc.cfg.GetAllControlPlaneNames()
			// assert the returned slice matches the expected names
			if len(got) != len(tc.want) {
				t.Fatalf("length mismatch: got %v want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("index %d: got %q want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestCheckThreeportConfigEmpty asserts empty detection reports true only when
// no control planes are configured.
func TestCheckThreeportConfigEmpty(t *testing.T) {
	// empty config reports empty
	if !(&ThreeportConfig{}).CheckThreeportConfigEmpty() {
		t.Fatal("expected empty config to report empty")
	}
	// populated config reports non-empty
	if sampleConfig().CheckThreeportConfigEmpty() {
		t.Fatal("expected populated config to report non-empty")
	}
}

// TestCheckThreeportControlPlaneExists asserts lookup by name returns true
// only for a configured control plane.
func TestCheckThreeportControlPlaneExists(t *testing.T) {
	cfg := sampleConfig()
	// existing name is found
	if !cfg.CheckThreeportControlPlaneExists("alpha") {
		t.Fatal("expected alpha to exist")
	}
	// missing name is not found
	if cfg.CheckThreeportControlPlaneExists("missing") {
		t.Fatal("expected missing to not exist")
	}
}

// TestGetControlPlaneConfig covers happy path and not-found error.
func TestGetControlPlaneConfig(t *testing.T) {
	cfg := sampleConfig()

	// happy path returns the named control plane
	cp, err := cfg.GetControlPlaneConfig("beta")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cp.Name != "beta" || cp.Provider != "kind" {
		t.Fatalf("unexpected control plane: %+v", cp)
	}

	// missing name returns a not-found error
	if _, err := cfg.GetControlPlaneConfig("missing"); err == nil {
		t.Fatal("expected error for missing control plane")
	}
}

// TestSimpleFieldGetters covers the per-field lookup wrappers around
// GetControlPlaneConfig, including the not-found error branch of each.
func TestSimpleFieldGetters(t *testing.T) {
	cfg := sampleConfig()

	// APIServer returns the configured endpoint
	if endpoint, err := cfg.GetThreeportAPIEndpoint("alpha"); err != nil || endpoint != "https://alpha.example.com" {
		t.Fatalf("APIEndpoint: got %q err %v", endpoint, err)
	}
	// APIServer returns an error when the control plane is missing
	if _, err := cfg.GetThreeportAPIEndpoint("missing"); err == nil {
		t.Fatal("expected error for missing endpoint")
	}

	// AuthEnabled reflects the configured bool
	if enabled, err := cfg.GetThreeportAuthEnabled("alpha"); err != nil || !enabled {
		t.Fatalf("AuthEnabled: got %v err %v", enabled, err)
	}
	if enabled, err := cfg.GetThreeportAuthEnabled("beta"); err != nil || enabled {
		t.Fatalf("AuthEnabled: got %v err %v", enabled, err)
	}
	// AuthEnabled returns an error when the control plane is missing
	if _, err := cfg.GetThreeportAuthEnabled("missing"); err == nil {
		t.Fatal("expected error for missing auth flag")
	}

	// EncryptionKey returns the configured key
	if key, err := cfg.GetThreeportEncryptionKey("alpha"); err != nil || key != "alpha-key" {
		t.Fatalf("EncryptionKey: got %q err %v", key, err)
	}
	if _, err := cfg.GetThreeportEncryptionKey("missing"); err == nil {
		t.Fatal("expected error for missing encryption key")
	}

	// InfraProvider returns the configured provider
	if provider, err := cfg.GetThreeportInfraProvider("beta"); err != nil || provider != "kind" {
		t.Fatalf("Provider: got %q err %v", provider, err)
	}
	if _, err := cfg.GetThreeportInfraProvider("missing"); err == nil {
		t.Fatal("expected error for missing provider")
	}

	// GenesisControlPlane reflects the genesis flag
	if genesis, err := cfg.CheckThreeportGenesisControlPlane("alpha"); err != nil || !genesis {
		t.Fatalf("Genesis: got %v err %v", genesis, err)
	}
	if genesis, err := cfg.CheckThreeportGenesisControlPlane("beta"); err != nil || genesis {
		t.Fatalf("Genesis: got %v err %v", genesis, err)
	}
	if _, err := cfg.CheckThreeportGenesisControlPlane("missing"); err == nil {
		t.Fatal("expected error for missing genesis flag")
	}
}

// TestGetThreeportCertificatesForControlPlane covers the base64-decoded cert
// return, the auth-disabled fallback, the CA decode error, and the not-found
// error branch.
func TestGetThreeportCertificatesForControlPlane(t *testing.T) {
	// build a control plane whose CA and matching credential are base64-encoded
	caPlain := "ca-cert-body"
	clientCertPlain := "client-cert-body"
	clientKeyPlain := "client-key-body"
	cfg := &ThreeportConfig{
		ControlPlanes: []ControlPlane{
			{
				Name:   "auth-on",
				CACert: util.Base64Encode(caPlain),
				Credentials: []Credential{
					{
						Name:       "auth-on",
						ClientCert: util.Base64Encode(clientCertPlain),
						ClientKey:  util.Base64Encode(clientKeyPlain),
					},
				},
			},
			{
				// no credential matches the control plane name; treated as auth disabled
				Name:   "auth-off",
				CACert: util.Base64Encode(caPlain),
				Credentials: []Credential{
					{
						Name:       "unrelated",
						ClientCert: util.Base64Encode(clientCertPlain),
						ClientKey:  util.Base64Encode(clientKeyPlain),
					},
				},
			},
			{
				// CACert is not valid base64
				Name:   "bad-ca",
				CACert: "!!!not-base64!!!",
			},
		},
	}

	// happy path returns decoded CA + client cert + key
	ca, cert, key, err := cfg.GetThreeportCertificatesForControlPlane("auth-on")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ca != caPlain || cert != clientCertPlain || key != clientKeyPlain {
		t.Fatalf("unexpected decoded values: %q %q %q", ca, cert, key)
	}

	// no matching credential returns empty strings with no error (auth disabled)
	ca, cert, key, err = cfg.GetThreeportCertificatesForControlPlane("auth-off")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ca != "" || cert != "" || key != "" {
		t.Fatalf("expected empty triple, got %q %q %q", ca, cert, key)
	}

	// invalid base64 on the CA surfaces a decode error
	if _, _, _, err := cfg.GetThreeportCertificatesForControlPlane("bad-ca"); err == nil {
		t.Fatal("expected CA decode error")
	}

	// missing control plane returns an error
	if _, _, _, err := cfg.GetThreeportCertificatesForControlPlane("missing"); err == nil {
		t.Fatal("expected error for missing control plane")
	}
}

// TestGetThreeportCertificatesBadClientCert covers the client-cert and
// client-key decode error branches.
func TestGetThreeportCertificatesBadClientCert(t *testing.T) {
	cfg := &ThreeportConfig{
		ControlPlanes: []ControlPlane{
			{
				Name:   "bad-cert",
				CACert: util.Base64Encode("ca"),
				Credentials: []Credential{
					{
						Name:       "bad-cert",
						ClientCert: "!!!invalid!!!",
						ClientKey:  util.Base64Encode("key"),
					},
				},
			},
			{
				Name:   "bad-key",
				CACert: util.Base64Encode("ca"),
				Credentials: []Credential{
					{
						Name:       "bad-key",
						ClientCert: util.Base64Encode("cert"),
						ClientKey:  "!!!invalid!!!",
					},
				},
			},
		},
	}

	// invalid client cert surfaces a decode error
	if _, _, _, err := cfg.GetThreeportCertificatesForControlPlane("bad-cert"); err == nil {
		t.Fatal("expected client cert decode error")
	}
	// invalid client key surfaces a decode error
	if _, _, _, err := cfg.GetThreeportCertificatesForControlPlane("bad-key"); err == nil {
		t.Fatal("expected client key decode error")
	}
}

// TestDefaultThreeportConfigPath verifies the ~/.threeport suffix layout.
func TestDefaultThreeportConfigPath(t *testing.T) {
	got := DefaultThreeportConfigPath("/home/user")
	want := filepath.Join("/home/user", ".threeport")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// TestDefaultProviderConfigDir asserts the default config dir is created
// under the homedir the HOME env var points to.
func TestDefaultProviderConfigDir(t *testing.T) {
	// point homedir at a scratch directory
	tmp := t.TempDir()
	setHome(t, tmp)

	// invoke and assert the returned path plus that the directory exists
	dir, err := DefaultProviderConfigDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(tmp, ".threeport")
	if dir != want {
		t.Fatalf("got %q want %q", dir, want)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("expected directory at %q: err %v info %v", dir, err, info)
	}
}

// TestDefaultPluginDir asserts the plugin dir lives under ~/.threeport/plugins.
func TestDefaultPluginDir(t *testing.T) {
	tmp := t.TempDir()
	setHome(t, tmp)

	dir, err := DefaultPluginDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(tmp, ".threeport", "plugins")
	if dir != want {
		t.Fatalf("got %q want %q", dir, want)
	}
}

// TestDetermineThreeportConfigPath covers the explicit-flag, env-var, and
// fallback branches in precedence order.
func TestDetermineThreeportConfigPath(t *testing.T) {
	// explicit path wins over env and default
	t.Run("explicit path wins", func(t *testing.T) {
		t.Setenv(ThreeportConfigEnvKey, "/from/env.yaml")
		got := DetermineThreeportConfigPath("/explicit/path.yaml")
		if got != "/explicit/path.yaml" {
			t.Fatalf("got %q", got)
		}
	})

	// env var used when no explicit path
	t.Run("env var used when no explicit path", func(t *testing.T) {
		t.Setenv(ThreeportConfigEnvKey, "/from/env.yaml")
		got := DetermineThreeportConfigPath("")
		if got != "/from/env.yaml" {
			t.Fatalf("got %q", got)
		}
	})

	// fallback to homedir default when neither is set
	t.Run("fallback to home default", func(t *testing.T) {
		os.Unsetenv(ThreeportConfigEnvKey)
		tmp := t.TempDir()
		setHome(t, tmp)
		got := DetermineThreeportConfigPath("")
		want := filepath.Join(tmp, ".threeport", "config.yaml")
		if got != want {
			t.Fatalf("got %q want %q", got, want)
		}
	})
}

// setupViperFile initializes viper against a fresh config file in a temp dir
// so tests that hit viper.WriteConfig() land in an isolated location.
func setupViperFile(t *testing.T) string {
	t.Helper()
	viper.Reset()
	t.Cleanup(func() { viper.Reset() })

	dir := t.TempDir()
	file := filepath.Join(dir, "config.yaml")
	// create an empty file so viper's read/write cycle has a target
	if err := os.WriteFile(file, []byte{}, 0o600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}
	viper.SetConfigFile(file)
	viper.SetConfigType("yaml")
	return file
}

// TestSetCurrentControlPlane asserts the setter writes CurrentControlPlane
// through viper.
func TestSetCurrentControlPlane(t *testing.T) {
	setupViperFile(t)

	cfg := &ThreeportConfig{}
	cfg.SetCurrentControlPlane("primary")

	// viper carries the new value in memory
	if got := viper.GetString("CurrentControlPlane"); got != "primary" {
		t.Fatalf("got %q want %q", got, "primary")
	}
}

// TestSetCurrentInstance asserts the setter writes CurrentInstance through
// viper.
func TestSetCurrentInstance(t *testing.T) {
	setupViperFile(t)

	cfg := &ThreeportConfig{}
	cfg.SetCurrentInstance("primary-instance")

	if got := viper.GetString("CurrentInstance"); got != "primary-instance" {
		t.Fatalf("got %q want %q", got, "primary-instance")
	}
}

// TestUpdateThreeportConfigAppend covers appending a new control plane and
// setting it as current.
func TestUpdateThreeportConfigAppend(t *testing.T) {
	setupViperFile(t)

	cfg := &ThreeportConfig{}
	newCP := &ControlPlane{Name: "new", Provider: "kind"}

	// append the new control plane
	if err := UpdateThreeportConfig(cfg, newCP); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// slice grew by one
	if len(cfg.ControlPlanes) != 1 || cfg.ControlPlanes[0].Name != "new" {
		t.Fatalf("append did not record control plane: %+v", cfg.ControlPlanes)
	}
	// viper reflects the new current control plane
	if got := viper.GetString("CurrentControlPlane"); got != "new" {
		t.Fatalf("current control plane got %q", got)
	}
}

// TestUpdateThreeportConfigReplace covers updating an existing control plane
// in place.
func TestUpdateThreeportConfigReplace(t *testing.T) {
	setupViperFile(t)

	cfg := &ThreeportConfig{
		ControlPlanes: []ControlPlane{{Name: "existing", Provider: "kind"}},
	}
	updated := &ControlPlane{Name: "existing", Provider: "eks"}

	// call replaces the existing control plane in place, preserving length
	if err := UpdateThreeportConfig(cfg, updated); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.ControlPlanes) != 1 {
		t.Fatalf("expected len 1, got %d", len(cfg.ControlPlanes))
	}
	if cfg.ControlPlanes[0].Provider != "eks" {
		t.Fatalf("expected replacement to update Provider: %+v", cfg.ControlPlanes[0])
	}
}

// TestDeleteThreeportConfigControlPlane covers removal of the matching entry
// and preservation of the others.
func TestDeleteThreeportConfigControlPlane(t *testing.T) {
	setupViperFile(t)

	cfg := &ThreeportConfig{
		ControlPlanes: []ControlPlane{
			{Name: "keep"},
			{Name: "drop"},
			{Name: "keep2"},
		},
	}

	// delete the middle entry
	DeleteThreeportConfigControlPlane(cfg, "drop")

	// verify only the remaining control planes end up in viper
	var remaining []ControlPlane
	if err := viper.UnmarshalKey("ControlPlanes", &remaining); err != nil {
		t.Fatalf("failed to unmarshal ControlPlanes: %v", err)
	}
	if len(remaining) != 2 {
		t.Fatalf("expected 2 remaining, got %d: %+v", len(remaining), remaining)
	}
	for _, cp := range remaining {
		if cp.Name == "drop" {
			t.Fatalf("expected drop to be removed: %+v", remaining)
		}
	}
	// current control plane is cleared
	if got := viper.GetString("CurrentControlPlane"); got != "" {
		t.Fatalf("expected CurrentControlPlane cleared, got %q", got)
	}
}

// TestGetThreeportConfig covers unmarshalling viper into a ThreeportConfig
// and the requested-vs-current control plane precedence.
func TestGetThreeportConfig(t *testing.T) {
	setupViperFile(t)
	viper.Set("CurrentControlPlane", "current")
	viper.Set("ControlPlanes", []map[string]any{
		{"Name": "current"},
		{"Name": "other"},
	})

	// no override: unmarshals and returns CurrentControlPlane
	cfg, name, err := GetThreeportConfig("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "current" {
		t.Fatalf("expected name from viper, got %q", name)
	}
	if len(cfg.ControlPlanes) != 2 {
		t.Fatalf("expected 2 control planes, got %d", len(cfg.ControlPlanes))
	}

	// explicit request overrides the current control plane
	_, name, err = GetThreeportConfig("other")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "other" {
		t.Fatalf("expected explicit override, got %q", name)
	}
}

// TestUpdateThreeportConfigInstance asserts the mutator callback runs and the
// updated control plane is persisted through the shared viper state.
func TestUpdateThreeportConfigInstance(t *testing.T) {
	setupViperFile(t)

	// prime viper with an existing control plane so GetThreeportConfig has
	// something to unmarshal
	viper.Set("CurrentControlPlane", "primary")
	viper.Set("ControlPlanes", []map[string]any{{"Name": "primary", "Provider": "kind"}})

	cp := &ControlPlane{Name: "primary", Provider: "kind"}

	// mutate via callback
	updated, err := cp.UpdateThreeportConfigInstance(func(c *ControlPlane) {
		c.Provider = "eks"
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// callback ran on the receiver
	if cp.Provider != "eks" {
		t.Fatalf("callback did not mutate receiver: %+v", cp)
	}
	// returned config carries a viper-loaded slice with the updated entry
	if updated == nil {
		t.Fatal("expected non-nil updated config")
	}
	// viper reflects the new provider in its stored ControlPlanes list
	var stored []ControlPlane
	if err := viper.UnmarshalKey("ControlPlanes", &stored); err != nil {
		t.Fatalf("failed to unmarshal ControlPlanes: %v", err)
	}
	found := false
	for _, s := range stored {
		if s.Name == "primary" && s.Provider == "eks" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected stored control planes to reflect update: %+v", stored)
	}
}
