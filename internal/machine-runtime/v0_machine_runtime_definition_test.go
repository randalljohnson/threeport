package machineruntime

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	logr "github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/threeport/threeport/internal/machinetest"
	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	controller "github.com/threeport/threeport/pkg/controller/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestMachineRuntimeDefinitionCreated_AdoptsExistingMarried covers the Gap B
// adopt guard at the definition site: when a married GCE definition already
// exists for this parent, a re-reconcile must adopt it instead of creating a
// second one. A create that succeeded before the parent recorded its reconciled
// state would otherwise be duplicated on retry.
func TestMachineRuntimeDefinitionCreated_AdoptsExistingMarried(t *testing.T) {
	defID := uint(501)

	// arrange a provider-provisioned definition whose married GCE definition
	// already exists from a prior, partially-completed reconcile
	mrd := &v0.MachineRuntimeDefinition{
		Common:         v0.Common{ID: util.Ptr(defID)},
		Definition:     v0.Definition{Name: util.Ptr("mrd-adopt")},
		InfraProvider:  util.Ptr(v0.MachineRuntimeInfraProviderGCE),
		MachineProfile: util.Ptr("Balanced"),
		MachineSize:    util.Ptr("Medium"),
	}

	api := machinetest.NewAPIStub(t)

	// register a single handler for the GCE definition collection path: the GET
	// query returns the existing married definition; a POST create fails the
	// test, proving the adopt guard skips creation
	var createCalls int64
	existing := v0.GcpGceMachineRuntimeDefinition{
		Common:                     v0.Common{ID: util.Ptr(uint(601))},
		Definition:                 v0.Definition{Name: util.Ptr("mrd-adopt")},
		MachineRuntimeDefinitionID: util.Ptr(defID),
	}
	api.Mux.HandleFunc(v0.PathGcpGceMachineRuntimeDefinitions, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			m := existing
			machinetest.WriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{&m})
		case http.MethodPost:
			atomic.AddInt64(&createCalls, 1)
			machinetest.WriteResponse(t, w, http.StatusCreated, []apiserver_lib.Object{&existing})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	// the handler marks the abstract definition reconciled at the end; record
	// that PATCH so the test can confirm the adopt path still settles the object
	var patches [][]byte
	api.Mux.HandleFunc(fmt.Sprintf("%s/%d", v0.PathMachineRuntimeDefinitions, defID), func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPatch, r.Method)
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		patches = append(patches, body)
		var updated v0.MachineRuntimeDefinition
		require.NoError(t, json.Unmarshal(body, &updated))
		updated.ID = util.Ptr(defID)
		machinetest.WriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{&updated})
	})

	log := logr.Discard()
	r := &controller.Reconciler{
		APIClient: api.Client,
		APIServer: api.Addr,
	}

	// run the Created hook against a definition whose married child already exists
	delay, err := v0MachineRuntimeDefinitionCreated(r, mrd, &log)
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)
	// verify the married definition was adopted, not recreated
	assert.Equal(t, int64(0), atomic.LoadInt64(&createCalls), "an existing married definition must be adopted, not recreated")
	// verify the abstract definition is still marked reconciled
	require.Len(t, patches, 1, "the adopt path must still mark the definition reconciled")
	assert.Contains(t, string(patches[0]), `"Reconciled":true`)
}

// TestMachineRuntimeDefinitionCreated_CreatesWhenMissing covers the create path:
// when no married GCE definition exists, the handler resolves the machine type
// and creates one, then marks the abstract definition reconciled.
func TestMachineRuntimeDefinitionCreated_CreatesWhenMissing(t *testing.T) {
	defID := uint(502)

	mrd := &v0.MachineRuntimeDefinition{
		Common:         v0.Common{ID: util.Ptr(defID)},
		Definition:     v0.Definition{Name: util.Ptr("mrd-create")},
		InfraProvider:  util.Ptr(v0.MachineRuntimeInfraProviderGCE),
		MachineProfile: util.Ptr("Balanced"),
		MachineSize:    util.Ptr("Medium"),
	}

	api := machinetest.NewAPIStub(t)

	// the GET query returns no existing definition, so the handler creates one
	var createCalls int64
	api.Mux.HandleFunc(v0.PathGcpGceMachineRuntimeDefinitions, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			machinetest.WriteResponse(t, w, http.StatusOK, nil)
		case http.MethodPost:
			atomic.AddInt64(&createCalls, 1)
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			var created v0.GcpGceMachineRuntimeDefinition
			require.NoError(t, json.Unmarshal(body, &created))
			created.ID = util.Ptr(uint(602))
			machinetest.WriteResponse(t, w, http.StatusCreated, []apiserver_lib.Object{&created})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	api.Mux.HandleFunc(fmt.Sprintf("%s/%d", v0.PathMachineRuntimeDefinitions, defID), func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPatch, r.Method)
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var updated v0.MachineRuntimeDefinition
		require.NoError(t, json.Unmarshal(body, &updated))
		updated.ID = util.Ptr(defID)
		machinetest.WriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{&updated})
	})

	log := logr.Discard()
	r := &controller.Reconciler{
		APIClient: api.Client,
		APIServer: api.Addr,
	}

	// run the Created hook with no pre-existing married definition
	delay, err := v0MachineRuntimeDefinitionCreated(r, mrd, &log)
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)
	// verify exactly one married definition was created
	assert.Equal(t, int64(1), atomic.LoadInt64(&createCalls), "a missing married definition must be created once")
}
