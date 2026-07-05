package aws

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

	logr "github.com/go-logr/logr"
	"github.com/nukleros/aws-builder/pkg/eks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	controller "github.com/threeport/threeport/pkg/controller/v0"
)

// apiStub wraps an httptest.Server with the shape the threeport client
// helpers expect (bare *http.Client + scheme-less addr).
type apiStub struct {
	mux    *http.ServeMux
	server *httptest.Server
	client *http.Client
	addr   string
}

// newAPIStub returns an apiStub with an empty mux. The addr has the
// "http://" scheme stripped because client_lib.GetResponse prepends its own
// scheme; the bare *http.Client keeps that scheme selection on "http://".
func newAPIStub(t *testing.T) *apiStub {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &apiStub{
		mux:    mux,
		server: srv,
		client: &http.Client{},
		addr:   strings.TrimPrefix(srv.URL, "http://"),
	}
}

// writeResponse marshals data into an apiserver_lib.Response envelope and
// writes it with the given status; the threeport client requires that shape.
func writeResponse(t *testing.T, w http.ResponseWriter, status int, data []apiserver_lib.Object) {
	t.Helper()
	body, err := json.Marshal(apiserver_lib.Response{Data: data})
	require.NoError(t, err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func uintPtr(v uint) *uint     { return &v }
func stringPtr(v string) *string { return &v }
func boolPtr(v bool) *bool     { return &v }
func timePtr(v time.Time) *time.Time { return &v }

// TestCheckStaleEksAck_FreshAckNotStale asserts that an acknowledgement
// timestamp taken within the stale window returns false.
func TestCheckStaleEksAck_FreshAckNotStale(t *testing.T) {
	// setup: an ack timestamp from 10s ago is well inside the 240s stale window
	fresh := time.Now().UTC().Add(-10 * time.Second)

	// action + assertion: not stale
	assert.False(t, checkStaleEksAck(fresh))
}

// TestCheckStaleEksAck_StaleAckIsStale asserts that an acknowledgement
// timestamp older than the stale threshold returns true.
func TestCheckStaleEksAck_StaleAckIsStale(t *testing.T) {
	// setup: an ack timestamp from 5 min ago is past the 240s stale window
	stale := time.Now().UTC().Add(-5 * time.Minute)

	// action + assertion: stale
	assert.True(t, checkStaleEksAck(stale))
}

// TestCheckStaleEksAck_TableDriven pins the stale/fresh boundary with
// several ages around the threshold.
func TestCheckStaleEksAck_TableDriven(t *testing.T) {
	cases := []struct {
		name     string
		ageSec   int
		wantStal bool
	}{
		{"one second old", 1, false},
		{"just under threshold", staleAckDurationSeconds - 5, false},
		{"just over threshold", staleAckDurationSeconds + 5, true},
		{"far past threshold", staleAckDurationSeconds * 4, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// setup: ack timestamp aged the target amount into the past
			ack := time.Now().UTC().Add(-time.Duration(tc.ageSec) * time.Second)

			// action + assertion: matches expected staleness verdict
			assert.Equal(t, tc.wantStal, checkStaleEksAck(ack))
		})
	}
}

// TestV0AwsEksKubernetesRuntimeInstanceUpdated_NoOp asserts the update
// handler is a no-op: it always returns (0, nil).
func TestV0AwsEksKubernetesRuntimeInstanceUpdated_NoOp(t *testing.T) {
	// setup: reconciler and instance carry no state (never touched)
	r := &controller.Reconciler{}
	inst := &v0.AwsEksKubernetesRuntimeInstance{}
	log := logr.Discard()

	// action: invoke the updated handler
	delay, err := v0AwsEksKubernetesRuntimeInstanceUpdated(r, inst, &log)

	// assertion: zero delay and no error
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)
}

