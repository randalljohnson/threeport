package v0

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"k8s.io/client-go/tools/clientcmd"
)

// KubeConnectionInfo contains the necessary info to connect to a Kubernetes
// API.
type KubeConnectionInfo struct {
	APIEndpoint     string
	CACertificate   string
	Certificate     string
	Key             string
	Token           string
	TokenExpiration time.Time
}

// DefaultKubeconfig returns the path to the user's default kubeconfig.
// $KUBECONFIG takes precedence over ~/.kube/config so wrappers that
// materialize a kubeconfig per-environment (direnv-driven dev setups,
// the 1Password wrapper, etc) flow through to threeport tooling
// without needing to pass --kubeconfig everywhere.
func DefaultKubeconfig() (string, error) {
	if env := os.Getenv("KUBECONFIG"); env != "" {
		return env, nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user's home directory: %w", err)
	}

	return filepath.Join(homeDir, ".kube", "config"), nil
}

// GetConnectionInfoFromKubeconfig extracts the Kubernetes API connection info
// from a kubeconfig.
func GetConnectionInfoFromKubeconfig(kubeconfig string) (*KubeConnectionInfo, error) {
	var kubeConnInfo KubeConnectionInfo

	// read kubeconfig
	kubeconfigContent, err := os.ReadFile(kubeconfig)
	if err != nil {
		return &kubeConnInfo, fmt.Errorf("failed to read kubeconfig file: %w", err)
	}

	// get kube client config
	clientConfig, err := clientcmd.NewClientConfigFromBytes(kubeconfigContent)
	if err != nil {
		return &kubeConnInfo, fmt.Errorf("failed to get client config from kubeconfig file: %w", err)
	}
	kubeConfig, err := clientConfig.RawConfig()

	// get cluster CA and server endpoint
	clusterFound := false
	for clusterName, cluster := range kubeConfig.Clusters {
		if clusterName == kubeConfig.CurrentContext {
			kubeConnInfo.CACertificate = string(cluster.CertificateAuthorityData)
			kubeConnInfo.APIEndpoint = string(cluster.Server)
			clusterFound = true
		}
	}
	if !clusterFound {
		return &kubeConnInfo, fmt.Errorf(
			"failed to get Kubernetes cluster CA and endpoint: %w",
			errors.New("cluster config not found in kubeconfig"),
		)
	}

	// get client certificate and key
	userFound := false
	for userName, user := range kubeConfig.AuthInfos {
		if userName == kubeConfig.CurrentContext {
			kubeConnInfo.Certificate = string(user.ClientCertificateData)
			kubeConnInfo.Key = string(user.ClientKeyData)
			userFound = true
		}
	}
	if !userFound {
		return &kubeConnInfo, fmt.Errorf(
			"failed to get user credentials to Kubernetes cluster: %w",
			errors.New("kubeconfig user for threeport cluster not found"),
		)
	}

	return &kubeConnInfo, nil
}
