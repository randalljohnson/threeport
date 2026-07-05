package notify

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	"github.com/threeport/threeport/internal/agent"
	tpapi "github.com/threeport/threeport/pkg/api/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestAppendUniqueWRI covers the helper's append and replace
// branches. The dedup key is *uint pointer equality (not underlying
// value equality), so the replace branch only fires when the
// caller passes back the SAME pointer address the existing entry
// holds. Two independently allocated *uint pointers referring to
// the same integer are treated as distinct.
func TestAppendUniqueWRI(t *testing.T) {
	// shared pointer used to seed and replace the same entry so the
	// replace branch fires: pointer equality is what appendUniqueWRI
	// compares
	sharedID := uint(1)
	sharedIDPtr := &sharedID

	tests := []struct {
		name         string
		initial      []tpapi.KubernetesWorkloadResourceInstance
		toAppend     tpapi.KubernetesWorkloadResourceInstance
		expectedLen  int
		expectedLast string // LastOperation of the entry whose ID pointer matches toAppend.ID
	}{
		{
			name:         "appends to empty slice",
			initial:      nil,
			toAppend:     tpapi.KubernetesWorkloadResourceInstance{Common: tpapi.Common{ID: util.Ptr(uint(1))}, LastOperation: util.Ptr("ADDED")},
			expectedLen:  1,
			expectedLast: "ADDED",
		},
		{
			name: "appends when no ID pointer matches",
			initial: []tpapi.KubernetesWorkloadResourceInstance{
				{Common: tpapi.Common{ID: util.Ptr(uint(1))}, LastOperation: util.Ptr("ADDED")},
			},
			toAppend:     tpapi.KubernetesWorkloadResourceInstance{Common: tpapi.Common{ID: util.Ptr(uint(2))}, LastOperation: util.Ptr("MODIFIED")},
			expectedLen:  2,
			expectedLast: "MODIFIED",
		},
		{
			name: "replaces existing entry when ID pointer matches",
			initial: []tpapi.KubernetesWorkloadResourceInstance{
				{Common: tpapi.Common{ID: sharedIDPtr}, LastOperation: util.Ptr("ADDED")},
				{Common: tpapi.Common{ID: util.Ptr(uint(2))}, LastOperation: util.Ptr("ADDED")},
			},
			toAppend:     tpapi.KubernetesWorkloadResourceInstance{Common: tpapi.Common{ID: sharedIDPtr}, LastOperation: util.Ptr("MODIFIED")},
			expectedLen:  2,
			expectedLast: "MODIFIED",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// exercise the helper against the seeded slice
			out := appendUniqueWRI(tc.initial, tc.toAppend)

			// slice length matches the append-vs-replace expectation
			assert.Len(t, out, tc.expectedLen)

			// entry whose ID pointer is the toAppend's pointer carries
			// the new LastOperation
			found := false
			for _, w := range out {
				if w.ID == tc.toAppend.ID {
					require.NotNil(t, w.LastOperation)
					assert.Equal(t, tc.expectedLast, *w.LastOperation)
					found = true
				}
			}
			assert.True(t, found, "expected an entry with the appended WRI's ID pointer")
		})
	}
}

// TestAppendUniqueWRI_ReplaceIsInPlace verifies the replace branch
// keeps the slice's ordering: the replaced WRI stays at its
// original index rather than moving to the tail. Uses a shared
// pointer since the dedup check is pointer equality.
func TestAppendUniqueWRI_ReplaceIsInPlace(t *testing.T) {
	// shared pointer seeds the middle entry; replacement reuses the
	// same pointer so the replace branch fires
	middleID := uint(2)
	middlePtr := &middleID

	initial := []tpapi.KubernetesWorkloadResourceInstance{
		{Common: tpapi.Common{ID: util.Ptr(uint(1))}, LastOperation: util.Ptr("A")},
		{Common: tpapi.Common{ID: middlePtr}, LastOperation: util.Ptr("B")},
		{Common: tpapi.Common{ID: util.Ptr(uint(3))}, LastOperation: util.Ptr("C")},
	}
	replacement := tpapi.KubernetesWorkloadResourceInstance{
		Common:        tpapi.Common{ID: middlePtr},
		LastOperation: util.Ptr("B-new"),
	}

	// exercise the replace path
	out := appendUniqueWRI(initial, replacement)

	// ordering preserved: index 1 still holds the middlePtr entry
	require.Len(t, out, 3)
	assert.Equal(t, middlePtr, out[1].ID, "middle entry stays at its index")
	require.NotNil(t, out[1].LastOperation)
	assert.Equal(t, "B-new", *out[1].LastOperation, "replacement value survives")
}