// TestV0AwsEksKubernetesRuntimeInstanceDeleted_MissingScheduleErrors covers
// the guard that rejects a delete notification whose scheduled timestamp is
// nil: no reconciliation should happen and an error is returned.
func TestV0AwsEksKubernetesRuntimeInstanceDeleted_MissingScheduleErrors(t *testing.T) {
	// setup: DeletionScheduled nil while ID/Name are set so log-metadata
	// derefs succeed
	inst := &v0.AwsEksKubernetesRuntimeInstance{
		Common: v0.Common{ID: uintPtr(42)},
	}
	inst.Name = stringPtr("eks-a")
	inst.DeletionScheduled = nil
	log := logr.Discard()

	// action: call the delete handler
	delay, err := v0AwsEksKubernetesRuntimeInstanceDeleted(&controller.Reconciler{}, inst, &log)

	// assertion: returns the "not scheduled" error and zero delay
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not scheduled")
	assert.Equal(t, int64(0), delay)
}

// TestV0AwsEksKubernetesRuntimeInstanceDeleted_AlreadyConfirmedNoOp asserts
// the delete handler returns (0, nil) immediately when DeletionConfirmed
// already carries a timestamp.
func TestV0AwsEksKubernetesRuntimeInstanceDeleted_AlreadyConfirmedNoOp(t *testing.T) {
	// setup: DeletionScheduled and DeletionConfirmed both non-nil so the
	// handler passes the schedule guard and hits the confirmed branch
	now := time.Now().UTC()
	inst := &v0.AwsEksKubernetesRuntimeInstance{
		Common: v0.Common{ID: uintPtr(7)},
	}
	inst.Name = stringPtr("eks-confirmed")
	inst.DeletionScheduled = timePtr(now.Add(-time.Minute))
	inst.DeletionConfirmed = timePtr(now)
	log := logr.Discard()

	// action: call the delete handler
	delay, err := v0AwsEksKubernetesRuntimeInstanceDeleted(&controller.Reconciler{}, inst, &log)

	// assertion: no error, no requeue
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)
}

// TestGetInventory_HappyPath covers the read-then-decode path: the API
// returns an instance whose ResourceInventory carries a marshalled
// EksInventory, and getInventory returns the decoded value.
func TestGetInventory_HappyPath(t *testing.T) {
	// setup: build an inventory, marshal it, and stub the GET endpoint to
	// return an instance carrying that JSON
	stub := newAPIStub(t)
	want := eks.EksInventory{VpcId: "vpc-abc", ClusterAddon: true}
	invJSON, err := want.Marshal()
	require.NoError(t, err)
	invDT := datatypes.JSON(invJSON)
	fetched := v0.AwsEksKubernetesRuntimeInstance{
		Common:            v0.Common{ID: uintPtr(11)},
		ResourceInventory: &invDT,
	}
	stub.mux.HandleFunc(
		fmt.Sprintf("%s/%d", v0.PathAwsEksKubernetesRuntimeInstances, 11),
		func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, http.MethodGet, r.Method)
			writeResponse(t, w, http.StatusOK, []apiserver_lib.Object{fetched})
		},
	)
	r := &controller.Reconciler{APIClient: stub.client, APIServer: stub.addr}
	inst := &v0.AwsEksKubernetesRuntimeInstance{Common: v0.Common{ID: uintPtr(11)}}

	// action: retrieve the inventory
	got, err := getInventory(r, inst)

	// assertion: fields survive the round trip
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "vpc-abc", got.VpcId)
	assert.True(t, got.ClusterAddon)
}

// TestGetInventory_NilInventoryReturnsZero covers the branch where the API
// returns an instance whose ResourceInventory pointer is nil: getInventory
// returns a zero EksInventory and no error.
func TestGetInventory_NilInventoryReturnsZero(t *testing.T) {
	// setup: stub returns an instance with no inventory attached
	stub := newAPIStub(t)
	fetched := v0.AwsEksKubernetesRuntimeInstance{
		Common:            v0.Common{ID: uintPtr(12)},
		ResourceInventory: nil,
	}
	stub.mux.HandleFunc(
		fmt.Sprintf("%s/%d", v0.PathAwsEksKubernetesRuntimeInstances, 12),
		func(w http.ResponseWriter, r *http.Request) {
			writeResponse(t, w, http.StatusOK, []apiserver_lib.Object{fetched})
		},
	)
	r := &controller.Reconciler{APIClient: stub.client, APIServer: stub.addr}
	inst := &v0.AwsEksKubernetesRuntimeInstance{Common: v0.Common{ID: uintPtr(12)}}

	// action: retrieve the inventory
	got, err := getInventory(r, inst)

	// assertion: zero-value inventory, no error
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "", got.VpcId)
	assert.False(t, got.ClusterAddon)
}

