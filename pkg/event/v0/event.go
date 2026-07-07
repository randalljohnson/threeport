package v0

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-logr/logr"
	api "github.com/threeport/threeport/pkg/api/v0"
	client_v0 "github.com/threeport/threeport/pkg/client/v0"
	tp_errors "github.com/threeport/threeport/pkg/errors/v0"
	notifications "github.com/threeport/threeport/pkg/notifications/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

const (
	// Default event reasons for crud operations.
	ReasonSuccessfulCreate = "SuccessfulCreate"
	ReasonFailedCreate     = "FailedCreate"

	ReasonSuccessfulUpdate = "SuccessfulUpdate"
	ReasonFailedUpdate     = "FailedUpdate"

	ReasonSuccessfulDelete = "SuccessfulDelete"
	ReasonFailedDelete     = "FailedDelete"

	// Default event types
	TypeNormal  = "Normal"
	TypeWarning = "Warning"
)

// EventRecorder records events to the backend.
type EventRecorder struct {

	// APIClient is the HTTP client used to make requests to the Threeport API.
	APIClient *http.Client

	// APIServer is the endpoint to reach Threeport REST API.
	// format: [protocol]://[hostname]:[port]
	APIServer string

	// Name of the controller that emitted this Event.
	ReportingController string
}

// RecordEvent posts a new event row for the given object. Every emit
// stores a raw Count=1 row; dedup and aggregation happen at read time
// in the events endpoint handler, so concurrent recorders do not race
// on a shared bump path. fullyQualifiedObjectType must be the form
// returned by GetFullyQualifiedType.
func (r *EventRecorder) RecordEvent(
	event *api.Event,
	objectId uint,
	fullyQualifiedObjectType string,
) error {
	// stamp reporter, timestamps, and count on the in-memory event.
	// count is always 1 at insert time; the meaningful aggregate lives
	// at read time.
	now := time.Now()
	event.ReportingController = &r.ReportingController
	event.EventTime = util.Ptr(now)
	event.LastObservedTime = util.Ptr(now)
	event.Count = util.Ptr(uint(1))

	// carry the subject info on the in-memory Event so the API
	// server's beforeCreate validates and afterCreate writes the
	// matching AttachedObjectReference in the same transaction.
	// gorm:"-" keeps these fields off the row; the AOR is the on-disk
	// source of truth for the subject linkage.
	event.ObjectType = util.Ptr(fullyQualifiedObjectType)
	event.ObjectID = util.Ptr(objectId)

	if _, err := client_v0.CreateEvent(r.APIClient, r.APIServer, event); err != nil {
		return fmt.Errorf("failed to create event: %w", err)
	}
	return nil
}

// HandleEventOverride records the given event unless the error is an
// ErrWithEvent, in which case it records the event carried by the
// error. errors.As unwraps ErrWithEvent through wrapped error chains,
// so a handler that wraps the returned error still routes the specific
// event through the same substitution path. fullyQualifiedObjectType
// must be the form returned by GetFullyQualifiedType.
func (r *EventRecorder) HandleEventOverride(
	event *api.Event,
	objectId uint,
	fullyQualifiedObjectType string,
	err error,
	log *logr.Logger,
) {
	var errWithEvent *tp_errors.ErrWithEvent
	switch {
	case errors.As(err, &errWithEvent):
		if err := r.RecordEvent(
			&errWithEvent.Event,
			objectId,
			fullyQualifiedObjectType,
		); err != nil {
			log.Error(err, "failed to record event")
		}
	default:
		if err := r.RecordEvent(
			event,
			objectId,
			fullyQualifiedObjectType,
		); err != nil {
			log.Error(err, "failed to record event")
		}
	}
}

// GetSuccessReasonForOperation returns the default reason for the operation.
func GetSuccessReasonForOperation(operation notifications.NotificationOperation) string {
	switch operation {
	case notifications.NotificationOperationCreated:
		return ReasonSuccessfulCreate
	case notifications.NotificationOperationUpdated:
		return ReasonSuccessfulUpdate
	case notifications.NotificationOperationDeleted:
		return ReasonSuccessfulDelete
	default:
		return ""
	}
}
