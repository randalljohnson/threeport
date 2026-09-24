package v0

import (
	"strings"
	"testing"

	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestAwsEksKubernetesRuntimeDefinitionConfigValidateAcceptsAllFieldsSet covers
// the happy-path branch where every required field is present.
func TestAwsEksKubernetesRuntimeDefinitionConfigValidateAcceptsAllFieldsSet(t *testing.T) {
	// build a config with all required fields populated
	cfg := &AwsEksKubernetesRuntimeDefinitionConfig{
		AwsEksKubernetesRuntimeDefinition: AwsEksKubernetesRuntimeDefinitionValues{
			Name:                         util.Ptr("eks-def"),
			ZoneCount:                    util.Ptr(2),
			DefaultNodeGroupInstanceType: util.Ptr("t3.medium"),
			DefaultNodeGroupInitialSize:  util.Ptr(2),
			DefaultNodeGroupMinimumSize:  util.Ptr(1),
			DefaultNodeGroupMaximumSize:  util.Ptr(5),
		},
	}

	// invoke validate on the fully-populated config
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config to pass, got: %v", err)
	}
}

// TestAwsEksKubernetesRuntimeDefinitionConfigValidateRejectsMissingName covers
// the branch that flags a nil Name field.
func TestAwsEksKubernetesRuntimeDefinitionConfigValidateRejectsMissingName(t *testing.T) {
	// build a config missing only the Name field
	cfg := &AwsEksKubernetesRuntimeDefinitionConfig{
		AwsEksKubernetesRuntimeDefinition: AwsEksKubernetesRuntimeDefinitionValues{
			ZoneCount:                    util.Ptr(2),
			DefaultNodeGroupInstanceType: util.Ptr("t3.medium"),
			DefaultNodeGroupInitialSize:  util.Ptr(2),
			DefaultNodeGroupMinimumSize:  util.Ptr(1),
			DefaultNodeGroupMaximumSize:  util.Ptr(5),
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

// TestAwsEksKubernetesRuntimeDefinitionConfigValidateRejectsMissingZoneCount
// covers the branch that flags a nil ZoneCount field.
func TestAwsEksKubernetesRuntimeDefinitionConfigValidateRejectsMissingZoneCount(t *testing.T) {
	// build a config missing only the ZoneCount field
	cfg := &AwsEksKubernetesRuntimeDefinitionConfig{
		AwsEksKubernetesRuntimeDefinition: AwsEksKubernetesRuntimeDefinitionValues{
			Name:                         util.Ptr("eks-def"),
			DefaultNodeGroupInstanceType: util.Ptr("t3.medium"),
			DefaultNodeGroupInitialSize:  util.Ptr(2),
			DefaultNodeGroupMinimumSize:  util.Ptr(1),
			DefaultNodeGroupMaximumSize:  util.Ptr(5),
		},
	}

	// invoke validate and verify the missing-zone-count error surfaces
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected error for missing zone count, got nil")
	}
	if !strings.Contains(err.Error(), "ZoneCount") {
		t.Errorf("expected error to mention ZoneCount, got: %v", err)
	}
}

// TestAwsEksKubernetesRuntimeDefinitionConfigValidateRejectsMissingInstanceType
// covers the branch that flags a nil DefaultNodeGroupInstanceType field.
func TestAwsEksKubernetesRuntimeDefinitionConfigValidateRejectsMissingInstanceType(t *testing.T) {
	// build a config missing only the DefaultNodeGroupInstanceType field
	cfg := &AwsEksKubernetesRuntimeDefinitionConfig{
		AwsEksKubernetesRuntimeDefinition: AwsEksKubernetesRuntimeDefinitionValues{
			Name:                        util.Ptr("eks-def"),
			ZoneCount:                   util.Ptr(2),
			DefaultNodeGroupInitialSize: util.Ptr(2),
			DefaultNodeGroupMinimumSize: util.Ptr(1),
			DefaultNodeGroupMaximumSize: util.Ptr(5),
		},
	}

	// invoke validate and verify the missing-instance-type error surfaces
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected error for missing instance type, got nil")
	}
	if !strings.Contains(err.Error(), "DefaultNodeGroupInstanceType") {
		t.Errorf("expected error to mention DefaultNodeGroupInstanceType, got: %v", err)
	}
}

// TestAwsEksKubernetesRuntimeDefinitionConfigValidateRejectsMissingInitialSize
// covers the branch that flags a nil DefaultNodeGroupInitialSize field.
func TestAwsEksKubernetesRuntimeDefinitionConfigValidateRejectsMissingInitialSize(t *testing.T) {
	// build a config missing only the DefaultNodeGroupInitialSize field
	cfg := &AwsEksKubernetesRuntimeDefinitionConfig{
		AwsEksKubernetesRuntimeDefinition: AwsEksKubernetesRuntimeDefinitionValues{
			Name:                         util.Ptr("eks-def"),
			ZoneCount:                    util.Ptr(2),
			DefaultNodeGroupInstanceType: util.Ptr("t3.medium"),
			DefaultNodeGroupMinimumSize:  util.Ptr(1),
			DefaultNodeGroupMaximumSize:  util.Ptr(5),
		},
	}

	// invoke validate and verify the missing-initial-size error surfaces
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected error for missing initial size, got nil")
	}
	if !strings.Contains(err.Error(), "DefaultNodeGroupInitialSize") {
		t.Errorf("expected error to mention DefaultNodeGroupInitialSize, got: %v", err)
	}
}