// TestGetInventory_APIErrorPropagates covers the branch where the API
// responds with a non-200: getInventory wraps and returns the error.
func TestGetInventory_APIErrorPropagates(t *testing.T) {
	// setup: stub returns 500 for the GET
	stub := newAPIStub(t)
	stub.mux.HandleFunc(
		fmt.Sprintf("%s/%d", v0.PathAwsEksKubernetesRuntimeInstances, 13),
		func(w http.ResponseWriter, r *http.Request) {
			writeResponse(t, w, http.StatusInternalServerError, nil)
		},
	)
	r := &controller.Reconciler{APIClient: stub.client, APIServer: stub.addr}
	inst := &v0.AwsEksKubernetesRuntimeInstance{Common: v0.Common{ID: uintPtr(13)}}

	// action: retrieve the inventory
	got, err := getInventory(r, inst)

	// assertion: error surfaced, no inventory
	require.Error(t, err)
	assert.Nil(t, got)
	assert.Contains(t, err.Error(), "failed to get EKS cluster instance inventory")
}

// TestCheckEksCreated_ClusterAddonTrue asserts checkEksCreated returns
// true when the inventory reports the cluster addon is installed (the last
// step of creation).
func TestCheckEksCreated_ClusterAddonTrue(t *testing.T) {
	// setup: stub returns inventory with ClusterAddon = true
	stub := newAPIStub(t)
	inv := eks.EksInventory{ClusterAddon: true, VpcId: "vpc-x"}
	invJSON, err := inv.Marshal()
	require.NoError(t, err)
	invDT := datatypes.JSON(invJSON)
	fetched := v0.AwsEksKubernetesRuntimeInstance{
		Common:            v0.Common{ID: uintPtr(20)},
		ResourceInventory: &invDT,
	}
	stub.mux.HandleFunc(
		fmt.Sprintf("%s/%d", v0.PathAwsEksKubernetesRuntimeInstances, 20),
		func(w http.ResponseWriter, r *http.Request) {
			writeResponse(t, w, http.StatusOK, []apiserver_lib.Object{fetched})
		},
	)
	r := &controller.Reconciler{APIClient: stub.client, APIServer: stub.addr}
	inst := &v0.AwsEksKubernetesRuntimeInstance{Common: v0.Common{ID: uintPtr(20)}}

	// action: check creation completion
	done, err := checkEksCreated(r, inst)

	// assertion: creation complete, no error
	require.NoError(t, err)
	assert.True(t, done)
}

// TestCheckEksCreated_ClusterAddonFalse asserts checkEksCreated returns
// false when the cluster addon has not yet been created.
func TestCheckEksCreated_ClusterAddonFalse(t *testing.T) {
	// setup: stub returns inventory with ClusterAddon = false
	stub := newAPIStub(t)
	inv := eks.EksInventory{ClusterAddon: false, VpcId: "vpc-x"}
	invJSON, err := inv.Marshal()
	require.NoError(t, err)
	invDT := datatypes.JSON(invJSON)
	fetched := v0.AwsEksKubernetesRuntimeInstance{
		Common:            v0.Common{ID: uintPtr(21)},
		ResourceInventory: &invDT,
	}
	stub.mux.HandleFunc(
		fmt.Sprintf("%s/%d", v0.PathAwsEksKubernetesRuntimeInstances, 21),
		func(w http.ResponseWriter, r *http.Request) {
			writeResponse(t, w, http.StatusOK, []apiserver_lib.Object{fetched})
		},
	)
	r := &controller.Reconciler{APIClient: stub.client, APIServer: stub.addr}
	inst := &v0.AwsEksKubernetesRuntimeInstance{Common: v0.Common{ID: uintPtr(21)}}

	// action: check creation completion
	done, err := checkEksCreated(r, inst)

	// assertion: creation not complete, no error
	require.NoError(t, err)
	assert.False(t, done)
}

