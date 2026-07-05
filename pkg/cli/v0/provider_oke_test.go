package v0

import (
	"crypto/rsa"
	"errors"
	"strings"
	"testing"

	"github.com/mitchellh/go-homedir"
	"github.com/oracle/oci-go-sdk/v65/common"

	"github.com/threeport/threeport/internal/provider"
	kube "github.com/threeport/threeport/pkg/kube/v0"
	threeport "github.com/threeport/threeport/pkg/threeport-installer/v0"
)

// stubConfigProvider is a minimal common.ConfigurationProvider implementation
// used by ConfigureControlPlaneWithOkeConfig tests. Each field is returned
// verbatim from the matching method; setting tenancyErr makes the earliest
// call in the exercised path fail with a wrappable root cause.
type stubConfigProvider struct {
	tenancy     string
	tenancyErr  error
	user        string
	userErr     error
	fingerprint string
	region      string
	privateKey  *rsa.PrivateKey
}

func (s *stubConfigProvider) TenancyOCID() (string, error)    { return s.tenancy, s.tenancyErr }
func (s *stubConfigProvider) UserOCID() (string, error)       { return s.user, s.userErr }
func (s *stubConfigProvider) KeyFingerprint() (string, error) { return s.fingerprint, nil }
func (s *stubConfigProvider) Region() (string, error)         { return s.region, nil }
func (s *stubConfigProvider) AuthType() (common.AuthConfig, error) {
	return common.AuthConfig{AuthType: common.UserPrincipal, IsFromConfigFile: true}, nil
}
func (s *stubConfigProvider) PrivateRSAKey() (*rsa.PrivateKey, error) { return s.privateKey, nil }
func (s *stubConfigProvider) KeyID() (string, error)                  { return "kid", nil }

// asserting the interface satisfaction avoids silent drift if the OCI SDK
// widens the interface.
var _ common.ConfigurationProvider = (*stubConfigProvider)(nil)

// TestDeployOkeInfraLoadOCIConfigFailurePopulatesInfraAndReturnsWrappedError
// covers the early-fail path where LoadOCIConfig cannot find ~/.oci/config.
// The returned error must wrap "failed to load OCI config", and the runtime
// infra out-pointer must already carry the OKE-typed value (assignment
// happens before LoadOCIConfig).
func TestDeployOkeInfraLoadOCIConfigFailurePopulatesInfraAndReturnsWrappedError(t *testing.T) {
	// point HOME at a scratch dir so LoadOCIConfig cannot resolve ~/.oci/config
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	homedir.Reset()
	t.Cleanup(homedir.Reset)

	// build minimal installer with OCI-related opts filled in
	cpi := &threeport.ControlPlaneInstaller{
		Opts: threeport.Options{
			OciRegion:        "us-phoenix-1",
			OciConfigProfile: "DEFAULT",
			ControlPlaneName: "tp-test",
		},
	}
	cpConfig := &ControlPlane{
		Name: "tp-test",
		OKEProviderConfig: OKEProviderConfig{
			OciCompartmentOcid: "ocid1.compartment.oc1..existing",
		},
	}
	tpConfig := &ThreeportConfig{}
	var runtimeInfra provider.KubernetesRuntimeInfra
	kubeInfo := &kube.KubeConnectionInfo{}
	teardown := false
	cleanCfg := false
	uninstaller := &Uninstaller{teardownOnFailure: &teardown, cleanConfig: &cleanCfg}

	// invoke deploy; LoadOCIConfig must fail before any other work
	err := DeployOkeInfra(cpi, cpConfig, tpConfig, &runtimeInfra, kubeInfo, uninstaller)

	// assert the error wraps the load-config framing
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to load OCI config") {
		t.Errorf("expected error to wrap %q, got %q", "failed to load OCI config", err.Error())
	}

	// assert the runtime-infra pointer was populated before the error, with
	// the OKE-typed value the function constructs
	if runtimeInfra == nil {
		t.Fatalf("expected runtimeInfra to be populated before LoadOCIConfig error")
	}
	okeInfra, ok := runtimeInfra.(*provider.KubernetesRuntimeInfraOKE)
	if !ok {
		t.Fatalf("expected *KubernetesRuntimeInfraOKE, got %T", runtimeInfra)
	}
	// assert the OKE-specific defaults and copied opts land on the struct
	if okeInfra.Region != "us-phoenix-1" {
		t.Errorf("Region: want %q, got %q", "us-phoenix-1", okeInfra.Region)
	}
	if okeInfra.WorkerNodeShape != "VM.Standard.A1.Flex" {
		t.Errorf("WorkerNodeShape: want VM.Standard.A1.Flex, got %q", okeInfra.WorkerNodeShape)
	}
	if okeInfra.WorkerNodeInitialCount != 2 {
		t.Errorf("WorkerNodeInitialCount: want 2, got %d", okeInfra.WorkerNodeInitialCount)
	}
	if okeInfra.ProjectName != "oke" {
		t.Errorf("ProjectName: want oke, got %q", okeInfra.ProjectName)
	}
	if okeInfra.RuntimeInstanceName != "threeport-tp-test" {
		t.Errorf("RuntimeInstanceName: want threeport-tp-test, got %q", okeInfra.RuntimeInstanceName)
	}

	// assert the uninstaller captured the same infra pointer for later teardown
	if uninstaller.kubernetesRuntimeInfra != okeInfra {
		t.Errorf("uninstaller.kubernetesRuntimeInfra: want same pointer as runtimeInfra, got %p vs %p",
			uninstaller.kubernetesRuntimeInfra, okeInfra)
	}
}

