package v0

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gorm.io/datatypes"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	v0 "github.com/threeport/threeport/pkg/api/v0"
)

// gvkEntry describes one GVK the fake REST mapper should know about.
type gvkEntry struct {
	group   string
	version string
	kind    string
	scope   meta.RESTScope
}

// testGVKs returns the GVKs used by the paths under test. Kept in one place so
// the fake scheme and REST mapper stay in sync.
func testGVKs() []gvkEntry {
	return []gvkEntry{
		{"", "v1", "Namespace", meta.RESTScopeRoot},
		{"", "v1", "Secret", meta.RESTScopeNamespace},
		{"", "v1", "ConfigMap", meta.RESTScopeNamespace},
		{"", "v1", "Service", meta.RESTScopeNamespace},
	}
}

// newTestScheme returns a runtime.Scheme with every GVK from testGVKs()
// registered as unstructured. The fake dynamic client uses this scheme to
// convert tracker objects back to *unstructured.Unstructured on read paths.
func newTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	for _, g := range testGVKs() {
		gvk := schema.GroupVersionKind{Group: g.group, Version: g.version, Kind: g.kind}
		scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
		listGVK := gvk
		listGVK.Kind += "List"
		scheme.AddKnownTypeWithName(listGVK, &unstructured.UnstructuredList{})
	}
	return scheme
}

// newTestMapper returns a *meta.RESTMapper populated with the GVKs the test
// paths exercise. Every group-version registered is also added as a default
// so RESTMapping() calls without an explicit version still resolve.
func newTestMapper() *meta.RESTMapper {
	gvs := []schema.GroupVersion{}
	seen := map[schema.GroupVersion]bool{}
	for _, g := range testGVKs() {
		gv := schema.GroupVersion{Group: g.group, Version: g.version}
		if seen[gv] {
			continue
		}
		seen[gv] = true
		gvs = append(gvs, gv)
	}
	m := meta.NewDefaultRESTMapper(gvs)
	for _, g := range testGVKs() {
		gvk := schema.GroupVersionKind{Group: g.group, Version: g.version, Kind: g.kind}
		m.Add(gvk, g.scope)
	}
	var rm meta.RESTMapper = m
	return &rm
}

// newTestClient returns a fake dynamic client seeded with objs, wired to the
// same GVK set as newTestMapper.
func newTestClient(objs ...runtime.Object) *dynamicfake.FakeDynamicClient {
	return dynamicfake.NewSimpleDynamicClient(newTestScheme(), objs...)
}

// baseInstaller returns a ControlPlaneInstaller with the minimal Opts needed
// for kube-facing tests. CreateOrUpdateKubeResources is set so repeated calls
// converge instead of erroring on AlreadyExists.
func baseInstaller() *ControlPlaneInstaller {
	return &ControlPlaneInstaller{
		Opts: Options{
			Name:                        "threeport",
			Namespace:                   "threeport-control-plane",
			ControlPlaneName:            "local",
			CreateOrUpdateKubeResources: true,
			InfraProvider:               "kind",
			RestApiInfo: &v0.ControlPlaneComponent{
				Name:                "threeport-rest-api",
				BinaryName:          "threeport-rest-api",
				ImageName:           "threeport-rest-api",
				ImageNamespace:      "example.io/threeport",
				ImageTag:            "v0.0.0-test",
				ServiceResourceName: ThreeportAPIServiceResourceName,
			},
			DatabaseMigratorInfo: &v0.ControlPlaneComponent{
				Name:           "database-migrator",
				BinaryName:     "database-migrator",
				ImageName:      "database-migrator",
				ImageNamespace: "example.io/threeport",
				ImageTag:       "v0.0.0-test",
			},
			AgentInfo: &v0.ControlPlaneComponent{
				Name:           "threeport-agent",
				BinaryName:     "threeport-agent",
				ImageName:      "threeport-agent",
				ImageNamespace: "example.io/threeport",
				ImageTag:       "v0.0.0-test",
			},
		},
	}
}

// TestGetThreeportAPIPort covers the port selected based on authEnabled.
func TestGetThreeportAPIPort(t *testing.T) {
	// auth-enabled maps to https/443
	if got := GetThreeportAPIPort(true); got != 443 {
		t.Errorf("auth enabled: expected 443, got %d", got)
	}

	// auth-disabled maps to http/80
	if got := GetThreeportAPIPort(false); got != 80 {
		t.Errorf("auth disabled: expected 80, got %d", got)
	}
}

