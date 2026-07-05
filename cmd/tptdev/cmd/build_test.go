package cmd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	installer "github.com/threeport/threeport/pkg/threeport-installer/v0"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

// TestImageBuildTarget covers imageBuildTarget()'s routing of component names
// to Dockerfile build targets: terraform to release-terraform, oci and gcp to
// release-pulumi, everything else to the distroless release target.
func TestImageBuildTarget(t *testing.T) {
	tests := []struct {
		name          string
		componentName string
		want          string
	}{
		{
			// terraform-controller needs the terraform CLI at runtime
			name:          "terraform controller routes to release-terraform",
			componentName: installer.ThreeportTerraformControllerName,
			want:          "release-terraform",
		},
		{
			// oci-controller needs pulumi CLI at runtime
			name:          "oci controller routes to release-pulumi",
			componentName: installer.ThreeportOciControllerName,
			want:          "release-pulumi",
		},
		{
			// gcp-controller also needs pulumi CLI at runtime
			name:          "gcp controller routes to release-pulumi",
			componentName: installer.ThreeportGcpControllerName,
			want:          "release-pulumi",
		},
		{
			// default falls through to the distroless release target
			name:          "rest-api falls through to release",
			componentName: "rest-api",
			want:          "release",
		},
		{
			// unknown/empty component name still yields the default target
			name:          "empty name falls through to release",
			componentName: "",
			want:          "release",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// action under test: route componentName to Dockerfile target
			got := imageBuildTarget(tt.componentName)
			// assert selection matches the target documented for each family
			if got != tt.want {
				t.Errorf("imageBuildTarget(%q) = %q, want %q", tt.componentName, got, tt.want)
			}
		})
	}
}

// TestComponentMainPath covers componentMainPath()'s split between the
// hand-written agent main and the generated main_gen.go for every other
// component.
func TestComponentMainPath(t *testing.T) {
	tests := []struct {
		name          string
		componentName string
		want          string
	}{
		{
			// agent is hand-written, so its main lives at main.go
			name:          "agent uses hand-written main.go",
			componentName: "agent",
			want:          "cmd/agent/main.go",
		},
		{
			// non-agent components are code-generated, so their main is main_gen.go
			name:          "rest-api uses generated main_gen.go",
			componentName: "rest-api",
			want:          "cmd/rest-api/main_gen.go",
		},
		{
			// controllers follow the generated main_gen.go convention
			name:          "controller uses generated main_gen.go",
			componentName: "kubernetes-workload-controller",
			want:          "cmd/kubernetes-workload-controller/main_gen.go",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// action under test: derive path to a component's main file
			got := componentMainPath(tt.componentName)
			// assert path matches the agent/other split
			if got != tt.want {
				t.Errorf("componentMainPath(%q) = %q, want %q", tt.componentName, got, tt.want)
			}
		})
	}
}

// chdirTemp switches into a fresh temp directory for the duration of the test
// and restores the original working directory afterward. Isolates gitBranchName
// tests from any real .git/HEAD in the enclosing tree.
func chdirTemp(t *testing.T) string {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	// use a nested subdirectory so the parent chain search has room to walk
	root := t.TempDir()
	sub := filepath.Join(root, "sub", "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("failed to create nested temp dir: %v", err)
	}
	if err := os.Chdir(sub); err != nil {
		t.Fatalf("failed to chdir to temp dir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(orig); err != nil {
			t.Errorf("failed to restore working directory: %v", err)
		}
	})
	return root
}

// TestGitBranchNameReadsHeadRef covers gitBranchName()'s happy path: reads a
// standard "ref: refs/heads/<branch>" HEAD file and returns the branch name.
func TestGitBranchNameReadsHeadRef(t *testing.T) {
	// setup: fake repo root with a HEAD pointing at a branch
	root := chdirTemp(t)
	gitDir := filepath.Join(root, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatalf("failed to create fake .git dir: %v", err)
	}
	headPath := filepath.Join(gitDir, "HEAD")
	if err := os.WriteFile(headPath, []byte("ref: refs/heads/feat-example\n"), 0o644); err != nil {
		t.Fatalf("failed to write HEAD file: %v", err)
	}

	// action under test: walk up from cwd, read HEAD, strip prefix
	got, err := gitBranchName()
	// assert: no error and branch name extracted
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "feat-example" {
		t.Errorf("gitBranchName() = %q, want %q", got, "feat-example")
	}
}

// TestGitBranchNameDetached covers gitBranchName()'s error path when HEAD holds
// a raw commit SHA (detached HEAD) instead of a ref line.
func TestGitBranchNameDetached(t *testing.T) {
	// setup: fake repo root whose HEAD file is a detached SHA
	root := chdirTemp(t)
	gitDir := filepath.Join(root, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatalf("failed to create fake .git dir: %v", err)
	}
	headPath := filepath.Join(gitDir, "HEAD")
	if err := os.WriteFile(headPath, []byte("abc123def456\n"), 0o644); err != nil {
		t.Fatalf("failed to write detached HEAD file: %v", err)
	}

	// action under test: HEAD lacks the ref prefix
	got, err := gitBranchName()
	// assert: empty return and an error mentioning detached/unexpected format
	if err == nil {
		t.Fatalf("expected error for detached HEAD, got branch %q", got)
	}
	if got != "" {
		t.Errorf("gitBranchName() = %q, want empty string on error", got)
	}
}

