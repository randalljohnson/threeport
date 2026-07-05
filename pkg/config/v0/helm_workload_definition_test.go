package v0

import (
	"strings"
	"testing"

	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestHelmWorkloadDefinitionConfigValidateAcceptsAllRequired covers the
// happy-path branch where every required field is populated.
func TestHelmWorkloadDefinitionConfigValidateAcceptsAllRequired(t *testing.T) {
	// build a config with all required fields populated
	cfg := &HelmWorkloadDefinitionConfig{
		HelmWorkloadDefinition: HelmWorkloadDefinitionValues{
			Name:  util.Ptr("hw-def"),
			Repo:  util.Ptr("https://charts.example"),
			Chart: util.Ptr("nginx"),
		},
	}

	// invoke validate on the fully-populated config
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config to pass, got: %v", err)
	}
}

// TestHelmWorkloadDefinitionConfigValidateRejectsMissingName covers the branch
// that flags a nil Name field.
func TestHelmWorkloadDefinitionConfigValidateRejectsMissingName(t *testing.T) {
	// build a config missing only the Name field
	cfg := &HelmWorkloadDefinitionConfig{
		HelmWorkloadDefinition: HelmWorkloadDefinitionValues{
			Repo:  util.Ptr("https://charts.example"),
			Chart: util.Ptr("nginx"),
		},
	}

	// invoke validate and verify the missing-name error surfaces
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected error for missing name, got nil")
	}
	if !strings.Contains(err.Error(), "Name") {
		t.Errorf("expected error to mention Name, got: %v", err)
	}
}

// TestHelmWorkloadDefinitionConfigValidateRejectsMissingRepo covers the branch
// that flags a nil Repo field.
func TestHelmWorkloadDefinitionConfigValidateRejectsMissingRepo(t *testing.T) {
	// build a config missing only the Repo field
	cfg := &HelmWorkloadDefinitionConfig{
		HelmWorkloadDefinition: HelmWorkloadDefinitionValues{
			Name:  util.Ptr("hw-def"),
			Chart: util.Ptr("nginx"),
		},
	}

	// invoke validate and verify the missing-repo error surfaces
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected error for missing repo, got nil")
	}
	if !strings.Contains(err.Error(), "Repo") {
		t.Errorf("expected error to mention Repo, got: %v", err)
	}
}

// TestHelmWorkloadDefinitionConfigValidateRejectsMissingChart covers the branch
// that flags a nil Chart field.
func TestHelmWorkloadDefinitionConfigValidateRejectsMissingChart(t *testing.T) {
	// build a config missing only the Chart field
	cfg := &HelmWorkloadDefinitionConfig{
		HelmWorkloadDefinition: HelmWorkloadDefinitionValues{
			Name: util.Ptr("hw-def"),
			Repo: util.Ptr("https://charts.example"),
		},
	}

	// invoke validate and verify the missing-chart error surfaces
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected error for missing chart, got nil")
	}
	if !strings.Contains(err.Error(), "Chart") {
		t.Errorf("expected error to mention Chart, got: %v", err)
	}
}

// TestHelmWorkloadDefinitionConfigValidateRejectsValuesAndValuesDocument covers
// the branch that forbids setting Values and ValuesDocument simultaneously.
func TestHelmWorkloadDefinitionConfigValidateRejectsValuesAndValuesDocument(t *testing.T) {
	// build a config that populates both mutually-exclusive value inputs
	cfg := &HelmWorkloadDefinitionConfig{
		HelmWorkloadDefinition: HelmWorkloadDefinitionValues{
			Name:           util.Ptr("hw-def"),
			Repo:           util.Ptr("https://charts.example"),
			Chart:          util.Ptr("nginx"),
			Values:         util.Ptr("key: value"),
			ValuesDocument: util.Ptr("/tmp/values.yaml"),
		},
	}

	// invoke validate and verify the mutual-exclusion error surfaces
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected error for Values+ValuesDocument, got nil")
	}
	if !strings.Contains(err.Error(), "Values") {
		t.Errorf("expected error to mention Values, got: %v", err)
	}
}

// TestHelmWorkloadDefinitionConfigValidateAcceptsValuesOnly covers the branch
// where only Values is set.
func TestHelmWorkloadDefinitionConfigValidateAcceptsValuesOnly(t *testing.T) {
	// build a config with the inline Values field set
	cfg := &HelmWorkloadDefinitionConfig{
		HelmWorkloadDefinition: HelmWorkloadDefinitionValues{
			Name:   util.Ptr("hw-def"),
			Repo:   util.Ptr("https://charts.example"),
			Chart:  util.Ptr("nginx"),
			Values: util.Ptr("key: value"),
		},
	}

	// invoke validate on the values-only config
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid values-only config to pass, got: %v", err)
	}
}

// TestHelmWorkloadDefinitionConfigValidateAcceptsValuesDocumentOnly covers the
// branch where only ValuesDocument is set.
func TestHelmWorkloadDefinitionConfigValidateAcceptsValuesDocumentOnly(t *testing.T) {
	// build a config with the file-path ValuesDocument field set
	cfg := &HelmWorkloadDefinitionConfig{
		HelmWorkloadDefinition: HelmWorkloadDefinitionValues{
			Name:           util.Ptr("hw-def"),
			Repo:           util.Ptr("https://charts.example"),
			Chart:          util.Ptr("nginx"),
			ValuesDocument: util.Ptr("/tmp/values.yaml"),
		},
	}

	// invoke validate on the values-document-only config
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid values-document-only config to pass, got: %v", err)
	}
}

// TestHelmWorkloadDefinitionConfigValidateAccumulatesErrors covers the
// multi-error path where every required field is missing at once.
func TestHelmWorkloadDefinitionConfigValidateAccumulatesErrors(t *testing.T) {
	// build a config with none of the required fields populated
	cfg := &HelmWorkloadDefinitionConfig{}

	// invoke validate and verify every missing-field error surfaces at once
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected error for empty config, got nil")
	}
	msg := err.Error()
	for _, field := range []string{"Name", "Repo", "Chart"} {
		if !strings.Contains(msg, field) {
			t.Errorf("expected error to mention %s, got: %v", field, err)
		}
	}
}