// TestAppendUniqueWRI_DistinctPointersSameValueAppend documents the
// current implementation's dedup semantics: pointer equality, not
// integer equality. Two *uint pointers referring to the same
// underlying value are treated as distinct, so appendUniqueWRI
// appends rather than replaces.
func TestAppendUniqueWRI_DistinctPointersSameValueAppend(t *testing.T) {
	// two independently allocated pointers, both to the value 1
	initial := []tpapi.KubernetesWorkloadResourceInstance{
		{Common: tpapi.Common{ID: util.Ptr(uint(1))}, LastOperation: util.Ptr("ADDED")},
	}
	toAppend := tpapi.KubernetesWorkloadResourceInstance{
		Common:        tpapi.Common{ID: util.Ptr(uint(1))},
		LastOperation: util.Ptr("MODIFIED"),
	}

	// exercise the helper
	out := appendUniqueWRI(initial, toAppend)

	// distinct pointer addresses do NOT match under pointer equality
	// so the new entry is appended alongside the existing one
	require.Len(t, out, 2)
	require.NotNil(t, out[0].LastOperation)
	require.NotNil(t, out[1].LastOperation)
	assert.Equal(t, "ADDED", *out[0].LastOperation)
	assert.Equal(t, "MODIFIED", *out[1].LastOperation)
}

// TestNotify_ReturnsWhenClosedChannelEmpty asserts that Notify exits
// promptly when its input channel is closed with no pending data:
// the WaitGroup counter must return to zero without any HTTP calls
// being made.
func TestNotify_ReturnsWhenClosedChannelEmpty(t *testing.T) {
	// server that fails the test if any request arrives; the empty-close
	// path must skip the final send entirely
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected HTTP request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	// buffered channel closed before Notify runs; the first select
	// iteration receives ok=false and returns
	ch := make(chan ThreeportNotif, 1)
	close(ch)

	var wg sync.WaitGroup
	done := make(chan struct{})
	go func() {
		Notify(ch, strings.TrimPrefix(srv.URL, "http://"), &http.Client{}, logr.Discard(), &wg)
		close(done)
	}()

	// Notify must exit quickly: no messages queued, closed channel
	// takes the receive branch immediately
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Notify did not return after channel close")
	}

	// WaitGroup was Add(1)/Done(1) internally, so Wait returns without
	// blocking
	wg.Wait()
}

// TestNotify_DiscardsHelmOperation covers the branch that silently
// drops an Operation carrying WorkloadType=HelmWorkloadInstance: the
// caller has no Threeport-side WRI to update for helm workloads, so
// no WRI must be accumulated and the final send must be skipped.
func TestNotify_DiscardsHelmOperation(t *testing.T) {
	// server that fails the test if any request arrives; a discarded
	// operation leaves the pending slice empty, so the closed-channel
	// exit path must skip the final send
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected HTTP request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	// queue one Helm operation, then close so Notify processes and exits
	ch := make(chan ThreeportNotif, 2)
	ch <- ThreeportNotif{
		Operation: &ResourceOperation{
			WorkloadType:                         agent.HelmWorkloadInstanceType,
			KubernetesWorkloadResourceInstanceID: 42,
			OperationType:                        "ADDED",
			OperationObject:                      `{"kind":"Deployment"}`,
		},
	}
	close(ch)

	// drive Notify to completion
	var wg sync.WaitGroup
	Notify(ch, strings.TrimPrefix(srv.URL, "http://"), &http.Client{}, logr.Discard(), &wg)
	wg.Wait()
}

