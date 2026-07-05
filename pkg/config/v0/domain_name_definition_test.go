package v0

import (
	"strings"
	"testing"

	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestDomainNameDefinitionConfigValidateAcceptsAllFieldsSet covers the happy-path
// branch where every required field is present.
func TestDomainNameDefinitionConfigValidateAcceptsAllFieldsSet(t *testing.T) {
	// build a config with all required fields populated
	cfg := &DomainNameDefinitionConfig{
		DomainNameDefinition: DomainNameDefinitionValues{
			Name:       util.Ptr("example"),
			Domain:     util.Ptr("example.com"),
			Zone:       util.Ptr("example.com."),
			AdminEmail: util.Ptr("admin@example.com"),
		},
	}

	// invoke validate on the fully-populated config
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config to pass, got: %v", err)
	}
}

// TestDomainNameDefinitionConfigValidateRejectsMissingName covers the branch
// that flags a nil Name field.
func TestDomainNameDefinitionConfigValidateRejectsMissingName(t *testing.T) {
	// build a config missing only the Name field
	cfg := &DomainNameDefinitionConfig{
		DomainNameDefinition: DomainNameDefinitionValues{
			Domain:     util.Ptr("example.com"),
			Zone:       util.Ptr("example.com."),
			AdminEmail: util.Ptr("admin@example.com"),
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

// TestDomainNameDefinitionConfigValidateRejectsMissingDomain covers the branch
// that flags a nil Domain field.
func TestDomainNameDefinitionConfigValidateRejectsMissingDomain(t *testing.T) {
	// build a config missing only the Domain field
	cfg := &DomainNameDefinitionConfig{
		DomainNameDefinition: DomainNameDefinitionValues{
			Name:       util.Ptr("example"),
			Zone:       util.Ptr("example.com."),
			AdminEmail: util.Ptr("admin@example.com"),
		},
	}

	// invoke validate and verify the missing-domain error surfaces
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected error for missing domain, got nil")
	}
	if !strings.Contains(err.Error(), "Domain") {
		t.Errorf("expected error to mention Domain, got: %v", err)
	}
}

// TestDomainNameDefinitionConfigValidateRejectsMissingZone covers the branch
// that flags a nil Zone field.
func TestDomainNameDefinitionConfigValidateRejectsMissingZone(t *testing.T) {
	// build a config missing only the Zone field
	cfg := &DomainNameDefinitionConfig{
		DomainNameDefinition: DomainNameDefinitionValues{
			Name:       util.Ptr("example"),
			Domain:     util.Ptr("example.com"),
			AdminEmail: util.Ptr("admin@example.com"),
		},
	}

	// invoke validate and verify the missing-zone error surfaces
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected error for missing zone, got nil")
	}
	if !strings.Contains(err.Error(), "Zone") {
		t.Errorf("expected error to mention Zone, got: %v", err)
	}
}

// TestDomainNameDefinitionConfigValidateRejectsMissingAdminEmail covers the branch
// that flags a nil AdminEmail field.
func TestDomainNameDefinitionConfigValidateRejectsMissingAdminEmail(t *testing.T) {
	// build a config missing only the AdminEmail field
	cfg := &DomainNameDefinitionConfig{
		DomainNameDefinition: DomainNameDefinitionValues{
			Name:   util.Ptr("example"),
			Domain: util.Ptr("example.com"),
			Zone:   util.Ptr("example.com."),
		},
	}

	// invoke validate and verify the missing-admin-email error surfaces
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected error for missing admin email, got nil")
	}
	if !strings.Contains(err.Error(), "AdminEmail") {
		t.Errorf("expected error to mention AdminEmail, got: %v", err)
	}
}

// TestDomainNameDefinitionConfigValidateAccumulatesErrors covers the multi-error
// path where every required field is missing at once.
func TestDomainNameDefinitionConfigValidateAccumulatesErrors(t *testing.T) {
	// build a config with no fields set so every required check fails
	cfg := &DomainNameDefinitionConfig{}

	// invoke validate and verify each field surfaces in the joined message
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected error for empty config, got nil")
	}
	msg := err.Error()
	for _, field := range []string{"Name", "Domain", "Zone", "AdminEmail"} {
		if !strings.Contains(msg, field) {
			t.Errorf("expected error to mention %s, got: %v", field, err)
		}
	}
}
