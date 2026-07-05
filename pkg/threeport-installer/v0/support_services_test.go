package v0

import (
	"errors"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

// failingMapper implements meta.RESTMapper and always returns an error so any
// downstream kube.CreateResource call fails at the mapping step.
type failingMapper struct{}

func (f *failingMapper) KindFor(_ schema.GroupVersionResource) (schema.GroupVersionKind, error) {
	return schema.GroupVersionKind{}, errors.New("no kind for resource")
}

func (f *failingMapper) KindsFor(_ schema.GroupVersionResource) ([]schema.GroupVersionKind, error) {
	return nil, errors.New("no kinds for resource")
}

func (f *failingMapper) ResourceFor(_ schema.GroupVersionResource) (schema.GroupVersionResource, error) {
	return schema.GroupVersionResource{}, errors.New("no resource")
}

func (f *failingMapper) ResourcesFor(_ schema.GroupVersionResource) ([]schema.GroupVersionResource, error) {
	return nil, errors.New("no resources")
}

func (f *failingMapper) RESTMapping(_ schema.GroupKind, _ ...string) (*meta.RESTMapping, error) {
	return nil, errors.New("no mapping")
}

func (f *failingMapper) RESTMappings(_ schema.GroupKind, _ ...string) ([]*meta.RESTMapping, error) {
	return nil, errors.New("no mappings")
}

func (f *failingMapper) ResourceSingularizer(resource string) (string, error) {
	return resource, nil
}

// newEmptyDynamicClient returns a fake dynamic client with an empty scheme.
// The client is passed to installers whose mappings fail first, so no Create
// call actually reaches it.
func newEmptyDynamicClient() dynamic.Interface {
	return dynamicfake.NewSimpleDynamicClient(k8sruntime.NewScheme())
}

// TestSupportServicesConstants asserts stable identity for the exported string
// constants. These constants link the installed ServiceAccount names to the
// aws-builder IAM role bindings, so a silent rename would break IRSA wiring.
func TestSupportServicesConstants(t *testing.T) {
	// each pair is (constant value, expected literal) so a rename surfaces
	// as a test failure instead of a silent identity break
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"SupportServicesOperatorImage", SupportServicesOperatorImage, "ghcr.io/nukleros/support-services-operator:v0.6.0"},
		{"RBACProxyImage", RBACProxyImage, "ghcr.io/kube-rbac-proxy/kube-rbac-proxy:v0.22.0"},
		{"DNSManagerServiceAccountName", DNSManagerServiceAccountName, "external-dns"},
		{"DNSManagerServiceAccountNamepace", DNSManagerServiceAccountNamepace, "nukleros-gateway-system"},
		{"DNS01ChallengeServiceAccountName", DNS01ChallengeServiceAccountName, "cert-manager"},
		{"DNS01ChallengeServiceAccountNamepace", DNS01ChallengeServiceAccountNamepace, "nukleros-certs-system"},
		{"SecretsManagerServiceAccountName", SecretsManagerServiceAccountName, "external-secrets"},
		{"SecretsManagerServiceAccountNamespace", SecretsManagerServiceAccountNamespace, "nukleros-secrets-system"},
		{"StorageManagerServiceAccountName", StorageManagerServiceAccountName, "ebs-csi-controller-sa"},
		{"StorageManagerServiceAccountNamespace", StorageManagerServiceAccountNamespace, "kube-system"},
		{"ClusterAutoscalerServiceAccountName", ClusterAutoscalerServiceAccountName, "cluster-autoscaler"},
		{"ClusterAutoscalerNamespace", ClusterAutoscalerNamespace, "kube-system"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// assert each constant matches the expected literal
			if tc.got != tc.want {
				t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
			}
		})
	}
}

// TestInstallThreeportCRDs_MapperFailure covers the mapper-error branch: when
// the REST mapper cannot resolve the first CRD's group/kind, the function
// short-circuits and returns a wrapped error naming the cert-manager CRD (the
// first resource attempted).
func TestInstallThreeportCRDs_MapperFailure(t *testing.T) {
	// arrange: failing mapper causes CreateResource to fail immediately
	var mapper meta.RESTMapper = &failingMapper{}
	client := newEmptyDynamicClient()

	// act: attempt to install all CRDs
	err := InstallThreeportCRDs(client, &mapper)

	// assert: error is non-nil, mentions the cert-manager CRD, and unwraps
	// to the underlying mapper failure
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to create cert manager crd") {
		t.Errorf("error should identify cert manager crd, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "no mapping") {
		t.Errorf("error should wrap the mapping failure, got %q", err.Error())
	}
}

// TestInstallThreeportSupportServicesOperator_MapperFailure covers the
// mapper-error branch: the operator installer aborts on the first
// ServiceAccount and reports a wrapped "service account" error.
func TestInstallThreeportSupportServicesOperator_MapperFailure(t *testing.T) {
	// arrange
	var mapper meta.RESTMapper = &failingMapper{}
	client := newEmptyDynamicClient()

	// act
	err := InstallThreeportSupportServicesOperator(client, &mapper)

	// assert: error identifies the first resource (service account) and
	// wraps the mapping failure
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to create service account") {
		t.Errorf("error should identify service account, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "no mapping") {
		t.Errorf("error should wrap the mapping failure, got %q", err.Error())
	}
}

// TestInstallEksThreeportSystemServices covers the EKS installer's mapper
// failure path and boundary cases on the templated cluster-name and account-id.
// The function's happy path cannot be tested against the fake dynamic client
// because the resource definitions contain untyped int literals that panic the
// tracker's DeepCopyJSON; the mapper-failure paths still execute every
// resource-construction statement up to the first Create call.
func TestInstallEksThreeportSystemServices(t *testing.T) {
	// table drives common failure and boundary inputs; each row expects the
	// same wrapped "cluster autoscaler service account" error surface because
	// the mapper always fails on the first resource
	cases := []struct {
		name        string
		clusterName string
		accountId   string
	}{
		{"populated cluster and account", "cluster-a", "123456789012"},
		{"empty account id boundary", "cluster-a", ""},
		{"empty cluster name boundary", "", "123456789012"},
		{"both empty boundary", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// arrange: failing mapper aborts on the first Create call
			var mapper meta.RESTMapper = &failingMapper{}
			client := newEmptyDynamicClient()

			// act: build and attempt to install every EKS resource; empty
			// inputs must not panic during the fmt.Sprintf arn templating
			err := InstallEksThreeportSystemServices(client, &mapper, tc.clusterName, tc.accountId)

			// assert: error consistently identifies the first EKS resource
			// (the cluster-autoscaler service account) and wraps the
			// underlying mapping failure
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), "failed to create cluster autoscaler service account") {
				t.Errorf("error should identify cluster autoscaler service account, got %q", err.Error())
			}
			if !strings.Contains(err.Error(), "no mapping") {
				t.Errorf("error should wrap the mapping failure, got %q", err.Error())
			}
		})
	}
}
