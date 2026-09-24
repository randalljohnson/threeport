package gateway

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	v0 "github.com/threeport/threeport/pkg/api/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestGetCleanedDomain asserts the cleaner strips http:// and https:// prefixes
// and leaves bare domains untouched.
func TestGetCleanedDomain(t *testing.T) {
	// each case names the behavior verified: strip http prefix, strip https
	// prefix, pass through unaltered, and handle an empty input.
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"strips http prefix", "http://example.com", "example.com"},
		{"strips https prefix", "https://example.com", "example.com"},
		{"passes through bare domain", "example.com", "example.com"},
		{"passes through empty", "", ""},
		{"strips only one prefix", "https://http://example.com", "http://example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// exercise the cleaner and assert it returns the expected value.
			assert.Equal(t, tc.want, getCleanedDomain(tc.in))
		})
	}
}

// TestGetVirtualServiceName asserts the naming rules: bare gateway name when
// no domain is provided; kebab-cased domain otherwise; and consistent handling
// of scheme prefixes.
func TestGetVirtualServiceName(t *testing.T) {
	// build a gateway definition whose name is the fallback branch for an
	// empty-domain call.
	gwName := "my-gateway"
	gd := &v0.GatewayDefinition{
		Definition: v0.Definition{Name: &gwName},
	}

	// no domain: name should fall back to the gateway definition name.
	t.Run("empty domain uses gateway name", func(t *testing.T) {
		got := getVirtualServiceName(gd, "", 80)
		assert.Equal(t, "my-gateway-80", got)
	})

	// domain present: name is the kebab-cased domain.
	t.Run("domain kebab-cases hostname", func(t *testing.T) {
		got := getVirtualServiceName(gd, "Foo.Example.com", 443)
		assert.Equal(t, "foo-example-com-443", got)
	})

	// scheme prefix is stripped before kebab-casing.
	t.Run("scheme is stripped before naming", func(t *testing.T) {
		got := getVirtualServiceName(gd, "https://example.com", 443)
		assert.Equal(t, "example-com-443", got)
	})
}

// TestGetGlooEdgePort asserts the port helper builds an unstructured with the
// input fields set verbatim.
func TestGetGlooEdgePort(t *testing.T) {
	// build a port with representative values for each field.
	port := getGlooEdgePort("HTTP", "http-port", 8080, true)

	// each field should be readable back from the unstructured object.
	assert.Equal(t, "HTTP", port.Object["protocol"])
	assert.Equal(t, "http-port", port.Object["name"])
	assert.Equal(t, int64(8080), port.Object["port"])
	assert.Equal(t, true, port.Object["ssl"])
}

// TestGetGlooEdgeYaml asserts the gloo edge manifest carries the expected
// apiVersion, kind, name, and namespace fields.
func TestGetGlooEdgeYaml(t *testing.T) {
	// invoke the builder and parse the resulting YAML back to a map.
	got, err := getGlooEdgeYaml()
	require.NoError(t, err)

	obj := map[string]interface{}{}
	require.NoError(t, yaml.Unmarshal([]byte(got), &obj))

	// verify the apiVersion and kind identify the resource type.
	assert.Equal(t, "gateway.support-services.nukleros.io/v1alpha1", obj["apiVersion"])
	assert.Equal(t, "GlooEdge", obj["kind"])

	// verify the metadata name and spec namespace match the hardcoded values.
	meta := obj["metadata"].(map[string]interface{})
	assert.Equal(t, "glooedge", meta["name"])

	spec := obj["spec"].(map[string]interface{})
	assert.Equal(t, util.GatewaySystemNamespace, spec["namespace"])
	// ports should be an empty list on a fresh manifest.
	assert.NotNil(t, spec["ports"])
}

// TestGetExternalDnsYaml asserts each input argument surfaces in the correct
// spec field, and that the extraArgs list encodes the gloo namespace and
// runtime instance identifier.
func TestGetExternalDnsYaml(t *testing.T) {
	// call the builder with distinct string arguments so each field can be
	// asserted independently.
	got, err := getExternalDnsYaml(
		"example.com",
		"aws",
		"arn:aws:iam::123:role/dns",
		"my-gcp-project",
		"gloo-system",
		"kri-42",
	)
	require.NoError(t, err)

	obj := map[string]interface{}{}
	require.NoError(t, yaml.Unmarshal([]byte(got), &obj))

	// top-level identifying fields.
	assert.Equal(t, "ExternalDNS", obj["kind"])
	spec := obj["spec"].(map[string]interface{})

	// domain, provider, iamRoleArn, and gcpProject should round-trip.
	assert.Equal(t, "example.com", spec["domainName"])
	assert.Equal(t, "aws", spec["provider"])
	assert.Equal(t, "arn:aws:iam::123:role/dns", spec["iamRoleArn"])
	assert.Equal(t, "my-gcp-project", spec["gcpProject"])

	// extraArgs should embed the gloo namespace and runtime instance id.
	extras := spec["extraArgs"].([]interface{})
	joined := ""
	for _, e := range extras {
		joined += e.(string) + "\n"
	}
	assert.Contains(t, joined, "--gloo-namespace=gloo-system")
	assert.Contains(t, joined, "--txt-owner-id=kri-42-")
	assert.Contains(t, joined, "--txt-prefix=kri-42-")
	assert.Contains(t, joined, "--source=gloo-proxy")
}

