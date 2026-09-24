package observability

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"

	"github.com/threeport/threeport/internal/machinetest"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	controller "github.com/threeport/threeport/pkg/controller/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestV0LoggingInstanceCreatedEmitsReconciliationStartedBeforeFanOut covers
// the ReconciliationStarted emit at the top of v0LoggingInstanceCreated:
// the event lands on the recorder before the fan-out proceeds, so a
// reader sees the causal boundary even when a downstream API call fails.
func TestV0LoggingInstanceCreatedEmitsReconciliationStartedBeforeFanOut(t *testing.T) {
	// stub API returning 500 on every call so the first client fetch after
	// the emit fails, isolating the emit as the only observable behavior
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	// wire a reconciler with a fake recorder so the test can inspect the emit
	recorder := machinetest.NewFakeRecorder()
	r := &controller.Reconciler{
		APIClient:      server.Client(),
		APIServer:      server.URL,
		EventsRecorder: recorder,
	}

	// drive the created handler with a minimally-populated instance so the
	// emit runs before the client fetch, which is expected to error out
	loggingInstance := &v0.LoggingInstance{
		Common:              v0.Common{ID: util.Ptr(uint(42))},
		Instance:            v0.Instance{Name: util.Ptr("test-logging-instance")},
		LoggingDefinitionID: util.Ptr(uint(1)),
	}

	log := logr.Discard()
	_, err := v0LoggingInstanceCreated(r, loggingInstance, &log)

	// downstream fetch is expected to fail; the recorder capture is the
	// behavior under test
	assert.Error(t, err, "handler should surface the downstream fetch failure")

	// assert the emit landed on the recorder before the failure
	events := recorder.GetEvents()
	assert.Len(t, events, 1, "one ReconciliationStarted event should be recorded")
	if len(events) == 0 {
		return
	}

	// assert the recorded event carries the expected subject and payload
	got := events[0]
	assert.Equal(t, uint(42), got.ObjectID, "event object ID should match the logging instance")
	assert.Equal(t, "threeport.io/v0.LoggingInstance", got.Type, "event object type should be the fully qualified type")
	assert.NotNil(t, got.Event, "recorded event body should be populated")
	assert.Equal(t, "ReconciliationStarted", *got.Event.Reason)
	assert.Equal(t, "Normal", *got.Event.Type)
	assert.Contains(t, *got.Event.Note, "test-logging-instance")
	assert.Contains(t, *got.Event.Note, "starting reconciliation of logging instance")
}