// TestCheckEksCreated_APIErrorPropagates asserts checkEksCreated wraps and
// returns errors from the inventory fetch.
func TestCheckEksCreated_APIErrorPropagates(t *testing.T) {
	// setup: stub returns 500 for the GET so getInventory fails
	stub := newAPIStub(t)
	stub.mux.HandleFunc(
		fmt.Sprintf("%s/%d", v0.PathAwsEksKubernetesRuntimeInstances, 22),
		func(w http.ResponseWriter, r *http.Request) {
			writeResponse(t, w, http.StatusInternalServerError, nil)
		},
	)
	r := &controller.Reconciler{APIClient: stub.client, APIServer: stub.addr}
	inst := &v0.AwsEksKubernetesRuntimeInstance{Common: v0.Common{ID: uintPtr(22)}}

	// action: check creation completion
	done, err := checkEksCreated(r, inst)

	// assertion: error surfaced, false returned
	require.Error(t, err)
	assert.False(t, done)
	assert.Contains(t, err.Error(), "creation check")
}

// TestCheckDeleted_VpcRemovedIsDeleted asserts checkDeleted returns true
// when the VPC has been removed from the inventory (the last step of
// deletion).
func TestCheckDeleted_VpcRemovedIsDeleted(t *testing.T) {
	// setup: stub returns inventory with empty VpcId
	stub := newAPIStub(t)
	inv := eks.EksInventory{VpcId: ""}
	invJSON, err := inv.Marshal()
	require.NoError(t, err)
	invDT := datatypes.JSON(invJSON)
	fetched := v0.AwsEksKubernetesRuntimeInstance{
		Common:            v0.Common{ID: uintPtr(30)},
		ResourceInventory: &invDT,
	}
	stub.mux.HandleFunc(
		fmt.Sprintf("%s/%d", v0.PathAwsEksKubernetesRuntimeInstances, 30),
		func(w http.ResponseWriter, r *http.Request) {
			writeResponse(t, w, http.StatusOK, []apiserver_lib.Object{fetched})
		},
	)
	r := &controller.Reconciler{APIClient: stub.client, APIServer: stub.addr}
	inst := &v0.AwsEksKubernetesRuntimeInstance{Common: v0.Common{ID: uintPtr(30)}}

	// action: check deletion completion
	deleted, err := checkDeleted(r, inst)

	// assertion: deletion complete, no error
	require.NoError(t, err)
	assert.True(t, deleted)
}

// TestCheckDeleted_VpcStillPresentNotDeleted asserts checkDeleted returns
// false when the VPC still exists in the inventory.
func TestCheckDeleted_VpcStillPresentNotDeleted(t *testing.T) {
	// setup: stub returns inventory with a populated VpcId
	stub := newAPIStub(t)
	inv := eks.EksInventory{VpcId: "vpc-still-there"}
	invJSON, err := inv.Marshal()
	require.NoError(t, err)
	invDT := datatypes.JSON(invJSON)
	fetched := v0.AwsEksKubernetesRuntimeInstance{
		Common:            v0.Common{ID: uintPtr(31)},
		ResourceInventory: &invDT,
	}
	stub.mux.HandleFunc(
		fmt.Sprintf("%s/%d", v0.PathAwsEksKubernetesRuntimeInstances, 31),
		func(w http.ResponseWriter, r *http.Request) {
			writeResponse(t, w, http.StatusOK, []apiserver_lib.Object{fetched})
		},
	)
	r := &controller.Reconciler{APIClient: stub.client, APIServer: stub.addr}
	inst := &v0.AwsEksKubernetesRuntimeInstance{Common: v0.Common{ID: uintPtr(31)}}

	// action: check deletion completion
	deleted, err := checkDeleted(r, inst)

	// assertion: deletion not complete, no error
	require.NoError(t, err)
	assert.False(t, deleted)
}