// TestAwsEksKubernetesRuntimeDefinitionConfigValidateRejectsMissingMinimumSize
// covers the branch that flags a nil DefaultNodeGroupMinimumSize field.
func TestAwsEksKubernetesRuntimeDefinitionConfigValidateRejectsMissingMinimumSize(t *testing.T) {
	// build a config missing only the DefaultNodeGroupMinimumSize field
	cfg := &AwsEksKubernetesRuntimeDefinitionConfig{
		AwsEksKubernetesRuntimeDefinition: AwsEksKubernetesRuntimeDefinitionValues{
			Name:                         util.Ptr("eks-def"),
			ZoneCount:                    util.Ptr(2),
			DefaultNodeGroupInstanceType: util.Ptr("t3.medium"),
			DefaultNodeGroupInitialSize:  util.Ptr(2),
			DefaultNodeGroupMaximumSize:  util.Ptr(5),
		},
	}

	// invoke validate and verify the missing-minimum-size error surfaces
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected error for missing minimum size, got nil")
	}
	if !strings.Contains(err.Error(), "DefaultNodeGroupMinimumSize") {
		t.Errorf("expected error to mention DefaultNodeGroupMinimumSize, got: %v", err)
	}
}

// TestAwsEksKubernetesRuntimeDefinitionConfigValidateRejectsMissingMaximumSize
// covers the branch that flags a nil DefaultNodeGroupMaximumSize field.
func TestAwsEksKubernetesRuntimeDefinitionConfigValidateRejectsMissingMaximumSize(t *testing.T) {
	// build a config missing only the DefaultNodeGroupMaximumSize field
	cfg := &AwsEksKubernetesRuntimeDefinitionConfig{
		AwsEksKubernetesRuntimeDefinition: AwsEksKubernetesRuntimeDefinitionValues{
			Name:                         util.Ptr("eks-def"),
			ZoneCount:                    util.Ptr(2),
			DefaultNodeGroupInstanceType: util.Ptr("t3.medium"),
			DefaultNodeGroupInitialSize:  util.Ptr(2),
			DefaultNodeGroupMinimumSize:  util.Ptr(1),
		},
	}

	// invoke validate and verify the missing-maximum-size error surfaces
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected error for missing maximum size, got nil")
	}
	if !strings.Contains(err.Error(), "DefaultNodeGroupMaximumSize") {
		t.Errorf("expected error to mention DefaultNodeGroupMaximumSize, got: %v", err)
	}
}

// TestAwsEksKubernetesRuntimeDefinitionConfigValidateAccumulatesErrors covers
// the multi-error path where every required field is missing at once.
func TestAwsEksKubernetesRuntimeDefinitionConfigValidateAccumulatesErrors(t *testing.T) {
	// build a config with no fields set so every required check fails
	cfg := &AwsEksKubernetesRuntimeDefinitionConfig{}

	// invoke validate and verify each field surfaces in the joined message
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected error for empty config, got nil")
	}
	msg := err.Error()
	fields := []string{
		"Name",
		"ZoneCount",
		"DefaultNodeGroupInstanceType",
		"DefaultNodeGroupInitialSize",
		"DefaultNodeGroupMinimumSize",
		"DefaultNodeGroupMaximumSize",
	}
	for _, field := range fields {
		if !strings.Contains(msg, field) {
			t.Errorf("expected error to mention %s, got: %v", field, err)
		}
	}
}
