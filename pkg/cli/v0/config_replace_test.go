package v0

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// TestReplaceThreeportConfigRenamesTheCurrentEntry asserts an upgrade keeps one
// config entry by writing the new name over the current entry.
func TestReplaceThreeportConfigRenamesTheCurrentEntry(t *testing.T) {
	// start from a current entry and a stale entry that already has the new name
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	viper.SetConfigFile(path)
	viper.SetConfigType("yaml")
	if err := viper.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	threeportConfig := &ThreeportConfig{
		ControlPlanes: []ControlPlane{
			{Name: "genesis-04"},
			{Name: "genesis-06", APIServer: "https://stale"},
			{Name: "genesis-05", APIServer: "https://old"},
		},
	}

	// rename the current entry and drop the stale copy of the new name
	err := replaceThreeportConfig(threeportConfig, "genesis-05", &ControlPlane{
		Name:      "genesis-06",
		APIServer: "https://new",
	})
	if err != nil {
		t.Fatal(err)
	}

	// the file has the renamed entry and the unrelated one
	if len(threeportConfig.ControlPlanes) != 2 {
		t.Fatalf("entries %d", len(threeportConfig.ControlPlanes))
	}
	if threeportConfig.ControlPlanes[0].Name != "genesis-04" {
		t.Fatalf("first %s", threeportConfig.ControlPlanes[0].Name)
	}
	if threeportConfig.ControlPlanes[1].Name != "genesis-06" || threeportConfig.ControlPlanes[1].APIServer != "https://new" {
		t.Fatalf("renamed %+v", threeportConfig.ControlPlanes[1])
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var written struct {
		CurrentControlPlane string `yaml:"currentcontrolplane"`
	}
	if err := yaml.Unmarshal(saved, &written); err != nil {
		t.Fatal(err)
	}
	if written.CurrentControlPlane != "genesis-06" {
		t.Fatalf("current %s", written.CurrentControlPlane)
	}
}