// TestNotify_SkipsUnrecognizedEventWorkloadType covers the default
// arm of the event-classification switch: an event whose WorkloadType
// isn't one of the known kinds AND whose KWRI ID is zero must be
// logged and skipped, not appended to pendingEvents.
func TestNotify_SkipsUnrecognizedEventWorkloadType(t *testing.T) {
	// server that fails the test if anything comes through; the event
	// must be dropped, leaving the pending slice empty
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected HTTP request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	// queue one event with unknown workload type + zero resource-inst
	// ID so no arm of the switch matches; close so Notify exits
	ch := make(chan ThreeportNotif, 2)
	ch <- ThreeportNotif{
		Event: &EventSummary{
			WorkloadType:                         "SomethingUnknown",
			KubernetesWorkloadInstanceID:         7,
			KubernetesWorkloadResourceInstanceID: 0,
			Timestamp:                            metav1.Now(),
			Type:                                 "Warning",
			Reason:                               "Weird",
			Message:                              "unrecognized",
		},
	}
	close(ch)

	// drive Notify to completion
	var wg sync.WaitGroup
	Notify(ch, strings.TrimPrefix(srv.URL, "http://"), &http.Client{}, logr.Discard(), &wg)
	wg.Wait()
}

// TestNotify_ClassifiesEventsAndSendsOnClose covers the three
// recognized event subject classifications (KWRI id present, KWI
// workload type, Helm workload type). It also exercises the
// final-send path triggered when the channel closes with pending
// data: each event should result in a GET+POST to the events
// endpoints via EventRecorder.RecordEvent.
func TestNotify_ClassifiesEventsAndSendsOnClose(t *testing.T) {
	mock := newMockAPI(t)
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer srv.Close()

	// each event maps to one of the three recognized cases; the
	// final send must issue GET+POST once per event
	ch := make(chan ThreeportNotif, 4)
	ch <- ThreeportNotif{
		Event: &EventSummary{
			// KWRI id set: hits the first switch arm regardless of
			// workload type
			KubernetesWorkloadResourceInstanceID: 11,
			Type:                                 "Warning",
			Reason:                               "R1",
			Message:                              "m1",
		},
	}
	ch <- ThreeportNotif{
		Event: &EventSummary{
			WorkloadType:                 agent.KubernetesWorkloadInstanceType,
			KubernetesWorkloadInstanceID: 22,
			Type:                         "Normal",
			Reason:                       "R2",
			Message:                      "m2",
		},
	}
	ch <- ThreeportNotif{
		Event: &EventSummary{
			WorkloadType:                 agent.HelmWorkloadInstanceType,
			KubernetesWorkloadInstanceID: 33,
			Type:                         "Normal",
			Reason:                       "R3",
			Message:                      "m3",
		},
	}
	close(ch)

	// drive Notify to completion; the closed-channel branch invokes
	// sendThreeportUpdates before returning
	var wg sync.WaitGroup
	Notify(ch, strings.TrimPrefix(srv.URL, "http://"), &http.Client{}, logr.Discard(), &wg)
	wg.Wait()

	// three events, each producing one GET (dedup lookup) + one POST
	// (create)
	assert.Equal(t, int32(3), atomic.LoadInt32(&mock.gets), "one dedup GET per event")
	assert.Equal(t, int32(3), atomic.LoadInt32(&mock.posts), "one create POST per event")

	// verify each POST body carries the expected fully qualified
	// ObjectType and ObjectID for its case
	postedTypes := make(map[uint]string)
	for _, ev := range mock.postedEvents() {
		require.NotNil(t, ev.ObjectID)
		require.NotNil(t, ev.ObjectType)
		postedTypes[*ev.ObjectID] = *ev.ObjectType
	}
	assert.Equal(t, "threeport.io/v0.KubernetesWorkloadResourceInstance", postedTypes[11],
		"KWRI id case posts KWRI object type")
	assert.Equal(t, "threeport.io/v0.KubernetesWorkloadInstance", postedTypes[22],
		"KWI workload case posts KWI object type")
	assert.Equal(t, "threeport.io/v0.HelmWorkloadInstance", postedTypes[33],
		"Helm workload case posts HelmWorkloadInstance object type")
}

