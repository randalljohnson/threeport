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

// mockRuntimeAPI is an httptest handler that serves runtime instance and definition lists.
// pathHits records each URL path so list Get can be checked for extra GETs.
type mockRuntimeAPI struct {
	t           *testing.T
	instances   []api_v0.KubernetesRuntimeInstance
	definitions []api_v0.KubernetesRuntimeDefinition

	mu       sync.Mutex
	pathHits map[string]int
}

// handler counts path hits and serves list envelopes.
func (m *mockRuntimeAPI) handler(w http.ResponseWriter, r *http.Request) {
	// count request path
	m.mu.Lock()
	if m.pathHits == nil {
		m.pathHits = map[string]int{}
	}
	m.pathHits[r.URL.Path]++
	m.mu.Unlock()

	// serve instance or definition list
	switch {
	case r.Method == http.MethodGet && r.URL.Path == api_v0.PathKubernetesRuntimeInstances:
		writeRuntimeEnvelope(m.t, w, instancesToObjects(m.instances))
	case r.Method == http.MethodGet && r.URL.Path == api_v0.PathKubernetesRuntimeDefinitions:
		writeRuntimeEnvelope(m.t, w, definitionsToObjects(m.definitions))
	default:
		http.Error(w, "unexpected request: "+r.Method+" "+r.URL.Path, http.StatusNotFound)
	}
}

// writeRuntimeEnvelope writes a one-page API list response.
func writeRuntimeEnvelope(t *testing.T, w http.ResponseWriter, data []apiserver_lib.Object) {
	t.Helper()
	// marshal envelope with HasMore false so the list client stops
	body, err := json.Marshal(apiserver_lib.Response{
		Meta: apiserver_lib.Meta{Pagination: apiserver_lib.Pagination{HasMore: false}},
		Data: data,
	})
	require.NoError(t, err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// instancesToObjects copies instances into an Object slice.
func instancesToObjects(items []api_v0.KubernetesRuntimeInstance) []apiserver_lib.Object {
	// wrap instances as Object slice
	out := make([]apiserver_lib.Object, len(items))
	for i := range items {
		out[i] = items[i]
	}
	return out
}

// definitionsToObjects copies definitions into an Object slice.
func definitionsToObjects(items []api_v0.KubernetesRuntimeDefinition) []apiserver_lib.Object {
	// wrap definitions as Object slice
	out := make([]apiserver_lib.Object, len(items))
	for i := range items {
		out[i] = items[i]
	}
	return out
}

// TestKubernetesRuntimeInstanceConfigGetListPathBatchesDefinitions covers list Get prefetching definitions once.
func TestKubernetesRuntimeInstanceConfigGetListPathBatchesDefinitions(t *testing.T) {
	// seed two definitions shared across three instances
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

	// serve mock API
	mock := &mockRuntimeAPI{
		t:           t,
		instances:   instances,
		definitions: []api_v0.KubernetesRuntimeDefinition{defA, defB},
	}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer srv.Close()

	// strip httptest scheme; GetResponse prepends http://
	endpoint := strings.TrimPrefix(srv.URL, "http://")

	// get unnamed list
	cfg := &KubernetesRuntimeInstanceConfig{}
	got, err := cfg.Get(&http.Client{}, endpoint)
	require.NoError(t, err)

	// copy path hits
	mock.mu.Lock()
	pathHits := make(map[string]int, len(mock.pathHits))
	for k, v := range mock.pathHits {
		pathHits[k] = v
	}
	mock.mu.Unlock()

	// assert one list GET each and no per-id GET
	assert.Equal(t, 1, pathHits[api_v0.PathKubernetesRuntimeInstances], "instances list should be fetched exactly once")
	assert.Equal(t, 1, pathHits[api_v0.PathKubernetesRuntimeDefinitions], "definitions list should be prefetched exactly once")
	perRowPrefix := api_v0.PathKubernetesRuntimeDefinitions + "/"
	for path := range pathHits {
		assert.False(t, strings.HasPrefix(path, perRowPrefix), "no per-row GET %s{id} on the list path, got %s", perRowPrefix, path)
	}

	// assert definition names resolved in instance order
	require.NotNil(t, got)
	require.Len(t, *got, 3)
	for i, wantName := range []string{"def-a", "def-b", "def-a"} {
		def := (*got)[i].KubernetesRuntimeInstance.KubernetesRuntimeDefinition
		require.NotNilf(t, def, "instance %d missing resolved definition", i)
		require.NotNilf(t, def.Name, "instance %d definition name unset", i)
		assert.Equal(t, wantName, *def.Name)
	}
}
