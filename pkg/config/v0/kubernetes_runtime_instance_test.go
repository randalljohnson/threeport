package v0

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	api_v0 "github.com/threeport/threeport/pkg/api/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// mockRuntimeAPI is an httptest fake that serves the list endpoints
// needed by KubernetesRuntimeInstanceConfig.Get() and counts inbound
// hits per path so the caller can assert on request shape.
type mockRuntimeAPI struct {
	t           *testing.T
	instances   []api_v0.KubernetesRuntimeInstance
	definitions []api_v0.KubernetesRuntimeDefinition

	mu       sync.Mutex
	pathHits map[string]int
}

func (m *mockRuntimeAPI) handler(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	if m.pathHits == nil {
		m.pathHits = map[string]int{}
	}
	m.pathHits[r.URL.Path]++
	m.mu.Unlock()

	switch {
	case r.Method == http.MethodGet && r.URL.Path == api_v0.PathKubernetesRuntimeInstances:
		writeRuntimeEnvelope(m.t, w, instancesToObjects(m.instances))
	case r.Method == http.MethodGet && r.URL.Path == api_v0.PathKubernetesRuntimeDefinitions:
		writeRuntimeEnvelope(m.t, w, definitionsToObjects(m.definitions))
	default:
		http.Error(w, "unexpected request: "+r.Method+" "+r.URL.Path, http.StatusNotFound)
	}
}

// writeRuntimeEnvelope wraps data in the apiserver_lib.Response envelope
// that client_lib.GetResponse expects, with HasMore false so the paginated
// list client stops after one page.
func writeRuntimeEnvelope(t *testing.T, w http.ResponseWriter, data []apiserver_lib.Object) {
	t.Helper()
	body, err := json.Marshal(apiserver_lib.Response{
		Meta: apiserver_lib.Meta{Pagination: apiserver_lib.Pagination{HasMore: false}},
		Data: data,
	})
	require.NoError(t, err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func instancesToObjects(items []api_v0.KubernetesRuntimeInstance) []apiserver_lib.Object {
	out := make([]apiserver_lib.Object, len(items))
	for i := range items {
		out[i] = items[i]
	}
	return out
}

func definitionsToObjects(items []api_v0.KubernetesRuntimeDefinition) []apiserver_lib.Object {
	out := make([]apiserver_lib.Object, len(items))
	for i := range items {
		out[i] = items[i]
	}
	return out
}

// TestKubernetesRuntimeInstanceConfigGetListPathBatchesDefinitions covers the
// list path of KubernetesRuntimeInstanceConfig.Get(): the caller must resolve
// every instance's definition name via a single prefetched list instead of a
// per-row GET /v0/kubernetes-runtime-definitions/{id}, and the resolved names
// must appear in the returned configs.
func TestKubernetesRuntimeInstanceConfigGetListPathBatchesDefinitions(t *testing.T) {
	// three instances that all reference definitions in a single list response,
	// so the pre-fix per-row lookup would land three separate GETs by ID
	defA := api_v0.KubernetesRuntimeDefinition{
		Common:     api_v0.Common{ID: util.Ptr(uint(1))},
		Definition: api_v0.Definition{Name: util.Ptr("def-a")},
	}
	defB := api_v0.KubernetesRuntimeDefinition{
		Common:     api_v0.Common{ID: util.Ptr(uint(2))},
		Definition: api_v0.Definition{Name: util.Ptr("def-b")},
	}
	now := time.Now()
	instances := []api_v0.KubernetesRuntimeInstance{
		{
			Common:                        api_v0.Common{ID: util.Ptr(uint(10)), CreatedAt: &now},
			Instance:                      api_v0.Instance{Name: util.Ptr("inst-1")},
			Location:                      util.Ptr("loc-1"),
			DefaultRuntime:                util.Ptr(true),
			KubernetesRuntimeDefinitionID: util.Ptr(uint(1)),
		},
		{
			Common:                        api_v0.Common{ID: util.Ptr(uint(11)), CreatedAt: &now},
			Instance:                      api_v0.Instance{Name: util.Ptr("inst-2")},
			Location:                      util.Ptr("loc-2"),
			DefaultRuntime:                util.Ptr(false),
			KubernetesRuntimeDefinitionID: util.Ptr(uint(2)),
		},
		{
			Common:                        api_v0.Common{ID: util.Ptr(uint(12)), CreatedAt: &now},
			Instance:                      api_v0.Instance{Name: util.Ptr("inst-3")},
			Location:                      util.Ptr("loc-3"),
			DefaultRuntime:                util.Ptr(false),
			KubernetesRuntimeDefinitionID: util.Ptr(uint(1)),
		},
	}

	// stand up the fake API and drive the list path via Get() with no name set
	mock := &mockRuntimeAPI{
		t:           t,
		instances:   instances,
		definitions: []api_v0.KubernetesRuntimeDefinition{defA, defB},
	}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer srv.Close()

	// client_lib.GetResponse prepends "http://" to the configured endpoint,
	// so strip the httptest scheme to avoid a doubled prefix
	endpoint := strings.TrimPrefix(srv.URL, "http://")
	cfg := &KubernetesRuntimeInstanceConfig{}
	got, err := cfg.Get(&http.Client{}, endpoint)
	require.NoError(t, err)

	// rejects the N+1 shape: the list definitions endpoint is hit once for the
	// batch prefetch, and no per-row GET /v0/kubernetes-runtime-definitions/{id}
	// lands on the fake API
	mock.mu.Lock()
	pathHits := make(map[string]int, len(mock.pathHits))
	for k, v := range mock.pathHits {
		pathHits[k] = v
	}
	mock.mu.Unlock()
	assert.Equal(t, 1, pathHits[api_v0.PathKubernetesRuntimeInstances], "instances list should be fetched exactly once")
	assert.Equal(t, 1, pathHits[api_v0.PathKubernetesRuntimeDefinitions], "definitions list should be prefetched exactly once")
	perRowPrefix := api_v0.PathKubernetesRuntimeDefinitions + "/"
	for path := range pathHits {
		assert.False(t, strings.HasPrefix(path, perRowPrefix), "no per-row GET %s{id} on the list path, got %s", perRowPrefix, path)
	}

	// asserts every instance appears in output with its definition name resolved
	// through the prefetched map
	require.NotNil(t, got)
	require.Len(t, *got, 3)
	for i, wantName := range []string{"def-a", "def-b", "def-a"} {
		def := (*got)[i].KubernetesRuntimeInstance.KubernetesRuntimeDefinition
		require.NotNilf(t, def, "instance %d missing resolved definition", i)
		require.NotNilf(t, def.Name, "instance %d definition name unset", i)
		assert.Equal(t, wantName, *def.Name)
	}
}
