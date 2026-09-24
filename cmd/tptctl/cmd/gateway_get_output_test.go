package cmd

import (
	"strings"
	"testing"

	config_v0 "github.com/threeport/threeport/pkg/config/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestOutputGetv0DomainNamesCmdRendersRows covers header emission,
// populated fields, and the nil-optional branches for domain names.
func TestOutputGetv0DomainNamesCmdRendersRows(t *testing.T) {
	cases := []struct {
		name        string
		domainNames []config_v0.DomainNameConfig
		wants       []string
		notWants    []string
	}{
		{
			name:        "empty slice renders header only",
			domainNames: []config_v0.DomainNameConfig{},
			wants:       []string{"NAME", "DOMAIN NAME DEFINITION", "DOMAIN", "ZONE", "ADMIN EMAIL", "WORKLOAD INSTANCE", "AGE"},
			notWants:    []string{"dn-a"},
		},
		{
			name: "populated row with workload instance and age",
			domainNames: []config_v0.DomainNameConfig{
				{DomainName: config_v0.DomainNameValues{
					Name:       util.Ptr("dn-a"),
					Domain:     util.Ptr("example.io"),
					Zone:       util.Ptr("zone-a"),
					AdminEmail: util.Ptr("admin@example.io"),
					KubernetesWorkloadInstance: &config_v0.KubernetesWorkloadInstanceValues{
						Name: util.Ptr("wl-a"),
					},
					Age: util.Ptr("3h"),
				}},
			},
			wants: []string{"dn-a", "example.io", "zone-a", "admin@example.io", "wl-a", "3h"},
		},
		{
			name: "nil workload instance and nil age render as empty",
			domainNames: []config_v0.DomainNameConfig{
				{DomainName: config_v0.DomainNameValues{
					Name:                       util.Ptr("dn-b"),
					Domain:                     util.Ptr("example.net"),
					Zone:                       util.Ptr("zone-b"),
					AdminEmail:                 util.Ptr("admin@example.net"),
					KubernetesWorkloadInstance: nil,
					Age:                        nil,
				}},
			},
			wants:    []string{"dn-b", "example.net", "zone-b", "admin@example.net"},
			notWants: []string{"<nil>"},
		},
		{
			name: "workload instance with nil name falls back to empty",
			domainNames: []config_v0.DomainNameConfig{
				{DomainName: config_v0.DomainNameValues{
					Name:                       util.Ptr("dn-c"),
					Domain:                     util.Ptr("example.org"),
					Zone:                       util.Ptr("zone-c"),
					AdminEmail:                 util.Ptr("admin@example.org"),
					KubernetesWorkloadInstance: &config_v0.KubernetesWorkloadInstanceValues{Name: nil},
					Age:                        util.Ptr("1d"),
				}},
			},
			wants:    []string{"dn-c", "example.org", "1d"},
			notWants: []string{"<nil>"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke helper and capture stdout
			domainNames := tc.domainNames
			out, err := captureStdout(t, func() error {
				return outputGetv0DomainNamesCmd(&domainNames)
			})
			// verify the helper's nil-error contract
			if err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			// verify each expected substring is present
			for _, want := range tc.wants {
				if !strings.Contains(out, want) {
					t.Errorf("expected output to contain %q; got:\n%s", want, out)
				}
			}
			// verify absent substrings really are absent
			for _, nw := range tc.notWants {
				if strings.Contains(out, nw) {
					t.Errorf("expected output NOT to contain %q; got:\n%s", nw, out)
				}
			}
		})
	}
}

// TestOutputGetv0DomainNameDefinitionsCmdRendersRows covers header emission,
// populated rows, and the nil-age fallback for domain name definitions.
func TestOutputGetv0DomainNameDefinitionsCmdRendersRows(t *testing.T) {
	cases := []struct {
		name    string
		configs []config_v0.DomainNameDefinitionConfig
		wants   []string
	}{
		{
			name:    "empty slice renders header only",
			configs: []config_v0.DomainNameDefinitionConfig{},
			wants:   []string{"NAME", "DOMAIN", "ZONE", "ADMIN EMAIL", "AGE"},
		},
		{
			name: "populated row with age",
			configs: []config_v0.DomainNameDefinitionConfig{
				{DomainNameDefinition: config_v0.DomainNameDefinitionValues{
					Name:       util.Ptr("dnd-a"),
					Domain:     util.Ptr("example.io"),
					Zone:       util.Ptr("zone-a"),
					AdminEmail: util.Ptr("admin@example.io"),
					Age:        util.Ptr("5m"),
				}},
			},
			wants: []string{"dnd-a", "example.io", "zone-a", "admin@example.io", "5m"},
		},
		{
			name: "nil age renders empty and does not panic",
			configs: []config_v0.DomainNameDefinitionConfig{
				{DomainNameDefinition: config_v0.DomainNameDefinitionValues{
					Name:       util.Ptr("dnd-b"),
					Domain:     util.Ptr("example.net"),
					Zone:       util.Ptr("zone-b"),
					AdminEmail: util.Ptr("admin@example.net"),
					Age:        nil,
				}},
			},
			wants: []string{"dnd-b", "example.net"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke helper and capture stdout
			configs := tc.configs
			out, err := captureStdout(t, func() error {
				return outputGetv0DomainNameDefinitionsCmd(&configs)
			})
			// verify the helper's nil-error contract
			if err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			// verify expected substrings appear
			for _, want := range tc.wants {
				if !strings.Contains(out, want) {
					t.Errorf("expected output to contain %q; got:\n%s", want, out)
				}
			}
			// verify no literal <nil> from format-nil-pointer bugs
			if strings.Contains(out, "<nil>") {
				t.Errorf("output must not contain <nil>; got:\n%s", out)
			}
		})
	}
}

// TestOutputGetv0DomainNameInstancesCmdRendersRows covers the header,
// populated relationships, and every nil-relationship branch.
func TestOutputGetv0DomainNameInstancesCmdRendersRows(t *testing.T) {
	cases := []struct {
		name     string
		configs  []config_v0.DomainNameInstanceConfig
		wants    []string
		notWants []string
	}{
		{
			name:    "empty slice renders header only",
			configs: []config_v0.DomainNameInstanceConfig{},
			wants:   []string{"NAME", "DOMAIN NAME DEFINITION", "KUBERNETES RUNTIME INSTANCE", "WORKLOAD INSTANCE", "AGE"},
		},
		{
			name: "populated row with all relations",
			configs: []config_v0.DomainNameInstanceConfig{
				{DomainNameInstance: config_v0.DomainNameInstanceValues{
					Name:                       util.Ptr("dni-a"),
					DomainNameDefinition:       &config_v0.DomainNameDefinitionValues{Name: util.Ptr("dnd-a")},
					KubernetesRuntimeInstance:  &config_v0.KubernetesRuntimeInstanceValues{Name: util.Ptr("kri-a")},
					KubernetesWorkloadInstance: &config_v0.KubernetesWorkloadInstanceValues{Name: util.Ptr("wl-a")},
					Age:                        util.Ptr("2h"),
				}},
			},
			wants: []string{"dni-a", "dnd-a", "kri-a", "wl-a", "2h"},
		},
		{
			name: "nil relations render empty and do not panic",
			configs: []config_v0.DomainNameInstanceConfig{
				{DomainNameInstance: config_v0.DomainNameInstanceValues{
					Name:                       util.Ptr("dni-b"),
					DomainNameDefinition:       nil,
					KubernetesRuntimeInstance:  nil,
					KubernetesWorkloadInstance: nil,
					Age:                        nil,
				}},
			},
			wants:    []string{"dni-b"},
			notWants: []string{"<nil>"},
		},
		{
			name: "relations present but name pointers nil fall back to empty",
			configs: []config_v0.DomainNameInstanceConfig{
				{DomainNameInstance: config_v0.DomainNameInstanceValues{
					Name:                       util.Ptr("dni-c"),
					DomainNameDefinition:       &config_v0.DomainNameDefinitionValues{Name: nil},
					KubernetesRuntimeInstance:  &config_v0.KubernetesRuntimeInstanceValues{Name: nil},
					KubernetesWorkloadInstance: &config_v0.KubernetesWorkloadInstanceValues{Name: nil},
					Age:                        util.Ptr("30s"),
				}},
			},
			wants:    []string{"dni-c", "30s"},
			notWants: []string{"<nil>"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke helper and capture stdout
			configs := tc.configs
			out, err := captureStdout(t, func() error {
				return outputGetv0DomainNameInstancesCmd(&configs)
			})
			// verify nil-error contract
			if err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			// verify expected substrings appear
			for _, want := range tc.wants {
				if !strings.Contains(out, want) {
					t.Errorf("expected output to contain %q; got:\n%s", want, out)
				}
			}
			// verify absent substrings really are absent
			for _, nw := range tc.notWants {
				if strings.Contains(out, nw) {
					t.Errorf("expected output NOT to contain %q; got:\n%s", nw, out)
				}
			}
		})
	}
}

// TestOutputGetv0GatewaysCmdRendersRows covers header emission, every
// pointer-guarded field, and the ports Sprintf formatting branch.
func TestOutputGetv0GatewaysCmdRendersRows(t *testing.T) {
	cases := []struct {
		name     string
		configs  []config_v0.GatewayConfig
		wants    []string
		notWants []string
	}{
		{
			name:    "empty slice renders header only",
			configs: []config_v0.GatewayConfig{},
			wants:   []string{"NAME", "GATEWAY DEFINITION", "GATEWAY INSTANCE", "HTTP PORTS", "TCP PORTS", "SUBDOMAIN", "KUBERNETES SERVICE NAME", "DOMAIN NAME DEFINITION", "KUBERNETES RUNTIME INSTANCE", "WORKLOAD INSTANCE", "AGE"},
		},
		{
			name: "fully populated row",
			configs: []config_v0.GatewayConfig{
				{Gateway: config_v0.GatewayValues{
					Name: util.Ptr("gw-a"),
					HttpPorts: &[]config_v0.GatewayHttpPortValues{
						{Port: util.Ptr(80), Path: util.Ptr("/")},
					},
					TcpPorts: &[]config_v0.GatewayTcpPortValues{
						{Port: util.Ptr(6379)},
					},
					SubDomain:                  util.Ptr("api"),
					ServiceName:                util.Ptr("svc-a"),
					DomainNameDefinition:       &config_v0.DomainNameDefinitionValues{Name: util.Ptr("dnd-a")},
					KubernetesRuntimeInstance:  &config_v0.KubernetesRuntimeInstanceValues{Name: util.Ptr("kri-a")},
					KubernetesWorkloadInstance: &config_v0.KubernetesWorkloadInstanceValues{Name: util.Ptr("wl-a")},
					Age:                        util.Ptr("1h"),
				}},
			},
			wants: []string{"gw-a", "api", "svc-a", "dnd-a", "kri-a", "wl-a", "1h"},
		},
		{
			name: "nil ports and relations render empty",
			configs: []config_v0.GatewayConfig{
				{Gateway: config_v0.GatewayValues{
					Name:                       util.Ptr("gw-b"),
					HttpPorts:                  nil,
					TcpPorts:                   nil,
					SubDomain:                  nil,
					ServiceName:                nil,
					DomainNameDefinition:       nil,
					KubernetesRuntimeInstance:  nil,
					KubernetesWorkloadInstance: nil,
					Age:                        nil,
				}},
			},
			wants:    []string{"gw-b"},
			notWants: []string{"<nil>"},
		},
		{
			name: "relations present but name pointers nil fall back to empty",
			configs: []config_v0.GatewayConfig{
				{Gateway: config_v0.GatewayValues{
					Name:                       util.Ptr("gw-c"),
					DomainNameDefinition:       &config_v0.DomainNameDefinitionValues{Name: nil},
					KubernetesRuntimeInstance:  &config_v0.KubernetesRuntimeInstanceValues{Name: nil},
					KubernetesWorkloadInstance: &config_v0.KubernetesWorkloadInstanceValues{Name: nil},
				}},
			},
			wants:    []string{"gw-c"},
			notWants: []string{"<nil>"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke helper and capture stdout
			configs := tc.configs
			out, err := captureStdout(t, func() error {
				return outputGetv0GatewaysCmd(&configs)
			})
			// verify nil-error contract
			if err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			// verify expected substrings appear
			for _, want := range tc.wants {
				if !strings.Contains(out, want) {
					t.Errorf("expected output to contain %q; got:\n%s", want, out)
				}
			}
			// verify absent substrings really are absent
			for _, nw := range tc.notWants {
				if strings.Contains(out, nw) {
					t.Errorf("expected output NOT to contain %q; got:\n%s", nw, out)
				}
			}
		})
	}
}

// TestOutputGetv0GatewayDefinitionsCmdRendersRows covers header emission,
// populated fields, and every optional-pointer branch for gateway
// definitions.
func TestOutputGetv0GatewayDefinitionsCmdRendersRows(t *testing.T) {
	cases := []struct {
		name     string
		configs  []config_v0.GatewayDefinitionConfig
		wants    []string
		notWants []string
	}{
		{
			name:    "empty slice renders header only",
			configs: []config_v0.GatewayDefinitionConfig{},
			wants:   []string{"NAME", "HTTP PORTS", "TCP PORTS", "SUBDOMAIN", "KUBERNETES SERVICE NAME", "DOMAIN NAME DEFINITION", "AGE"},
		},
		{
			name: "populated row with ports and definition",
			configs: []config_v0.GatewayDefinitionConfig{
				{GatewayDefinition: config_v0.GatewayDefinitionValues{
					Name: util.Ptr("gwd-a"),
					HttpPorts: &[]config_v0.GatewayHttpPortValues{
						{Port: util.Ptr(443)},
					},
					TcpPorts: &[]config_v0.GatewayTcpPortValues{
						{Port: util.Ptr(9000)},
					},
					SubDomain:            util.Ptr("api"),
					ServiceName:          util.Ptr("svc"),
					DomainNameDefinition: &config_v0.DomainNameDefinitionValues{Name: util.Ptr("dnd-a")},
					Age:                  util.Ptr("4h"),
				}},
			},
			wants: []string{"gwd-a", "api", "svc", "dnd-a", "4h"},
		},
		{
			name: "nil ports and relation render empty",
			configs: []config_v0.GatewayDefinitionConfig{
				{GatewayDefinition: config_v0.GatewayDefinitionValues{
					Name: util.Ptr("gwd-b"),
				}},
			},
			wants:    []string{"gwd-b"},
			notWants: []string{"<nil>"},
		},
		{
			name: "relation present but name pointer nil falls back to empty",
			configs: []config_v0.GatewayDefinitionConfig{
				{GatewayDefinition: config_v0.GatewayDefinitionValues{
					Name:                 util.Ptr("gwd-c"),
					DomainNameDefinition: &config_v0.DomainNameDefinitionValues{Name: nil},
					Age:                  util.Ptr("10s"),
				}},
			},
			wants:    []string{"gwd-c", "10s"},
			notWants: []string{"<nil>"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke helper and capture stdout
			configs := tc.configs
			out, err := captureStdout(t, func() error {
				return outputGetv0GatewayDefinitionsCmd(&configs)
			})
			// verify nil-error contract
			if err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			// verify expected substrings appear
			for _, want := range tc.wants {
				if !strings.Contains(out, want) {
					t.Errorf("expected output to contain %q; got:\n%s", want, out)
				}
			}
			// verify absent substrings really are absent
			for _, nw := range tc.notWants {
				if strings.Contains(out, nw) {
					t.Errorf("expected output NOT to contain %q; got:\n%s", nw, out)
				}
			}
		})
	}
}

// TestOutputGetv0GatewayInstancesCmdRendersRows covers header, populated
// relations, and every nil-relation branch for gateway instances.
func TestOutputGetv0GatewayInstancesCmdRendersRows(t *testing.T) {
	cases := []struct {
		name     string
		configs  []config_v0.GatewayInstanceConfig
		wants    []string
		notWants []string
	}{
		{
			name:    "empty slice renders header only",
			configs: []config_v0.GatewayInstanceConfig{},
			wants:   []string{"NAME", "GATEWAY DEFINITION", "KUBERNETES RUNTIME INSTANCE", "WORKLOAD INSTANCE", "AGE"},
		},
		{
			name: "populated row with all relations",
			configs: []config_v0.GatewayInstanceConfig{
				{GatewayInstance: config_v0.GatewayInstanceValues{
					Name:                       util.Ptr("gwi-a"),
					GatewayDefinition:          &config_v0.GatewayDefinitionValues{Name: util.Ptr("gwd-a")},
					KubernetesRuntimeInstance:  &config_v0.KubernetesRuntimeInstanceValues{Name: util.Ptr("kri-a")},
					KubernetesWorkloadInstance: &config_v0.KubernetesWorkloadInstanceValues{Name: util.Ptr("wl-a")},
					Age:                        util.Ptr("7m"),
				}},
			},
			wants: []string{"gwi-a", "gwd-a", "kri-a", "wl-a", "7m"},
		},
		{
			name: "nil relations and nil age render empty",
			configs: []config_v0.GatewayInstanceConfig{
				{GatewayInstance: config_v0.GatewayInstanceValues{
					Name: util.Ptr("gwi-b"),
				}},
			},
			wants:    []string{"gwi-b"},
			notWants: []string{"<nil>"},
		},
		{
			name: "relations present but name pointers nil fall back to empty",
			configs: []config_v0.GatewayInstanceConfig{
				{GatewayInstance: config_v0.GatewayInstanceValues{
					Name:                       util.Ptr("gwi-c"),
					GatewayDefinition:          &config_v0.GatewayDefinitionValues{Name: nil},
					KubernetesRuntimeInstance:  &config_v0.KubernetesRuntimeInstanceValues{Name: nil},
					KubernetesWorkloadInstance: &config_v0.KubernetesWorkloadInstanceValues{Name: nil},
					Age:                        util.Ptr("2s"),
				}},
			},
			wants:    []string{"gwi-c", "2s"},
			notWants: []string{"<nil>"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke helper and capture stdout
			configs := tc.configs
			out, err := captureStdout(t, func() error {
				return outputGetv0GatewayInstancesCmd(&configs)
			})
			// verify nil-error contract
			if err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			// verify expected substrings appear
			for _, want := range tc.wants {
				if !strings.Contains(out, want) {
					t.Errorf("expected output to contain %q; got:\n%s", want, out)
				}
			}
			// verify absent substrings really are absent
			for _, nw := range tc.notWants {
				if strings.Contains(out, nw) {
					t.Errorf("expected output NOT to contain %q; got:\n%s", nw, out)
				}
			}
		})
	}
}