// TestGetLocalThreeportAPIEndpoint covers the local endpoint composed from the
// host constant and the auth-selected port.
func TestGetLocalThreeportAPIEndpoint(t *testing.T) {
	cases := []struct {
		name string
		auth bool
		want string
	}{
		{"auth enabled uses 443", true, ThreeportLocalAPIEndpoint + ":443"},
		{"auth disabled uses 80", false, ThreeportLocalAPIEndpoint + ":80"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// the endpoint concatenates the local host constant and port
			if got := GetLocalThreeportAPIEndpoint(tc.auth); got != tc.want {
				t.Errorf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

// TestGetAPIServicePort covers the port scheme returned based on authEnabled.
func TestGetAPIServicePort(t *testing.T) {
	cpi := baseInstaller()

	// auth-enabled returns https/443
	cpi.Opts.AuthEnabled = true
	name, port := cpi.GetAPIServicePort()
	if name != "https" || port != 443 {
		t.Errorf("auth enabled: expected https/443, got %s/%d", name, port)
	}

	// auth-disabled returns http/80
	cpi.Opts.AuthEnabled = false
	name, port = cpi.GetAPIServicePort()
	if name != "http" || port != 80 {
		t.Errorf("auth disabled: expected http/80, got %s/%d", name, port)
	}
}

// TestCreateOrUpdateKubeResourceCreateOnly covers the branch that uses plain
// Create when CreateOrUpdateKubeResources is false: a fresh namespace lands.
func TestCreateOrUpdateKubeResourceCreateOnly(t *testing.T) {
	cpi := baseInstaller()
	cpi.Opts.CreateOrUpdateKubeResources = false
	client := newTestClient()
	mapper := newTestMapper()

	// build a simple namespace object with only string values so the fake
	// client's DeepCopyJSON pass does not choke on Go int types
	ns := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata":   map[string]interface{}{"name": "fresh-ns"},
	}}

	// first create against an empty tracker succeeds
	if err := cpi.CreateOrUpdateKubeResource(ns, client, mapper); err != nil {
		t.Fatalf("first create returned unexpected error: %v", err)
	}

	// second create hits AlreadyExists but CreateResource swallows it and
	// returns the input, so the wrapper still reports no error
	if err := cpi.CreateOrUpdateKubeResource(ns, client, mapper); err != nil {
		t.Fatalf("second create should be swallowed on AlreadyExists, got: %v", err)
	}
}

// TestCreateOrUpdateKubeResourceUpdate covers the branch that uses
// CreateOrUpdateResource when CreateOrUpdateKubeResources is true: repeated
// calls converge without error.
func TestCreateOrUpdateKubeResourceUpdate(t *testing.T) {
	cpi := baseInstaller()
	client := newTestClient()
	mapper := newTestMapper()

	// build a simple secret (string-only values) to avoid the fake client's
	// int-not-supported DeepCopyJSON path
	sec := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]interface{}{
			"name":      "example",
			"namespace": cpi.Opts.Namespace,
		},
		"stringData": map[string]interface{}{"a": "1"},
	}}

	// create-or-update on empty tracker
	if err := cpi.CreateOrUpdateKubeResource(sec, client, mapper); err != nil {
		t.Fatalf("initial create: %v", err)
	}

	// repeated call converges to the update path
	sec.Object["stringData"] = map[string]interface{}{"a": "2"}
	if err := cpi.CreateOrUpdateKubeResource(sec, client, mapper); err != nil {
		t.Fatalf("update: %v", err)
	}
}

// TestInstallThreeportAPITLSNilAuthConfig covers the nil-authConfig fast path:
// the function returns nil without touching the kube client.
func TestInstallThreeportAPITLSNilAuthConfig(t *testing.T) {
	cpi := baseInstaller()

	// nil kube client and mapper are never dereferenced when authConfig is nil
	if err := cpi.InstallThreeportAPITLS(nil, nil, nil, "alt.example"); err != nil {
		t.Fatalf("nil authConfig should return nil, got: %v", err)
	}
}

// TestGetThreeportAPIServiceFound covers retrieval of an existing API service
// object from the fake dynamic client.
func TestGetThreeportAPIServiceFound(t *testing.T) {
	cpi := baseInstaller()
	svc := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata": map[string]interface{}{
			"name":      cpi.Opts.RestApiInfo.ServiceResourceName,
			"namespace": cpi.Opts.Namespace,
		},
	}}
	client := newTestClient(svc)
	mapper := *newTestMapper()

	// the service seeded into the tracker is returned by name+namespace
	got, err := cpi.GetThreeportAPIService(client, mapper)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.GetName() != cpi.Opts.RestApiInfo.ServiceResourceName {
		t.Errorf("expected name %q, got %q", cpi.Opts.RestApiInfo.ServiceResourceName, got.GetName())
	}
}

// TestGetThreeportAPIServiceMissing covers the not-found path: the error is
// wrapped with the "failed to get" prefix.
func TestGetThreeportAPIServiceMissing(t *testing.T) {
	cpi := baseInstaller()
	client := newTestClient()
	mapper := *newTestMapper()

	// no service seeded, so GetResource returns NotFound and the wrapper adds
	// its "failed to get" prefix
	_, err := cpi.GetThreeportAPIService(client, mapper)
	if err == nil {
		t.Fatal("expected error for missing service")
	}
	if !strings.Contains(err.Error(), "failed to get Kubernetes service") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestDeleteNamespacesReturnsQuicklyWhenAbsent covers the happy path: the
// namespace exists in the tracker and gets removed; the Retry loop's first
// GetResource call then returns NotFound and the function returns nil.
func TestDeleteNamespacesReturnsQuicklyWhenAbsent(t *testing.T) {
	ns := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata":   map[string]interface{}{"name": "to-delete"},
	}}
	client := newTestClient(ns)
	mapper := newTestMapper()

	// bound overall runtime so a regression cannot silently hang for minutes
	// on the 120-attempt retry loop
	done := make(chan error, 1)
	go func() {
		done <- DeleteNamespaces(client, mapper, []string{"to-delete"})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("DeleteNamespaces did not return within 10s; the retry loop is hanging")
	}
}