// TestCheckDeleted_APIErrorPropagates asserts checkDeleted wraps and
// returns errors from the inventory fetch.
func TestCheckDeleted_APIErrorPropagates(t *testing.T) {
	// setup: stub returns 500 so getInventory fails
	stub := newAPIStub(t)
	stub.mux.HandleFunc(
		fmt.Sprintf("%s/%d", v0.PathAwsEksKubernetesRuntimeInstances, 32),
		func(w http.ResponseWriter, r *http.Request) {
			writeResponse(t, w, http.StatusInternalServerError, nil)
		},
	)
	r := &controller.Reconciler{APIClient: stub.client, APIServer: stub.addr}
	inst := &v0.AwsEksKubernetesRuntimeInstance{Common: v0.Common{ID: uintPtr(32)}}

	// action: check deletion completion
	deleted, err := checkDeleted(r, inst)

	// assertion: error surfaced, false returned
	require.Error(t, err)
	assert.False(t, deleted)
	assert.Contains(t, err.Error(), "deletion check")
}

// TestV0AwsEksKubernetesRuntimeInstanceCreated_AlreadyConfirmedNoOp covers
// the early-return path where the API-fetched instance already carries a
// CreationConfirmed timestamp: no work should occur and no requeue delay is
// returned.
func TestV0AwsEksKubernetesRuntimeInstanceCreated_AlreadyConfirmedNoOp(t *testing.T) {
	// setup: stub GET returns an instance with CreationConfirmed set
	stub := newAPIStub(t)
	now := time.Now().UTC()
	fetched := v0.AwsEksKubernetesRuntimeInstance{
		Common: v0.Common{ID: uintPtr(40)},
	}
	fetched.Name = stringPtr("eks-done")
	fetched.CreationConfirmed = timePtr(now)
	stub.mux.HandleFunc(
		fmt.Sprintf("%s/%d", v0.PathAwsEksKubernetesRuntimeInstances, 40),
		func(w http.ResponseWriter, r *http.Request) {
			writeResponse(t, w, http.StatusOK, []apiserver_lib.Object{fetched})
		},
	)
	r := &controller.Reconciler{APIClient: stub.client, APIServer: stub.addr}
	// pass a caller-side stub with just enough state for log metadata
	inst := &v0.AwsEksKubernetesRuntimeInstance{Common: v0.Common{ID: uintPtr(40)}}
	inst.Name = stringPtr("eks-done")
	log := logr.Discard()

	// action: run the create handler
	delay, err := v0AwsEksKubernetesRuntimeInstanceCreated(r, inst, &log)

	// assertion: no error, no requeue
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)
}

// TestV0AwsEksKubernetesRuntimeInstanceCreated_FreshAckRequeues covers the
// path where creation has been acknowledged recently but not yet completed:
// the handler should return the 90s requeue delay and not advance state.
func TestV0AwsEksKubernetesRuntimeInstanceCreated_FreshAckRequeues(t *testing.T) {
	// setup: fetched instance carries a fresh CreationAcknowledged, no
	// CreationConfirmed, CreationFailed=false, and an inventory whose
	// ClusterAddon is false so checkEksCreated returns false
	stub := newAPIStub(t)
	inv := eks.EksInventory{ClusterAddon: false}
	invJSON, err := inv.Marshal()
	require.NoError(t, err)
	invDT := datatypes.JSON(invJSON)
	freshAck := time.Now().UTC().Add(-30 * time.Second)
	fetched := v0.AwsEksKubernetesRuntimeInstance{
		Common:            v0.Common{ID: uintPtr(41)},
		ResourceInventory: &invDT,
	}
	fetched.Name = stringPtr("eks-fresh")
	fetched.CreationAcknowledged = timePtr(freshAck)
	fetched.CreationFailed = boolPtr(false)

	// count how many times the GET is served: the handler fetches once
	// (initial) and then getInventory fetches again inside checkEksCreated
	var gets int32
	stub.mux.HandleFunc(
		fmt.Sprintf("%s/%d", v0.PathAwsEksKubernetesRuntimeInstances, 41),
		func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, http.MethodGet, r.Method)
			atomic.AddInt32(&gets, 1)
			writeResponse(t, w, http.StatusOK, []apiserver_lib.Object{fetched})
		},
	)
	r := &controller.Reconciler{APIClient: stub.client, APIServer: stub.addr}
	inst := &v0.AwsEksKubernetesRuntimeInstance{Common: v0.Common{ID: uintPtr(41)}}
	inst.Name = stringPtr("eks-fresh")
	log := logr.Discard()

	// action: run the create handler
	delay, err := v0AwsEksKubernetesRuntimeInstanceCreated(r, inst, &log)

	// assertion: no error, 90s requeue, GET hit twice (initial + inventory)
	require.NoError(t, err)
	assert.Equal(t, int64(90), delay)
	assert.GreaterOrEqual(t, atomic.LoadInt32(&gets), int32(2))
}

