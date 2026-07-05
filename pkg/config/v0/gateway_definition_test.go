package v0

import (
	"strings"
	"testing"

	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestGatewayDefinitionConfigValidateAcceptsMinimal covers the happy-path branch
// where Name is set and at least one port list is present.
func TestGatewayDefinitionConfigValidateAcceptsMinimal(t *testing.T) {
	// build a config that satisfies both required-field checks: name set and
	// a non-nil (empty is fine) http port slice
	cfg := &GatewayDefinitionConfig{
		GatewayDefinition: GatewayDefinitionValues{
			Name:      util.Ptr("gw"),
			HttpPorts: &[]GatewayHttpPortValues{},
		},
	}

	// invoke validate on the minimally-valid config
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config to pass, got: %v", err)
	}
}

// TestGatewayDefinitionConfigValidateRejectsMissingName covers the branch where
// Name is nil.
func TestGatewayDefinitionConfigValidateRejectsMissingName(t *testing.T) {
	// build a config missing the required name field
	cfg := &GatewayDefinitionConfig{
		GatewayDefinition: GatewayDefinitionValues{
			HttpPorts: &[]GatewayHttpPortValues{},
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

// TestGatewayDefinitionConfigValidateRejectsMissingPorts covers the branch where
// both HttpPorts and TcpPorts are nil.
func TestGatewayDefinitionConfigValidateRejectsMissingPorts(t *testing.T) {
	// build a config missing both port slices
	cfg := &GatewayDefinitionConfig{
		GatewayDefinition: GatewayDefinitionValues{
			Name: util.Ptr("gw"),
		},
	}

	// invoke validate and verify the missing-ports error surfaces
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected error for missing ports, got nil")
	}
	if !strings.Contains(err.Error(), "HttpPorts") {
		t.Errorf("expected error to mention HttpPorts, got: %v", err)
	}
}

// TestGatewayDefinitionConfigValidateAccumulatesErrors covers the multi-error path
// where both name and ports are missing at the same time.
func TestGatewayDefinitionConfigValidateAccumulatesErrors(t *testing.T) {
	// build a config that fails both required-field checks
	cfg := &GatewayDefinitionConfig{}

	// invoke validate and verify both errors are present in the joined message
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected error for empty config, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "Name") {
		t.Errorf("expected error to mention Name, got: %v", err)
	}
	if !strings.Contains(msg, "HttpPorts") {
		t.Errorf("expected error to mention HttpPorts, got: %v", err)
	}
}

// TestGatewayDefinitionConfigValidateAcceptsTcpPortsOnly covers the branch where
// only TcpPorts is set and HttpPorts is nil.
func TestGatewayDefinitionConfigValidateAcceptsTcpPortsOnly(t *testing.T) {
	// build a config with tcp-only ports
	cfg := &GatewayDefinitionConfig{
		GatewayDefinition: GatewayDefinitionValues{
			Name:     util.Ptr("gw"),
			TcpPorts: &[]GatewayTcpPortValues{},
		},
	}

	// invoke validate on the tcp-only config
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid tcp-only config to pass, got: %v", err)
	}
}

// TestGatewayDefinitionConfigValidateRejectsRedirectWithoutPort443 covers the
// branch that forbids an HTTPS redirect from port 80 without port 443 configured.
func TestGatewayDefinitionConfigValidateRejectsRedirectWithoutPort443(t *testing.T) {
	// build a config that enables http->https redirect without a port 443 entry
	cfg := &GatewayDefinitionConfig{
		GatewayDefinition: GatewayDefinitionValues{
			Name: util.Ptr("gw"),
			HttpPorts: &[]GatewayHttpPortValues{
				{
					Port:          util.Ptr(80),
					HTTPSRedirect: util.Ptr(true),
				},
			},
		},
	}

	// invoke validate and verify the redirect-without-443 error surfaces
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected error for redirect without 443, got nil")
	}
	if !strings.Contains(err.Error(), "443") {
		t.Errorf("expected error to mention 443, got: %v", err)
	}
}