// TestDeleteNamespacesPropagatesDeleteError covers the delete-side error path:
// a reactor injects an error at the Delete verb and the outer function wraps
// it with "failed to delete namespace".
func TestDeleteNamespacesPropagatesDeleteError(t *testing.T) {
	client := newTestClient()
	client.PrependReactor("delete", "namespaces", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("delete rejected")
	})
	mapper := newTestMapper()

	err := DeleteNamespaces(client, mapper, []string{"boom"})
	if err == nil {
		t.Fatal("expected error when Delete fails")
	}
	if !strings.Contains(err.Error(), "failed to delete namespace") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestUnInstallThreeportControlPlaneComponents covers the wrapper around
// DeleteNamespaces: it removes cpi.Opts.Namespace via the fake client.
func TestUnInstallThreeportControlPlaneComponents(t *testing.T) {
	cpi := baseInstaller()
	ns := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata":   map[string]interface{}{"name": cpi.Opts.Namespace},
	}}
	client := newTestClient(ns)
	mapper := newTestMapper()

	// the namespace is present at start; UnInstall should tear it down cleanly
	done := make(chan error, 1)
	go func() {
		done <- cpi.UnInstallThreeportControlPlaneComponents(client, mapper)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("UnInstall did not return within 10s")
	}

	// after uninstall the namespace should no longer be tracked
	_, err := client.Resource(schema.GroupVersionResource{
		Version: "v1", Resource: "namespaces",
	}).Get(context.TODO(), cpi.Opts.Namespace, metav1.GetOptions{})
	if err == nil {
		t.Fatal("expected namespace to be gone")
	}
}

// TestUpdateThreeportAgentDeploymentInvalidVolumes covers the pre-create error
// path in UpdateThreeportAgentDeployment: getControllerVolumes fails on a
// malformed AdditionalVolumes JSON and the wrapper surfaces the error.
func TestUpdateThreeportAgentDeploymentInvalidVolumes(t *testing.T) {
	cpi := baseInstaller()
	bad := datatypes.JSON([]byte("not-json"))
	cpi.Opts.AgentInfo.AdditionalVolumes = &bad
	// nil client and mapper are never touched: the vols-decode error happens
	// before any kube call
	err := cpi.UpdateThreeportAgentDeployment(nil, nil)
	if err == nil {
		t.Fatal("expected error for malformed AdditionalVolumes")
	}
	if !strings.Contains(err.Error(), "could not get agent vols") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestUpdateThreeportAPIDeploymentInvalidVolumes covers the pre-create error
// path in UpdateThreeportAPIDeployment: getAPIVolumes fails on a malformed
// AdditionalVolumes JSON.
func TestUpdateThreeportAPIDeploymentInvalidVolumes(t *testing.T) {
	cpi := baseInstaller()
	bad := datatypes.JSON([]byte("not-json"))
	cpi.Opts.RestApiInfo.AdditionalVolumes = &bad
	// nil client and mapper are never touched: the vols-decode error happens
	// before any kube call
	err := cpi.UpdateThreeportAPIDeployment(nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for malformed AdditionalVolumes")
	}
	if !strings.Contains(err.Error(), "could not get vols") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestUpdateControllerDeploymentInvalidAdditionalEnvRef covers the pre-create
// error path in UpdateControllerDeployment: a malformed AdditionalEnvRef
// surfaces via the wrapper without any kube round trip.
func TestUpdateControllerDeploymentInvalidAdditionalEnvRef(t *testing.T) {
	cpi := baseInstaller()

	enabled := true
	bad := datatypes.JSON([]byte("not-json"))
	ctrl := v0.ControlPlaneComponent{
		Name:               "custom-controller",
		BinaryName:         "custom-controller",
		ImageName:          "custom-controller",
		ImageNamespace:     "example.io/threeport",
		ImageTag:           "v0.0.0-test",
		ServiceAccountName: "custom-controller",
		Enabled:            &enabled,
		AdditionalEnvRef:   &bad,
	}

	// nil client and mapper are never touched: the envRef-decode error happens
	// before any kube call
	err := cpi.UpdateControllerDeployment(nil, nil, ctrl)
	if err == nil {
		t.Fatal("expected error for malformed AdditionalEnvRef")
	}
	// the deployment builder surfaces the JSON error inside its
	// "failed to get <name> deployment" wrapper
	if !strings.Contains(err.Error(), "failed to unmarshal json") {
		t.Errorf("unexpected error: %v", err)
	}
}

// compile-time sanity: our fake client satisfies the dynamic.Interface the
// installer methods accept.
var _ dynamic.Interface = (*dynamicfake.FakeDynamicClient)(nil)
