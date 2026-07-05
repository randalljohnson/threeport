package v0

import (
	"strings"
	"testing"

	"github.com/threeport/threeport/internal/provider"
	kube "github.com/threeport/threeport/pkg/kube/v0"
	threeport "github.com/threeport/threeport/pkg/threeport-installer/v0"
)

// TestDeployKindInfraPortMappingErrors covers the port mapping parse branches:
// invalid split, non-numeric container port, and non-numeric host port each
// return an error and stop before any kubernetes side effects.
func TestDeployKindInfraPortMappingErrors(t *testing.T) {
	tests := []struct {
		name        string
		mapping     string
		errContains string
	}{
		{
			// bare port with no colon fails to split into container:host pair
			name:        "rejects mapping missing colon separator",
			mapping:     "8080",
			errContains: "failed to parse kind port forward",
		},
		{
			// three-part mapping fails split length check
			name:        "rejects mapping with too many colons",
			mapping:     "8080:9090:1010",
			errContains: "failed to parse kind port forward",
		},
		{
			// non-numeric container port trips strconv.ParseInt on left side
			name:        "rejects non-numeric container port",
			mapping:     "abc:9090",
			errContains: "failed to parse container port",
		},
		{
			// non-numeric host port trips strconv.ParseInt on right side
			name:        "rejects non-numeric host port",
			mapping:     "8080:xyz",
			errContains: "failed to parse host port",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// build minimal installer carrying just the bad port mapping
			cpi := &threeport.ControlPlaneInstaller{
				Opts: threeport.Options{
					KindPortMappings: []string{tc.mapping},
				},
			}

			// invoke DeployKindInfra with zero-value collaborators; failure
			// happens in the parse loop before they are dereferenced
			var infra provider.KubernetesRuntimeInfra
			var connInfo kube.KubeConnectionInfo
			err := DeployKindInfra(
				cpi,
				&ControlPlane{},
				&ThreeportConfig{},
				&infra,
				&connInfo,
				&Uninstaller{},
			)

			// assert error surfaced with the expected marker phrase
			if err == nil {
				t.Fatalf("expected error for mapping %q, got nil", tc.mapping)
			}
			if !strings.Contains(err.Error(), tc.errContains) {
				t.Fatalf("expected error containing %q, got %q", tc.errContains, err.Error())
			}

			// assert nothing was written to the output pointers since parse
			// failed before infra construction
			if infra != nil {
				t.Fatalf("expected kubernetesRuntimeInfra unset, got %#v", infra)
			}
			if connInfo.APIEndpoint != "" {
				t.Fatalf("expected kube connection info unset, got %+v", connInfo)
			}
		})
	}
}

// TestDeployKindInfraControlPlaneOnlyBadKubeconfig covers the ControlPlaneOnly
// branch: with no valid kubeconfig on disk, GetConnectionInfoFromKubeconfig
// fails and DeployKindInfra wraps the error.
func TestDeployKindInfraControlPlaneOnlyBadKubeconfig(t *testing.T) {
	// point kubeconfig at a path that cannot be read so the connection info
	// lookup fails deterministically
	cpi := &threeport.ControlPlaneInstaller{
		Opts: threeport.Options{
			ControlPlaneOnly: true,
			KubeconfigPath:   "/nonexistent/kubeconfig-for-test",
			ControlPlaneName: "test",
		},
	}

	// invoke the function; port mapping parsing is skipped (empty slice)
	// so control reaches the ControlPlaneOnly branch
	var infra provider.KubernetesRuntimeInfra
	var connInfo kube.KubeConnectionInfo
	err := DeployKindInfra(
		cpi,
		&ControlPlane{},
		&ThreeportConfig{},
		&infra,
		&connInfo,
		&Uninstaller{},
	)

	// assert the returned error wraps the connection info failure
	if err == nil {
		t.Fatalf("expected error for missing kubeconfig, got nil")
	}
	if !strings.Contains(err.Error(), "failed to get connection info for kind kubernetes runtime") {
		t.Fatalf("expected wrapped connection info error, got %q", err.Error())
	}

	// assert kubernetesRuntimeInfra was populated before the failing call,
	// confirming the function progressed past the parse step
	if infra == nil {
		t.Fatalf("expected kubernetesRuntimeInfra to be set before kubeconfig read")
	}
}

// TestDeployKindInfraNoPortMappings covers the empty-KindPortMappings path:
// the parse loop is a no-op, and control flows into the same ControlPlaneOnly
// branch verified above (proving the loop does not require a mapping).
func TestDeployKindInfraNoPortMappings(t *testing.T) {
	// installer with no port mappings and ControlPlaneOnly=true forces the
	// function to exercise the empty-loop path
	cpi := &threeport.ControlPlaneInstaller{
		Opts: threeport.Options{
			ControlPlaneOnly: true,
			KubeconfigPath:   "/nonexistent/kubeconfig-for-test-2",
		},
	}

	// call DeployKindInfra; expect an error only from the kubeconfig read
	var infra provider.KubernetesRuntimeInfra
	var connInfo kube.KubeConnectionInfo
	err := DeployKindInfra(
		cpi,
		&ControlPlane{},
		&ThreeportConfig{},
		&infra,
		&connInfo,
		&Uninstaller{},
	)

	// assert error is from the kubeconfig read, not the parse loop
	if err == nil {
		t.Fatalf("expected error for missing kubeconfig, got nil")
	}
	if strings.Contains(err.Error(), "failed to parse kind port forward") {
		t.Fatalf("empty mappings should skip parse loop, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "failed to get connection info") {
		t.Fatalf("expected connection info error, got %q", err.Error())
	}
}
