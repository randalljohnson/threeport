package tptdev

import (
	"strings"
	"testing"
)

// pointDockerAtDeadSocket forces the docker client's FromEnv option to build a
// client against an unreachable endpoint, so API calls return connection errors
// without touching any real docker state on the host running the tests.
func pointDockerAtDeadSocket(t *testing.T) {
	t.Helper()
	// unset TLS and API version overrides that could change error shape
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")
	t.Setenv("DOCKER_API_VERSION", "")
	// point at a unix socket path that will not exist so the first API call
	// fails cleanly with a connection error
	t.Setenv("DOCKER_HOST", "unix:///nonexistent/tptdev-registry-test.sock")
}

// TestCreateLocalRegistry_ReturnsWrappedErrorWhenDockerUnreachable asserts
// CreateLocalRegistry() wraps its docker-side failure with the expected prefix
// when the docker daemon cannot be reached.
func TestCreateLocalRegistry_ReturnsWrappedErrorWhenDockerUnreachable(t *testing.T) {
	// arrange a docker client that cannot reach any daemon
	pointDockerAtDeadSocket(t)

	// invoke the exported entry point
	err := CreateLocalRegistry()

	// verify an error surfaced and carries one of the documented wrap prefixes
	if err == nil {
		t.Fatalf("expected error from CreateLocalRegistry with unreachable docker, got nil")
	}
	msg := err.Error()
	// with a dead socket, ContainerInspect fails (registry "does not exist"
	// from the client's perspective), ImageInspectWithRaw fails, then
	// ImagePull is attempted and fails - so the surfaced wrap is the pull
	// failure. Accept either the pull-wrap or the create-wrap in case the
	// error path shifts on different docker client versions.
	if !strings.Contains(msg, "failed to pull registry image") &&
		!strings.Contains(msg, "failed to create registry container") &&
		!strings.Contains(msg, "failed to create Docker client") {
		t.Fatalf("expected a wrapped registry-setup error, got %q", msg)
	}
}

// TestDeleteLocalRegistry_ReturnsWrappedErrorWhenDockerUnreachable asserts
// DeleteLocalRegistry() wraps its stop-side failure when the daemon is
// unreachable.
func TestDeleteLocalRegistry_ReturnsWrappedErrorWhenDockerUnreachable(t *testing.T) {
	// arrange an unreachable docker daemon
	pointDockerAtDeadSocket(t)

	// invoke the exported delete entry point
	err := DeleteLocalRegistry()

	// verify the stop step surfaces its wrapped error
	if err == nil {
		t.Fatalf("expected error from DeleteLocalRegistry with unreachable docker, got nil")
	}
	msg := err.Error()
	// the first API call is ContainerStop; accept the remove-wrap too in
	// case a client version treats a missing container as a soft-stop
	if !strings.Contains(msg, "failed to stop registry docker container") &&
		!strings.Contains(msg, "failed to remove registry docker container") &&
		!strings.Contains(msg, "failed to create Docker client") {
		t.Fatalf("expected a wrapped registry-teardown error, got %q", msg)
	}
}

// TestConnectLocalRegistry_ReturnsWrappedErrorWhenDockerUnreachable asserts
// ConnectLocalRegistry() surfaces a wrapped error when the docker daemon
// underneath kind cannot be reached; nothing on the host should be mutated.
func TestConnectLocalRegistry_ReturnsWrappedErrorWhenDockerUnreachable(t *testing.T) {
	// arrange an unreachable docker daemon
	pointDockerAtDeadSocket(t)

	// invoke with a cluster name that will not resolve
	err := ConnectLocalRegistry("tptdev-registry-test-nonexistent-cluster")

	// verify an error surfaced with a documented wrap prefix
	if err == nil {
		t.Fatalf("expected error from ConnectLocalRegistry with unreachable docker, got nil")
	}
	msg := err.Error()
	// kind's ListNodes shells out to `docker ps`; with DOCKER_HOST pointed
	// at a dead socket, that call fails and the function wraps it. Other
	// wraps are possible on client-version drift, so accept the set of
	// documented prefixes this function can emit before it reaches the
	// kubernetes-config step.
	acceptable := []string{
		"failed to create Docker client",
		"failed to list nodes for kind cluster",
		"failed to make directory in kind node container",
		"failed to configure local registry networking in kind cluster node",
		"failed to inspect kind node container to configure docker network",
		"failed to configure docker network to connect registry to kind cluster",
		"failed to generate Kubernetes REST config from kubeconfig",
		"failed to create new clientset for Kubernetes",
		"failed to create configmap for local registry",
	}
	matched := false
	for _, prefix := range acceptable {
		if strings.Contains(msg, prefix) {
			matched = true
			break
		}
	}
	if !matched {
		t.Fatalf("expected one of the documented wrapped errors, got %q", msg)
	}
}