// TestDeployOkeInfraUsesClusterNameInControlPlaneOnlyMode covers the
// ControlPlaneOnly branch of runtimeInstanceName: the OKE infra picks up the
// caller-supplied cluster name verbatim, not the derived threeport-* form.
func TestDeployOkeInfraUsesClusterNameInControlPlaneOnlyMode(t *testing.T) {
	// force LoadOCIConfig failure through an empty HOME so we exit before
	// hitting any real infra path
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	homedir.Reset()
	t.Cleanup(homedir.Reset)

	cpi := &threeport.ControlPlaneInstaller{
		Opts: threeport.Options{
			OciRegion:        "us-ashburn-1",
			OciConfigProfile: "DEFAULT",
			ControlPlaneName: "ignored",
			ControlPlaneOnly: true,
			ClusterName:      "existing-cluster",
		},
	}
	cpConfig := &ControlPlane{Name: "ignored"}
	tpConfig := &ThreeportConfig{}
	var runtimeInfra provider.KubernetesRuntimeInfra
	kubeInfo := &kube.KubeConnectionInfo{}
	teardown := false
	cleanCfg := false
	uninstaller := &Uninstaller{teardownOnFailure: &teardown, cleanConfig: &cleanCfg}

	// invoke deploy; error is expected, but the interesting assertion is the
	// pre-error RuntimeInstanceName assignment
	if err := DeployOkeInfra(cpi, cpConfig, tpConfig, &runtimeInfra, kubeInfo, uninstaller); err == nil {
		t.Fatalf("expected LoadOCIConfig failure, got nil")
	}

	// assert the runtime-instance name honored the ControlPlaneOnly path
	okeInfra := runtimeInfra.(*provider.KubernetesRuntimeInfraOKE)
	if okeInfra.RuntimeInstanceName != "existing-cluster" {
		t.Errorf("RuntimeInstanceName: want existing-cluster, got %q", okeInfra.RuntimeInstanceName)
	}
}

// TestConfigureControlPlaneWithOkeConfigTenancyOCIDFailureWrapsError covers
// the earliest error path: a ConfigProvider whose TenancyOCID() call fails.
// The returned error must wrap "failed to get tenancy OCID from config
// provider" and carry the underlying cause via errors.Is.
func TestConfigureControlPlaneWithOkeConfigTenancyOCIDFailureWrapsError(t *testing.T) {
	// craft a KubernetesRuntimeInfraOKE whose ConfigProvider fails on TenancyOCID()
	rootCause := errors.New("provider offline")
	okeInfra := &provider.KubernetesRuntimeInfraOKE{
		ConfigProvider: &stubConfigProvider{tenancyErr: rootCause},
	}
	var runtimeInfra provider.KubernetesRuntimeInfra = okeInfra

	cpi := &threeport.ControlPlaneInstaller{
		Opts: threeport.Options{ControlPlaneName: "tp"},
	}
	teardown := false
	cleanCfg := false
	uninstaller := &Uninstaller{teardownOnFailure: &teardown, cleanConfig: &cleanCfg}

	// invoke configure; no HTTP client work happens because we fail early
	err := ConfigureControlPlaneWithOkeConfig(
		cpi,
		uninstaller,
		nil,
		"",
		nil,
		nil,
		&runtimeInfra,
	)

	// assert the error wraps the tenancy framing and preserves the root cause
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to get tenancy OCID from config provider") {
		t.Errorf("expected wrapping frame, got %q", err.Error())
	}
	if !errors.Is(err, rootCause) {
		t.Errorf("expected errors.Is to resolve root cause, got %v", err)
	}
}