// TestNotify_SendsOperationUpdateOnClose covers the operation branch:
// a non-Helm Operation must accumulate a WRI, and the closed-channel
// final send must PATCH it against the Threeport API.
func TestNotify_SendsOperationUpdateOnClose(t *testing.T) {
	mock := newMockAPI(t)
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer srv.Close()

	// queue one KubernetesWorkloadInstance operation, then close
	ch := make(chan ThreeportNotif, 2)
	ch <- ThreeportNotif{
		Operation: &ResourceOperation{
			WorkloadType:                         agent.KubernetesWorkloadInstanceType,
			KubernetesWorkloadResourceInstanceID: 77,
			OperationType:                        "MODIFIED",
			OperationObject:                      `{"kind":"Deployment","metadata":{"name":"web"}}`,
		},
	}
	close(ch)

	// drive Notify to completion; the final send invokes the WRI PATCH
	var wg sync.WaitGroup
	Notify(ch, strings.TrimPrefix(srv.URL, "http://"), &http.Client{}, logr.Discard(), &wg)
	wg.Wait()

	// one PATCH against the KWRI path is expected
	assert.Equal(t, int32(1), atomic.LoadInt32(&mock.patches),
		"KWRI operation triggers exactly one PATCH")
	assert.Equal(t, "/v0/kubernetes-workload-resource-instances/77", mock.lastPatchPath(),
		"PATCH targets the operation's KWRI id")
}

// mockAPI is a tiny httptest handler that stands in for the Threeport
// API surface exercised by Notify's final-send path: KWRI PATCH,
// events GET (join query), and events POST (create).
type mockAPI struct {
	t       *testing.T
	mu      sync.Mutex
	posts   int32
	gets    int32
	patches int32
	posted  []tpapi.Event
	lastPTH string
}

func newMockAPI(t *testing.T) *mockAPI {
	return &mockAPI{t: t}
}

func (m *mockAPI) postedEvents() []tpapi.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]tpapi.Event, len(m.posted))
	copy(out, m.posted)
	return out
}

func (m *mockAPI) lastPatchPath() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastPTH
}

func (m *mockAPI) handler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v0/events-join-attached-object-references":
		// dedup lookup: return an empty result so RecordEvent takes
		// the create branch
		atomic.AddInt32(&m.gets, 1)
		writeMockEnvelope(m.t, w, http.StatusOK, nil)
	case r.Method == http.MethodPost && r.URL.Path == tpapi.PathEvents:
		// create branch: echo the inbound payload back with an
		// assigned ID so RecordEvent can decode response.Data[0]
		atomic.AddInt32(&m.posts, 1)
		var ev tpapi.Event
		require.NoError(m.t, json.Unmarshal(body, &ev))
		ev.ID = util.Ptr(uint(1))
		m.mu.Lock()
		m.posted = append(m.posted, ev)
		m.mu.Unlock()
		writeMockEnvelope(m.t, w, http.StatusCreated, []apiserver_lib.Object{ev})
	case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/v0/kubernetes-workload-resource-instances/"):
		// operation update: echo an empty KWRI back
		atomic.AddInt32(&m.patches, 1)
		m.mu.Lock()
		m.lastPTH = r.URL.Path
		m.mu.Unlock()
		writeMockEnvelope(m.t, w, http.StatusOK, []apiserver_lib.Object{tpapi.KubernetesWorkloadResourceInstance{Common: tpapi.Common{ID: util.Ptr(uint(1))}}})
	default:
		http.Error(w, "unexpected request: "+r.Method+" "+r.URL.Path, http.StatusNotFound)
	}
}

func writeMockEnvelope(t *testing.T, w http.ResponseWriter, status int, data []apiserver_lib.Object) {
	t.Helper()
	body, err := json.Marshal(apiserver_lib.Response{Data: data})
	require.NoError(t, err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
