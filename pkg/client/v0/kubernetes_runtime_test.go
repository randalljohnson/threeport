package v0

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	v0 "github.com/threeport/threeport/pkg/api/v0"
)

// testApiServer returns a threeport API stand-in that answers every request
// with the supplied objects, along with the client and address to reach it at.
// The address carries no scheme because GetResponse prepends one itself.
func testApiServer(t *testing.T, data []apiserver_lib.Object) (*http.Client, string) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(apiserver_lib.Response{Data: data}); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	return &http.Client{}, strings.TrimPrefix(server.URL, "http://")
}

// TestGetThreeportControlPlaneKubernetesRuntimeInstanceRejectsUnusableResults
// asserts that a result set the caller cannot use produces an error rather
// than an index out of range. An empty set is what the API returns after the
// database is dropped and the bootstrap records have not been restored.
func TestGetThreeportControlPlaneKubernetesRuntimeInstanceRejectsUnusableResults(t *testing.T) {
	name := "threeport-dev-0"
	tests := []struct {
		name        string
		data        []apiserver_lib.Object
		errContains string
	}{
		{
			name:        "no host runtime returns an error",
			data:        []apiserver_lib.Object{},
			errContains: "no kubernetes runtime instance hosting the threeport control plane found",
		},
		{
			name: "multiple host runtimes return an error",
			data: []apiserver_lib.Object{
				v0.KubernetesRuntimeInstance{Instance: v0.Instance{Name: &name}},
				v0.KubernetesRuntimeInstance{Instance: v0.Instance{Name: &name}},
			},
			errContains: "multiple kubernetes runtime instances marked as threeport control plane host",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			apiClient, apiAddr := testApiServer(t, test.data)

			_, err := GetThreeportControlPlaneKubernetesRuntimeInstance(apiClient, apiAddr)
			if err == nil {
				t.Fatal("expected an error, got none")
			}
			if !strings.Contains(err.Error(), test.errContains) {
				t.Errorf("expected error containing %q, got %q", test.errContains, err.Error())
			}
		})
	}
}

// TestGetThreeportControlPlaneKubernetesRuntimeInstanceReturnsSingleResult
// asserts the guards leave the ordinary single-result case alone.
func TestGetThreeportControlPlaneKubernetesRuntimeInstanceReturnsSingleResult(t *testing.T) {
	name := "threeport-dev-0"
	apiClient, apiAddr := testApiServer(t, []apiserver_lib.Object{
		v0.KubernetesRuntimeInstance{Instance: v0.Instance{Name: &name}},
	})

	kubernetesRuntimeInstance, err := GetThreeportControlPlaneKubernetesRuntimeInstance(apiClient, apiAddr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if kubernetesRuntimeInstance.Name == nil || *kubernetesRuntimeInstance.Name != name {
		t.Errorf("expected kubernetes runtime instance %q, got %v", name, kubernetesRuntimeInstance.Name)
	}
}