// TestGetCertManagerYaml asserts the cert manager manifest carries the
// input iamRoleArn and the expected static replicas and images.
func TestGetCertManagerYaml(t *testing.T) {
	// call the builder with a distinct role arn.
	got, err := getCertManagerYaml("arn:aws:iam::123:role/cert")
	require.NoError(t, err)

	obj := map[string]interface{}{}
	require.NoError(t, yaml.Unmarshal([]byte(got), &obj))

	// verify identifying fields.
	assert.Equal(t, "CertManager", obj["kind"])
	spec := obj["spec"].(map[string]interface{})
	assert.Equal(t, "arn:aws:iam::123:role/cert", spec["iamRoleArn"])
	assert.Equal(t, "nukleros-certs-system", spec["namespace"])

	// each subcomponent block should exist and carry a replica count.
	for _, key := range []string{"cainjector", "controller", "webhook"} {
		sub, ok := spec[key].(map[string]interface{})
		require.True(t, ok, "expected %s block", key)
		assert.NotNil(t, sub["image"])
		assert.NotNil(t, sub["replicas"])
	}
}

// TestGetIssuerYaml asserts the issuer manifest is named after the kebab-cased
// domain and carries the admin email and dns zone.
func TestGetIssuerYaml(t *testing.T) {
	// gateway definition value is unused by the current implementation but
	// is passed to preserve the callable signature.
	gwName := "gw"
	gd := &v0.GatewayDefinition{
		Definition: v0.Definition{Name: &gwName},
	}

	// invoke the builder with a domain and admin email.
	got, err := getIssuerYaml(gd, "Foo.Example.com", "admin@example.com")
	require.NoError(t, err)

	obj := map[string]interface{}{}
	require.NoError(t, yaml.Unmarshal([]byte(got), &obj))

	// verify identifying fields.
	assert.Equal(t, "Issuer", obj["kind"])

	// metadata name should be the kebab-cased domain.
	meta := obj["metadata"].(map[string]interface{})
	assert.Equal(t, "foo-example-com", meta["name"])

	// email and dnsZones should reflect the inputs.
	spec := obj["spec"].(map[string]interface{})
	acme := spec["acme"].(map[string]interface{})
	assert.Equal(t, "admin@example.com", acme["email"])

	solvers := acme["solvers"].([]interface{})
	require.Len(t, solvers, 1)
	solver := solvers[0].(map[string]interface{})
	selector := solver["selector"].(map[string]interface{})
	zones := selector["dnsZones"].([]interface{})
	require.Len(t, zones, 1)
	assert.Equal(t, "Foo.Example.com", zones[0])

	// server URL should point at the letsencrypt staging endpoint.
	server, ok := acme["server"].(string)
	require.True(t, ok)
	assert.True(t, strings.HasPrefix(server, "https://acme-"))
}

// TestGetCertificateYaml asserts the certificate manifest is named after the
// kebab-cased domain and references a matching kebab-cased issuer + tls secret.
func TestGetCertificateYaml(t *testing.T) {
	// gateway definition value is unused by the current implementation.
	gwName := "gw"
	gd := &v0.GatewayDefinition{
		Definition: v0.Definition{Name: &gwName},
	}

	// invoke the builder with a domain.
	got, err := getCertificateYaml(gd, "Foo.Example.com")
	require.NoError(t, err)

	obj := map[string]interface{}{}
	require.NoError(t, yaml.Unmarshal([]byte(got), &obj))

	// verify identifying fields.
	assert.Equal(t, "Certificate", obj["kind"])

	// metadata name should be the kebab-cased domain.
	meta := obj["metadata"].(map[string]interface{})
	assert.Equal(t, "foo-example-com", meta["name"])

	spec := obj["spec"].(map[string]interface{})

	// secretName should be the kebab-cased domain + "-tls".
	assert.Equal(t, "foo-example-com-tls", spec["secretName"])

	// dnsNames should carry the raw domain input.
	dnsNames := spec["dnsNames"].([]interface{})
	require.Len(t, dnsNames, 1)
	assert.Equal(t, "Foo.Example.com", dnsNames[0])

	// issuerRef should name the same kebab-cased issuer.
	issuerRef := spec["issuerRef"].(map[string]interface{})
	assert.Equal(t, "foo-example-com", issuerRef["name"])
	assert.Equal(t, "Issuer", issuerRef["kind"])
}
