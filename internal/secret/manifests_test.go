package secret

import (
	"strings"
	"testing"

	v0 "github.com/threeport/threeport/pkg/api/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestGetExternalSecretPopulatesNamesFromInstanceAndDefinition asserts that
// getExternalSecret() returns an unstructured ExternalSecret whose metadata,
// secretStoreRef, target, and dataFrom fields draw from the configured
// SecretInstance and SecretDefinition names.
func TestGetExternalSecretPopulatesNamesFromInstanceAndDefinition(t *testing.T) {
	// build a config with a named secret instance and definition
	c := &SecretInstanceConfig{
		secretInstance: &v0.SecretInstance{
			Instance: v0.Instance{Name: util.Ptr("my-instance")},
		},
		secretDefinition: &v0.SecretDefinition{
			Definition: v0.Definition{Name: util.Ptr("my-definition")},
		},
	}

	// invoke the method under test
	es := c.getExternalSecret()

	// verify apiVersion and kind are the ExternalSecrets Operator v1beta1 values
	if got := es.Object["apiVersion"]; got != "external-secrets.io/v1beta1" {
		t.Fatalf("apiVersion mismatch: got %q", got)
	}
	if got := es.Object["kind"]; got != "ExternalSecret" {
		t.Fatalf("kind mismatch: got %q", got)
	}

	// verify metadata.name is derived from the SecretInstance name
	meta := es.Object["metadata"].(map[string]interface{})
	if meta["name"] != "my-instance" {
		t.Fatalf("metadata.name = %q, want my-instance", meta["name"])
	}

	// verify secretStoreRef.name matches the instance name and kind is SecretStore
	spec := es.Object["spec"].(map[string]interface{})
	storeRef := spec["secretStoreRef"].(map[string]interface{})
	if storeRef["name"] != "my-instance" || storeRef["kind"] != "SecretStore" {
		t.Fatalf("secretStoreRef mismatch: %+v", storeRef)
	}

	// verify target uses the instance name with an Owner creationPolicy
	target := spec["target"].(map[string]interface{})
	if target["name"] != "my-instance" || target["creationPolicy"] != "Owner" {
		t.Fatalf("target mismatch: %+v", target)
	}

	// verify dataFrom.extract.key sources from the SecretDefinition name
	dataFrom := spec["dataFrom"].([]interface{})
	if len(dataFrom) != 1 {
		t.Fatalf("dataFrom length = %d, want 1", len(dataFrom))
	}
	extract := dataFrom[0].(map[string]interface{})["extract"].(map[string]interface{})
	if extract["key"] != "my-definition" {
		t.Fatalf("dataFrom.extract.key = %q, want my-definition", extract["key"])
	}
}

// TestGetSecretStoreReturnsAwsProviderRegionForEks asserts that getSecretStore()
// resolves the EKS infra provider to AWS and the location to its AWS region.
func TestGetSecretStoreReturnsAwsProviderRegionForEks(t *testing.T) {
	// configure EKS runtime in a location whose AWS region is us-east-1
	c := &SecretInstanceConfig{
		secretInstance: &v0.SecretInstance{
			Instance: v0.Instance{Name: util.Ptr("store-1")},
		},
		kubernetesRuntimeDefinition: &v0.KubernetesRuntimeDefinition{
			InfraProvider: util.Ptr("eks"),
		},
		kubernetesRuntimeInstance: &v0.KubernetesRuntimeInstance{
			Location: util.Ptr("NorthAmerica:NewYork"),
		},
	}

	// invoke the method under test
	store, err := c.getSecretStore()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// verify apiVersion and kind identify a SecretStore
	if got := store.Object["apiVersion"]; got != "external-secrets.io/v1beta1" {
		t.Fatalf("apiVersion mismatch: got %q", got)
	}
	if got := store.Object["kind"]; got != "SecretStore" {
		t.Fatalf("kind mismatch: got %q", got)
	}

	// verify metadata.name matches the SecretInstance name
	meta := store.Object["metadata"].(map[string]interface{})
	if meta["name"] != "store-1" {
		t.Fatalf("metadata.name = %q, want store-1", meta["name"])
	}

	// verify provider.aws.service is SecretsManager and the region resolved to us-east-1
	spec := store.Object["spec"].(map[string]interface{})
	aws := spec["provider"].(map[string]interface{})["aws"].(map[string]interface{})
	if aws["service"] != "SecretsManager" {
		t.Fatalf("aws.service = %q, want SecretsManager", aws["service"])
	}
	if aws["region"] != "us-east-1" {
		t.Fatalf("aws.region = %q, want us-east-1", aws["region"])
	}
}

// TestGetSecretStoreRejectsUnsupportedInfraProvider asserts that an unknown
// InfraProvider causes getSecretStore() to return an error surfaced from the
// cloud-provider lookup.
func TestGetSecretStoreRejectsUnsupportedInfraProvider(t *testing.T) {
	// configure an infra provider that has no cloud-provider mapping
	c := &SecretInstanceConfig{
		secretInstance: &v0.SecretInstance{
			Instance: v0.Instance{Name: util.Ptr("store-1")},
		},
		kubernetesRuntimeDefinition: &v0.KubernetesRuntimeDefinition{
			InfraProvider: util.Ptr("not-a-provider"),
		},
		kubernetesRuntimeInstance: &v0.KubernetesRuntimeInstance{
			Location: util.Ptr("NorthAmerica:NewYork"),
		},
	}

	// invoke the method under test
	store, err := c.getSecretStore()

	// verify the error surfaces the wrapped cloud-provider lookup failure
	if err == nil {
		t.Fatalf("expected error, got store=%+v", store)
	}
	if !strings.Contains(err.Error(), "failed to get cloud provider") {
		t.Fatalf("error text = %q, want it to wrap the cloud-provider lookup", err.Error())
	}
}

// TestGetSecretStoreRejectsUnknownLocation asserts that a location the region
// mapping does not recognize causes getSecretStore() to return an error
// surfaced from the region lookup.
func TestGetSecretStoreRejectsUnknownLocation(t *testing.T) {
	// configure a valid provider paired with an unknown location
	c := &SecretInstanceConfig{
		secretInstance: &v0.SecretInstance{
			Instance: v0.Instance{Name: util.Ptr("store-1")},
		},
		kubernetesRuntimeDefinition: &v0.KubernetesRuntimeDefinition{
			InfraProvider: util.Ptr("eks"),
		},
		kubernetesRuntimeInstance: &v0.KubernetesRuntimeInstance{
			Location: util.Ptr("Antarctica:McMurdo"),
		},
	}

	// invoke the method under test
	store, err := c.getSecretStore()

	// verify the error surfaces the wrapped region lookup failure
	if err == nil {
		t.Fatalf("expected error, got store=%+v", store)
	}
	if !strings.Contains(err.Error(), "failed to get provider region for location") {
		t.Fatalf("error text = %q, want it to wrap the region lookup", err.Error())
	}
}

// TestGetExternalSecretsSupportServiceYamlEmbedsIamRoleArn asserts that
// getExternalSecretsSupportServiceYaml() serializes an ExternalSecrets custom
// resource carrying the provided IAM role ARN and the fixed
// nukleros-secrets-system defaults.
func TestGetExternalSecretsSupportServiceYamlEmbedsIamRoleArn(t *testing.T) {
	// only the ARN argument varies; the config itself is not consulted
	c := &SecretInstanceConfig{}
	arn := "arn:aws:iam::123456789012:role/external-secrets"

	// invoke the method under test
	yaml, err := c.getExternalSecretsSupportServiceYaml(arn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// verify the serialized CR advertises the expected apiVersion, kind, and metadata name
	for _, want := range []string{
		"apiVersion: secrets.support-services.nukleros.io/v1alpha1",
		"kind: ExternalSecrets",
		"name: externalsecrets",
	} {
		if !strings.Contains(yaml, want) {
			t.Fatalf("yaml missing %q\n%s", want, yaml)
		}
	}

	// verify the ARN was threaded into the spec and defaults were preserved
	if !strings.Contains(yaml, "iamRoleArn: "+arn) {
		t.Fatalf("yaml missing iamRoleArn %q\n%s", arn, yaml)
	}
	if !strings.Contains(yaml, "namespace: nukleros-secrets-system") {
		t.Fatalf("yaml missing nukleros-secrets-system namespace\n%s", yaml)
	}
	if !strings.Contains(yaml, "image: ghcr.io/external-secrets/external-secrets") {
		t.Fatalf("yaml missing external-secrets image\n%s", yaml)
	}
}
