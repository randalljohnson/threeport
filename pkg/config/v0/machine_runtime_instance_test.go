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

// TestMachineRuntimeInstanceGetDedupesDefinitionLookups covers Get listing
// instances that share a definition ID without repeating that ID fetch.
func TestMachineRuntimeInstanceGetDedupesDefinitionLookups(t *testing.T) {
	var (
		mu          sync.Mutex
		defHitsByID = map[uint]int{}
	)

	// setup three instances on definition 100 and one on 200
	sharedDefName := util.Ptr("shared-def")
	otherDefName := util.Ptr("other-def")

	instances := []api_v0.MachineRuntimeInstance{
		makeInstance(1, "mri-a", 100),
		makeInstance(2, "mri-b", 100),
		makeInstance(3, "mri-c", 100),
		makeInstance(4, "mri-d", 200),
	}

	var listHits int32

	// serve instance list and definition-by-id
	mux := http.NewServeMux()
	mux.HandleFunc("/v0/machine-runtime-instances", func(w http.ResponseWriter, r *http.Request) {
		// count list hits and return all fixtures
		atomic.AddInt32(&listHits, 1)
		data := make([]apiserver_lib.Object, 0, len(instances))
		for i := range instances {
			data = append(data, instances[i])
		}
		writeResponse(t, w, data)
	})
	mux.HandleFunc("/v0/machine-runtime-definitions/", func(w http.ResponseWriter, r *http.Request) {
		// count the id in the path and return that definition
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

	// strip scheme; GetResponse prepends http:// on a stock client
	apiEndpoint := strings.TrimPrefix(srv.URL, "http://")

	// call Get with empty encryption key
	cfg := &MachineRuntimeInstanceConfig{}
	got, err := cfg.Get(srv.Client(), apiEndpoint, "")
	require.NoError(t, err)
	require.NotNil(t, got)

	// assert each config carries the definition name for its ID
	assert.Len(t, *got, 4)
	assert.Equal(t, sharedDefName, (*got)[0].MachineRuntimeInstance.MachineRuntimeDefinition.Name)
	assert.Equal(t, sharedDefName, (*got)[1].MachineRuntimeInstance.MachineRuntimeDefinition.Name)
	assert.Equal(t, sharedDefName, (*got)[2].MachineRuntimeInstance.MachineRuntimeDefinition.Name)
	assert.Equal(t, otherDefName, (*got)[3].MachineRuntimeInstance.MachineRuntimeDefinition.Name)

	// assert the list ran once and each definition ID was fetched once
	assert.Equal(t, int32(1), atomic.LoadInt32(&listHits), "list endpoint should be hit once")
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, defHitsByID[100], "shared definition id 100 should be fetched exactly once, not once per instance")
	assert.Equal(t, 1, defHitsByID[200], "distinct definition id 200 should be fetched exactly once")
}

// makeInstance returns a MachineRuntimeInstance for id, name, and definition id.
func makeInstance(id uint, name string, defID uint) api_v0.MachineRuntimeInstance {
	// set CreatedAt; GetAge dereferences the pointer
	createdAt := time.Now().Add(-time.Hour)
	return api_v0.MachineRuntimeInstance{
		Common:                     api_v0.Common{ID: util.Ptr(id), CreatedAt: &createdAt},
		Instance:                   api_v0.Instance{Name: util.Ptr(name)},
		Hostname:                   util.Ptr("host.example"),
		SSHUser:                    util.Ptr("root"),
		MachineRuntimeDefinitionID: util.Ptr(defID),
	}
}

// writeResponse encodes data as an api-server Response envelope.
func writeResponse(t *testing.T, w http.ResponseWriter, data []apiserver_lib.Object) {
	t.Helper()
	// write JSON envelope with HTTP 200
	resp := apiserver_lib.Response{Data: data}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	require.NoError(t, json.NewEncoder(w).Encode(resp))
}
