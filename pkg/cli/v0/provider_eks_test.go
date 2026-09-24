package v0

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/threeport/threeport/internal/provider"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	kube "github.com/threeport/threeport/pkg/kube/v0"
	threeport "github.com/threeport/threeport/pkg/threeport-installer/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// newTestInstaller returns a ControlPlaneInstaller with the minimum Opts
// fields populated for the exported EKS helpers under test.
func newTestInstaller(providerConfigDir, controlPlaneName string) *threeport.ControlPlaneInstaller {
	return &threeport.ControlPlaneInstaller{
		Opts: threeport.Options{
			Name:              "threeport",
			Namespace:         "threeport-control-plane",
			ControlPlaneName:  controlPlaneName,
			ProviderConfigDir: providerConfigDir,
			AwsRegion:         "us-east-1",
		},
	}
}

// newTestUninstaller returns an Uninstaller wired to skip infra teardown so
// cleanOnCreateError returns the wrapped createErr immediately.
func newTestUninstaller(cpi *threeport.ControlPlaneInstaller) *Uninstaller {
	return &Uninstaller{
		teardownOnFailure: util.Ptr(false),
		cpi:               cpi,
	}
}

// newCallerIdentity returns a synthetic caller-identity output whose Account
// and Arn pointers are safe to dereference.
func newCallerIdentity() *sts.GetCallerIdentityOutput {
	acct := "123456789012"
	arn := "arn:aws:iam::123456789012:user/test"
	uid := "AIDAEXAMPLE"
	return &sts.GetCallerIdentityOutput{
		Account: &acct,
		Arn:     &arn,
		UserId:  &uid,
	}
}

// TestPrepForEksDeletion_ControlPlaneNotFoundInConfig asserts that
// PrepForEksDeletion propagates the "control plane not found" error surfaced
// by ThreeportConfig.GetAwsConfigs when the requested control plane is
// absent from the config.
func TestPrepForEksDeletion_ControlPlaneNotFoundInConfig(t *testing.T) {
	// arrange: empty config so GetControlPlaneConfig returns not-found
	cfg := &ThreeportConfig{ControlPlanes: nil}
	cp := &ControlPlane{Name: "missing"}
	cpi := newTestInstaller(t.TempDir(), "missing")

	// act: request deletion prep for a control plane that is not tracked
	got, err := PrepForEksDeletion(cpi, cp, cfg, nil, nil, "missing")

	// assert: infra pointer is nil and error explains the missing config
	if err == nil {
		t.Fatalf("expected error for missing control plane; got nil")
	}
	if got != nil {
		t.Fatalf("expected nil infra on error; got %#v", got)
	}
	if !strings.Contains(err.Error(), "failed to get AWS configs from threeport config") {
		t.Fatalf("expected error to wrap GetAwsConfigs failure; got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected error chain to mention not-found cause; got %q", err.Error())
	}
}

// TestConfigureEksKubernetesRuntimeInstance_MissingInventory asserts the
// helper surfaces an error when the EKS inventory file for the control plane
// is absent from the provider config directory.
func TestConfigureEksKubernetesRuntimeInstance_MissingInventory(t *testing.T) {
	// arrange: a fresh temp dir with no inventory file for the control plane
	dir := t.TempDir()
	cpName := "missing-inventory"
	cpi := newTestInstaller(dir, cpName)
	uninstaller := newTestUninstaller(cpi)
	identity := newCallerIdentity()
	awsConf := &aws.Config{Region: "us-east-1"}
	kubeConn := &kube.KubeConnectionInfo{
		APIEndpoint:   "https://example.com",
		CACertificate: "ca",
		Token:         "tok",
	}

	// act: attempt to configure the runtime instance without a stored inventory
	var runtimeInst *v0.KubernetesRuntimeInstance
	err := ConfigureEksKubernetesRuntimeInstance(
		cpi,
		kubeConn,
		uninstaller,
		awsConf,
		identity,
		awsConf,
		runtimeInst,
		"threeport-missing-inventory",
		false,
		true,
		true,
	)

	// assert: error mentions the inventory read failure and no os-level file
	if err == nil {
		t.Fatalf("expected error when inventory file is missing; got nil")
	}
	if !strings.Contains(err.Error(), "failed to read eks kubernetes runtime inventory") {
		t.Fatalf("expected error to mention inventory read failure; got %q", err.Error())
	}
	// sanity: confirm the file the helper tried to load really was absent
	inventoryPath := provider.EKSInventoryFilepath(dir, cpName)
	if _, statErr := os.Stat(inventoryPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected no inventory file at %s; got stat err %v", inventoryPath, statErr)
	}
}

// TestConfigureControlPlaneWithEksConfig_APIError asserts that a failure
// creating the default AWS provider in the Threeport API is surfaced back to
// the caller via cleanOnCreateError.
func TestConfigureControlPlaneWithEksConfig_APIError(t *testing.T) {
	// arrange: server returns 500 on the aws-providers POST so CreateAwsProvider fails
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"Status":{"Error":"boom"}}`))
	}))
	defer srv.Close()

	cpi := newTestInstaller(t.TempDir(), "eks-api-error")
	uninstaller := newTestUninstaller(cpi)
	identity := newCallerIdentity()
	awsConf := &aws.Config{Region: "us-east-1"}
	var infra provider.KubernetesRuntimeInfra = &provider.KubernetesRuntimeInfraEKS{
		RuntimeInstanceName:          "threeport-eks-api-error",
		AwsAccountID:                 *identity.Account,
		ZoneCount:                    2,
		DefaultNodeGroupInstanceType: "t3.medium",
		DefaultNodeGroupInitialNodes: 3,
		DefaultNodeGroupMinNodes:     3,
		DefaultNodeGroupMaxNodes:     250,
	}
	kubeRuntimeDef := &v0.KubernetesRuntimeDefinition{}
	kubeRuntimeInst := &v0.KubernetesRuntimeInstance{}

	// act: strip the http scheme because client.GetResponse prepends its own
	apiAddr := strings.TrimPrefix(srv.URL, "http://")
	err := ConfigureControlPlaneWithEksConfig(
		cpi,
		uninstaller,
		awsConf,
		identity,
		awsConf,
		srv.Client(),
		apiAddr,
		&infra,
		kubeRuntimeDef,
		kubeRuntimeInst,
	)

	// assert: error mentions the CreateAwsProvider failure that started the chain
	if err == nil {
		t.Fatalf("expected error when API POST returns 500; got nil")
	}
	if !strings.Contains(err.Error(), "failed to create new default AWS provider") {
		t.Fatalf("expected AWS-provider create failure in error chain; got %q", err.Error())
	}
}

// TestPrepForEksDeletion_EmptyControlPlaneName asserts the helper still
// routes an empty control-plane name through GetAwsConfigs and returns the
// same not-found error shape rather than panicking.
func TestPrepForEksDeletion_EmptyControlPlaneName(t *testing.T) {
	// arrange: config with one unrelated control plane
	cfg := &ThreeportConfig{
		ControlPlanes: []ControlPlane{{Name: "other"}},
	}
	cp := &ControlPlane{Name: ""}
	cpi := newTestInstaller(t.TempDir(), "")

	// act: request deletion prep for an empty name
	got, err := PrepForEksDeletion(cpi, cp, cfg, nil, nil, "")

	// assert: nil infra, not-found error surfaced from config lookup
	if got != nil {
		t.Fatalf("expected nil infra on empty control plane name; got %#v", got)
	}
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error; got %v", err)
	}
}