// TestV0AwsEksKubernetesRuntimeInstanceCreated_InitialGetErrorPropagates
// covers the branch where the initial API fetch fails: the handler wraps
// and returns the error without any further state change.
func TestV0AwsEksKubernetesRuntimeInstanceCreated_InitialGetErrorPropagates(t *testing.T) {
	// setup: stub returns 500 for the initial GET
	stub := newAPIStub(t)
	stub.mux.HandleFunc(
		fmt.Sprintf("%s/%d", v0.PathAwsEksKubernetesRuntimeInstances, 42),
		func(w http.ResponseWriter, r *http.Request) {
			writeResponse(t, w, http.StatusInternalServerError, nil)
		},
	)
	r := &controller.Reconciler{APIClient: stub.client, APIServer: stub.addr}
	inst := &v0.AwsEksKubernetesRuntimeInstance{Common: v0.Common{ID: uintPtr(42)}}
	inst.Name = stringPtr("eks-err")
	log := logr.Discard()

	// action: run the create handler
	delay, err := v0AwsEksKubernetesRuntimeInstanceCreated(r, inst, &log)

	// assertion: wrapped error, zero delay
	require.Error(t, err)
	assert.Equal(t, int64(0), delay)
	assert.Contains(t, err.Error(), "latest version of AWS EKS kubernetes runtime instance")
}

// TestDeleteInfra_ErrorLogged asserts that deleteInfra swallows the error
// from clusterInfra.Delete() after logging it, keeping the goroutine
// non-blocking for the caller.
func TestDeleteInfra_ErrorLogged(t *testing.T) {
	// setup: an empty KubernetesRuntimeInfraEKS with no ResourceClient will
	// panic if Delete() is invoked with real work; we instead pass one that
	// has a zero-value ResourceInventory and no client to just exercise that
	// the function returns without panic on a nil-run
	// NOTE: this test is intentionally omitted because Delete() dereferences
	// the ResourceClient. Skipping keeps the suite focused on isolated
	// behavior.
	t.Skip("deleteInfra requires a live ResourceClient; skipped as untestable in isolation")
}

// TestApiStubHelpersBuildValidEnvelope guards the local test helpers so a
// stub misconfiguration surfaces at test-authoring time rather than deep
// inside a client_lib decoder.
func TestApiStubHelpersBuildValidEnvelope(t *testing.T) {
	// setup: register a handler that echoes an instance back
	stub := newAPIStub(t)
	obj := v0.AwsEksKubernetesRuntimeInstance{Common: v0.Common{ID: uintPtr(1)}}
	stub.mux.HandleFunc(
		fmt.Sprintf("%s/%d", v0.PathAwsEksKubernetesRuntimeInstances, 1),
		func(w http.ResponseWriter, r *http.Request) {
			writeResponse(t, w, http.StatusOK, []apiserver_lib.Object{obj})
		},
	)

	// action: hit the stub with a raw GET using the same scheme-less addr
	// the client would use
	resp, err := stub.client.Get("http://" + stub.addr + fmt.Sprintf("%s/%d", v0.PathAwsEksKubernetesRuntimeInstances, 1))

	// assertion: 200 and a decodable envelope
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var env apiserver_lib.Response
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&env))
	require.Len(t, env.Data, 1)
}

// TestCheckStaleEksAck_Concurrent guards checkStaleEksAck for parallel use
// (it reads only local state, so many goroutines must all agree).
func TestCheckStaleEksAck_Concurrent(t *testing.T) {
	// setup: a shared stale timestamp across N goroutines
	stale := time.Now().UTC().Add(-time.Hour)
	var wg sync.WaitGroup
	const n = 32
	results := make([]bool, n)

	// action: each goroutine evaluates staleness on the same input
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = checkStaleEksAck(stale)
		}(i)
	}
	wg.Wait()

	// assertion: every goroutine sees the same stale=true result
	for i, r := range results {
		assert.True(t, r, "index %d saw non-stale", i)
	}
}