// TestGatewayDefinitionConfigValidateAcceptsRedirectWithPort443 covers the
// happy path where a redirect from 80 is paired with an explicit 443 entry.
func TestGatewayDefinitionConfigValidateAcceptsRedirectWithPort443(t *testing.T) {
	// build a config with a redirect from 80 and port 443 present
	cfg := &GatewayDefinitionConfig{
		GatewayDefinition: GatewayDefinitionValues{
			Name: util.Ptr("gw"),
			HttpPorts: &[]GatewayHttpPortValues{
				{
					Port:          util.Ptr(80),
					HTTPSRedirect: util.Ptr(true),
				},
				{
					Port:          util.Ptr(443),
					HTTPSRedirect: util.Ptr(false),
				},
			},
		},
	}

	// invoke validate and verify the redirect-with-443 pairing passes
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected redirect with 443 to pass, got: %v", err)
	}
}

// TestGatewayHttpPortValidateDefaultsPath covers the branch where Path is nil
// or empty and Validate sets it to "/".
func TestGatewayHttpPortValidateDefaultsPath(t *testing.T) {
	cases := []struct {
		name string
		port GatewayHttpPortValues
	}{
		{
			name: "nil path",
			port: GatewayHttpPortValues{
				Port: util.Ptr(80),
			},
		},
		{
			name: "empty path",
			port: GatewayHttpPortValues{
				Port: util.Ptr(80),
				Path: util.Ptr(""),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke validate on the port; expect no error and a defaulted path
			p := tc.port
			if err := p.Validate(); err != nil {
				t.Fatalf("expected valid port to pass, got: %v", err)
			}
			if p.Path == nil || *p.Path != "/" {
				t.Errorf("expected path defaulted to \"/\", got: %v", p.Path)
			}
		})
	}
}

// TestGatewayHttpPortValidatePreservesPath covers the branch where Path is
// explicitly set and must be preserved.
func TestGatewayHttpPortValidatePreservesPath(t *testing.T) {
	// build a port with an explicit non-empty path
	p := GatewayHttpPortValues{
		Port: util.Ptr(80),
		Path: util.Ptr("/api"),
	}

	// invoke validate; expect no error and the original path retained
	if err := p.Validate(); err != nil {
		t.Fatalf("expected valid port to pass, got: %v", err)
	}
	if p.Path == nil || *p.Path != "/api" {
		t.Errorf("expected path preserved as /api, got: %v", p.Path)
	}
}

// TestGatewayHttpPortValidateRejectsTLSAndRedirect covers the branch that forbids
// setting TLSEnabled and HTTPSRedirect to true simultaneously.
func TestGatewayHttpPortValidateRejectsTLSAndRedirect(t *testing.T) {
	// build a port that turns on both TLS and the redirect flag
	p := GatewayHttpPortValues{
		Port:          util.Ptr(443),
		TLSEnabled:    util.Ptr(true),
		HTTPSRedirect: util.Ptr(true),
	}

	// invoke validate and verify the mutual-exclusion error surfaces
	err := p.Validate()
	if err == nil {
		t.Fatalf("expected error for TLS+redirect, got nil")
	}
	if !strings.Contains(err.Error(), "TLSEnabled") {
		t.Errorf("expected error to mention TLSEnabled, got: %v", err)
	}
}

// TestGatewayHttpPortValidateRejectsMissingPort covers the branches where Port
// is nil or zero.
func TestGatewayHttpPortValidateRejectsMissingPort(t *testing.T) {
	cases := []struct {
		name string
		port GatewayHttpPortValues
	}{
		{
			name: "nil port",
			port: GatewayHttpPortValues{},
		},
		{
			name: "zero port",
			port: GatewayHttpPortValues{
				Port: util.Ptr(0),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke validate; expect the missing-port error to surface
			p := tc.port
			err := p.Validate()
			if err == nil {
				t.Fatalf("expected error for missing port, got nil")
			}
			if !strings.Contains(err.Error(), "Port") {
				t.Errorf("expected error to mention Port, got: %v", err)
			}
		})
	}
}
