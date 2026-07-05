//go:build integration

package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	cli "github.com/threeport/threeport/pkg/cli/v0"
)

// defaultKindContext names the kind cluster the integration suite talks to.
const defaultKindContext = "kind-threeport-threeport-1"

// requireKube skips t if the KUBECONFIG-selected context is not reachable.
// Every integration test opens with a call to this so a laptop without the
// kind cluster spins the suite as SKIP rather than FAIL.
func requireKube(t *testing.T) *rest.Config {
	t.Helper()
	cfg, err := loadKubeRESTConfig()
	if err != nil {
		t.Skipf("skipping integration test: no kube access (%v)", err)
	}
	// hit /version so a stale kubeconfig with an unreachable server also
	// short-circuits into SKIP instead of hanging in a later request
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Skipf("skipping integration test: cannot build kube client (%v)", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := client.Discovery().ServerVersion(); err != nil {
		t.Skipf("skipping integration test: kube server unreachable (%v)", err)
	}
	_ = ctx
	return cfg
}

// loadKubeRESTConfig assembles a rest.Config from KUBECONFIG (or ~/.kube/config)
// pinned to defaultKindContext.
func loadKubeRESTConfig() (*rest.Config, error) {
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to resolve home dir: %w", err)
		}
		kubeconfig = filepath.Join(home, ".kube", "config")
	}
	if _, err := os.Stat(kubeconfig); err != nil {
		return nil, fmt.Errorf("kubeconfig not present at %s: %w", kubeconfig, err)
	}
	loader := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig}
	overrides := &clientcmd.ConfigOverrides{CurrentContext: defaultKindContext}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loader, overrides).ClientConfig()
}

// getKubeClient returns a typed Kubernetes client bound to the default kind
// context; use for typed API operations (create/list workloads, etc.).
func getKubeClient(t *testing.T) *kubernetes.Clientset {
	t.Helper()
	cfg := requireKube(t)
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("failed to build typed kube client: %v", err)
	}
	return client
}

// getDynamicKubeClient returns a dynamic Kubernetes client; use for
// GVR-driven polling in waitForResource().
func getDynamicKubeClient(t *testing.T) dynamic.Interface {
	t.Helper()
	cfg := requireKube(t)
	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("failed to build dynamic kube client: %v", err)
	}
	return client
}

// getAPIServerURL returns the Threeport API endpoint recorded in the local
// threeport config plus an HTTP client wired for it. Skips t if no config is
// found so the suite degrades gracefully when the CLI has never been run.
func getAPIServerURL(t *testing.T) (string, *http.Client) {
	t.Helper()
	cli.InitConfig(nil, "")
	cfg, _, err := cli.GetThreeportConfig("")
	if err != nil {
		t.Skipf("skipping integration test: no threeport config (%v)", err)
	}
	cpConfig, err := cfg.GetControlPlaneConfig(cfg.CurrentControlPlane)
	if err != nil {
		t.Skipf("skipping integration test: no current control plane (%v)", err)
	}
	client, err := cfg.GetHTTPClient(cfg.CurrentControlPlane)
	if err != nil {
		t.Skipf("skipping integration test: cannot build API HTTP client (%v)", err)
	}
	return cpConfig.APIServer, client
}

// getEncryptionKey returns the shared encryption key from the local threeport
// config; skips t if unavailable.
func getEncryptionKey(t *testing.T) string {
	t.Helper()
	cli.InitConfig(nil, "")
	cfg, _, err := cli.GetThreeportConfig("")
	if err != nil {
		t.Skipf("skipping integration test: no threeport config (%v)", err)
	}
	key, err := cfg.GetThreeportEncryptionKey(cfg.CurrentControlPlane)
	if err != nil {
		t.Skipf("skipping integration test: no encryption key (%v)", err)
	}
	return key
}

// waitForResource polls the check callback every interval until it returns
// nil or the deadline passes, and reports the last error via t.Fatalf on
// timeout.
func waitForResource(t *testing.T, timeout time.Duration, interval time.Duration, description string, check func() error) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		err := check()
		if err == nil {
			return
		}
		lastErr = err
		time.Sleep(interval)
	}
	if lastErr == nil {
		lastErr = errors.New("no error recorded")
	}
	t.Fatalf("timed out waiting for %s after %s: %v", description, timeout, lastErr)
}

// sxaCLIAvailable reports whether the sxa-built tptctl binary is on PATH; the
// RMS/RMI CRUD tests use it as a gate for shelling out to the module's CLI.
func sxaCLIAvailable() bool {
	if _, err := exec.LookPath("sxa"); err == nil {
		return true
	}
	if _, err := exec.LookPath("tptctl"); err == nil {
		return true
	}
	return false
}
