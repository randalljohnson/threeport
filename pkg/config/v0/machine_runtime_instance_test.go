package v0

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	api_v0 "github.com/threeport/threeport/pkg/api/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestMachineRuntimeInstanceGetDedupesDefinitionLookups covers the client-side
// fix for the definition-fetch N+1 in `tptctl get machine-runtime-instances`:
// three instances that share the same MachineRuntimeDefinitionID must trigger
// exactly one GET /v0/machine-runtime-definitions/{id}, not one per instance.
// A distinct definition ID on another instance still adds one call, so a set
// of three sharing one definition plus one distinct definition totals two
// definition GETs.
func TestMachineRuntimeInstanceGetDedupesDefinitionLookups(t *testing.T) {
	// track per-ID definition fetch counts so we can assert on dedupe behavior
	var (
		mu           sync.Mutex
		defHitsByID  = map[uint]int{}
	)

	sharedDefName := util.Ptr("shared-def")
	otherDefName := util.Ptr("other-def")

	// build a mock threeport API that returns three instances sharing one
	// definition plus a fourth pointing at a different definition
	instances := []api_v0.MachineRuntimeInstance{
		makeInstance(1, "mri-a", 100),
		makeInstance(2, "mri-b", 100),
		makeInstance(3, "mri-c", 100),
		makeInstance(4, "mri-d", 200),
	}

	// list requests are counted to verify only one list call happens
	var listHits int32

	mux := http.NewServeMux()
	mux.HandleFunc("/v0/machine-runtime-instances", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&listHits, 1)
		data := make([]apiserver_lib.Object, 0, len(instances))
		for i := range instances {
			data = append(data, instances[i])
		}
		writeResponse(t, w, data)
	})
	mux.HandleFunc("/v0/machine-runtime-definitions/", func(w http.ResponseWriter, r *http.Request) {
		// path is /v0/machine-runtime-definitions/{id}
		idStr := strings.TrimPrefix(r.URL.Path, "/v0/machine-runtime-definitions/")
		var id uint
		if _, err := fmt.Sscan(idStr, &id); err != nil {
			http.Error(w, "bad id", http.StatusBadRequest)
			return
		}
		mu.Lock()
		defHitsByID[id]++
		mu.Unlock()

		var name *string
		switch id {
		case 100:
			name = sharedDefName
		case 200:
			name = otherDefName
		default:
			http.Error(w, "unknown id", http.StatusNotFound)
			return
		}
		def := api_v0.MachineRuntimeDefinition{
			Common:     api_v0.Common{ID: util.Ptr(id)},
			Definition: api_v0.Definition{Name: name},
		}
		writeResponse(t, w, []apiserver_lib.Object{def})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// pkg/client/lib/v0 prepends "http://" when the transport is a stock
	// http.Transport with no TLS config, so pass the host:port without scheme
	apiEndpoint := strings.TrimPrefix(srv.URL, "http://")

	// exercise the Get code path that used to fan out one definition GET per
	// instance
	cfg := &MachineRuntimeInstanceConfig{}
	got, err := cfg.Get(srv.Client(), apiEndpoint, "")
	require.NoError(t, err)
	require.NotNil(t, got)

	// assert one config per instance came back with the expected definition
	// name populated from the cache
	assert.Len(t, *got, 4)
	assert.Equal(t, sharedDefName, (*got)[0].MachineRuntimeInstance.MachineRuntimeDefinition.Name)
	assert.Equal(t, sharedDefName, (*got)[1].MachineRuntimeInstance.MachineRuntimeDefinition.Name)
	assert.Equal(t, sharedDefName, (*got)[2].MachineRuntimeInstance.MachineRuntimeDefinition.Name)
	assert.Equal(t, otherDefName, (*got)[3].MachineRuntimeInstance.MachineRuntimeDefinition.Name)

	// core assertion: each unique definition ID is fetched exactly once, not
	// once per instance sharing it
	assert.Equal(t, int32(1), atomic.LoadInt32(&listHits), "list endpoint should be hit once")
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, defHitsByID[100], "shared definition id 100 should be fetched exactly once, not once per instance")
	assert.Equal(t, 1, defHitsByID[200], "distinct definition id 200 should be fetched exactly once")
}

// makeInstance builds a minimal MachineRuntimeInstance sufficient for the
// enrichment loop under test.
func makeInstance(id uint, name string, defID uint) api_v0.MachineRuntimeInstance {
	// GetAgeFormatted panics on a nil CreatedAt, so give every fixture a
	// concrete timestamp
	createdAt := time.Now().Add(-time.Hour)
	return api_v0.MachineRuntimeInstance{
		Common:                     api_v0.Common{ID: util.Ptr(id), CreatedAt: &createdAt},
		Instance:                   api_v0.Instance{Name: util.Ptr(name)},
		Hostname:                   util.Ptr("host.example"),
		SSHUser:                    util.Ptr("root"),
		MachineRuntimeDefinitionID: util.Ptr(defID),
	}
}

// writeResponse encodes the given objects as an apiserver_lib.Response body,
// matching what pkg/client/lib/v0.GetResponse expects.
func writeResponse(t *testing.T, w http.ResponseWriter, data []apiserver_lib.Object) {
	t.Helper()
	resp := apiserver_lib.Response{Data: data}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	require.NoError(t, json.NewEncoder(w).Encode(resp))
}