// TestGitBranchNameNoRepo covers gitBranchName()'s error path when no ancestor
// directory contains a .git folder.
func TestGitBranchNameNoRepo(t *testing.T) {
	// setup: temp dir without any .git ancestor
	chdirTemp(t)

	// action under test: walk up finds no HEAD before the filesystem root
	got, err := gitBranchName()
	// assert: empty return and a "not inside a git repository" style error
	if err == nil {
		t.Fatalf("expected error when not inside a git repo, got branch %q", got)
	}
	if got != "" {
		t.Errorf("gitBranchName() = %q, want empty string on error", got)
	}
}

// newDynamicClientForTest wires a dynamic.DynamicClient to a fake API server
// URL. The returned client makes real HTTP calls, which lets tests exercise
// the concrete *dynamic.DynamicClient parameter that detectAuthEnabled takes.
func newDynamicClientForTest(t *testing.T, serverURL string) *dynamic.DynamicClient {
	t.Helper()
	client, err := dynamic.NewForConfig(&rest.Config{Host: serverURL})
	if err != nil {
		t.Fatalf("failed to build dynamic client for test: %v", err)
	}
	return client
}

// deploymentJSON returns a minimal apps/v1 Deployment body the fake server can
// serve. args populates the first container's args slice; when nil, the args
// key is omitted so the not-found branch is exercised.
func deploymentJSON(args []string) string {
	argsField := ""
	if args != nil {
		quoted := make([]string, 0, len(args))
		for _, a := range args {
			quoted = append(quoted, `"`+a+`"`)
		}
		argsField = `,"args":[` + strings.Join(quoted, ",") + `]`
	}
	return `{
		"apiVersion":"apps/v1","kind":"Deployment",
		"metadata":{"name":"threeport-api-server","namespace":"threeport-control-plane"},
		"spec":{"template":{"spec":{"containers":[{"name":"rest-api"` + argsField + `}]}}}
	}`
}

// TestDetectAuthEnabledDefaultsTrueOnGetError covers the safe-default branch:
// when the API server returns an error for the deployment fetch, the function
// returns true so callers do not accidentally drop auth on a healthy cluster.
func TestDetectAuthEnabledDefaultsTrueOnGetError(t *testing.T) {
	// setup: server always returns 500 so the Get() call errors
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	client := newDynamicClientForTest(t, srv.URL)

	// action under test: fetch fails, function must fall through to the safe default
	got := detectAuthEnabled(client)

	// assert: safe-default true so a partial failure never silently disables auth
	if !got {
		t.Fatalf("detectAuthEnabled() = false on Get error, want true (safe default)")
	}
}

// TestDetectAuthEnabledFalseWhenFlagPresent covers the disable path: when the
// deployment's first container args include -auth-enabled=false the function
// returns false, letting the debug/build restart path skip auth wiring.
func TestDetectAuthEnabledFalseWhenFlagPresent(t *testing.T) {
	// setup: server returns a deployment whose args include the disable flag
	body := deploymentJSON([]string{"-auth-enabled=false", "-log-level=info"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	client := newDynamicClientForTest(t, srv.URL)

	// action under test: parse args, match on -auth-enabled=false
	got := detectAuthEnabled(client)

	// assert: false so the caller mirrors the running deployment's disabled auth
	if got {
		t.Fatalf("detectAuthEnabled() = true, want false when -auth-enabled=false is in args")
	}
}

// TestDetectAuthEnabledTrueWhenFlagAbsent covers the normal-args path: when
// the deployment's args do not include the disable flag, the function returns
// true, matching the default enabled-auth deployment shape.
func TestDetectAuthEnabledTrueWhenFlagAbsent(t *testing.T) {
	// setup: server returns a deployment with args that do not include the disable flag
	body := deploymentJSON([]string{"-log-level=info", "-metrics=true"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	client := newDynamicClientForTest(t, srv.URL)

	// action under test: iterate args, no disable flag present
	got := detectAuthEnabled(client)

	// assert: true so the caller reflects the default enabled-auth deployment
	if !got {
		t.Fatalf("detectAuthEnabled() = false, want true when disable flag is absent")
	}
}

// TestDetectAuthEnabledTrueWhenArgsMissing covers the missing-args branch: a
// deployment without any args field on its first container still returns true,
// since detectAuthEnabled cannot prove auth is off and defaults to safe.
func TestDetectAuthEnabledTrueWhenArgsMissing(t *testing.T) {
	// setup: server returns a deployment whose container has no args field
	body := deploymentJSON(nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	client := newDynamicClientForTest(t, srv.URL)

	// action under test: unstructured nested lookup for args returns found=false
	got := detectAuthEnabled(client)

	// assert: true so the safe-default kicks in when args cannot be inspected
	if !got {
		t.Fatalf("detectAuthEnabled() = false, want true when args field is absent")
	}
}

// TestDetectAuthEnabledTrueWhenContainersEmpty covers the empty-containers
// branch: a deployment whose template lists no containers still returns true,
// since detectAuthEnabled has nothing to inspect and must not disable auth.
func TestDetectAuthEnabledTrueWhenContainersEmpty(t *testing.T) {
	// setup: server returns a deployment with an empty containers list
	body := `{
		"apiVersion":"apps/v1","kind":"Deployment",
		"metadata":{"name":"threeport-api-server","namespace":"threeport-control-plane"},
		"spec":{"template":{"spec":{"containers":[]}}}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	client := newDynamicClientForTest(t, srv.URL)

	// action under test: nested containers slice is empty, len==0 branch fires
	got := detectAuthEnabled(client)

	// assert: true so an empty container list falls through to the safe default
	if !got {
		t.Fatalf("detectAuthEnabled() = false, want true when containers list is empty")
	}
}
