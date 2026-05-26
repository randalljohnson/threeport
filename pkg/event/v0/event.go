package v0

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/go-logr/logr"
	apilib "github.com/threeport/threeport/pkg/api/lib/v0"
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

// RecordEvent records a new event for the given object.
// fullyQualifiedObjectType must be the form returned by
// GetFullyQualifiedType.
func (r *EventRecorder) RecordEvent(
	event *api.Event,
	objectId uint,
	fullyQualifiedObjectType string,
) error {
	// the events-join-attached-object-references endpoint requires
	// objecttypename when filtering by objectid; split the qualified
	// type into its parts and pass all three alongside the id
	namespace, version, typeName, ok := apilib.ParseQualifiedType(fullyQualifiedObjectType)
	if !ok {
		return fmt.Errorf("invalid fully qualified object type %q", fullyQualifiedObjectType)
	}
	query := fmt.Sprintf(
		"reason=%s&note=%s&type=%s&objectid=%d&objecttypename=%s&objectnamespace=%s&objectversion=%s",
		url.QueryEscape(*event.Reason),
		url.QueryEscape(*event.Note),
		url.QueryEscape(*event.Type),
		objectId,
		url.QueryEscape(typeName),
		url.QueryEscape(namespace),
		url.QueryEscape(version),
	)
	events, err := client_v0.GetEventsJoinAttachedObjectReferenceByQueryString(
		r.APIClient,
		r.APIServer,
		query,
	)
	if err != nil {
		return fmt.Errorf("failed to get events by object id %d: %w", objectId, err)
	}

	switch len(*events) {
	case 0:
		event.ReportingController = &r.ReportingController
		event.EventTime = util.Ptr(time.Now())
		event.LastObservedTime = util.Ptr(time.Now())
		event.Count = util.Ptr(uint(1))

		// carry the subject info on the in-memory Event so the API
		// server's BeforeCreate validates and AfterCreate writes the
		// matching AttachedObjectReference in the same transaction.
		// gorm:"-" keeps these fields off the row; the AOR is the on-disk
		// source of truth for the subject linkage.
		event.ObjectType = util.Ptr(fullyQualifiedObjectType)
		event.ObjectID = util.Ptr(objectId)

		if _, err := client_v0.CreateEvent(r.APIClient, r.APIServer, event); err != nil {
			return fmt.Errorf("failed to create event: %w", err)
		}
	case 1:
		event = &(*events)[0]
		event.Count = util.Ptr(uint((*event.Count + 1)))
		event.LastObservedTime = util.Ptr(time.Now())
		// clear projection fields; UpdateEvent() rejects them as unsupported
		event.ObjectType = nil
		event.ObjectID = nil
		event.ObjectName = nil
		_, err := client_v0.UpdateEvent(r.APIClient, r.APIServer, event)
		if err != nil {
			return fmt.Errorf("failed to update event: %w", err)
		}
	default:
		return fmt.Errorf("unexpected number of events found: %d", len(*events))
	}

	return nil
}

// HandleEventOverride records the given event unless the error is an
// ErrWithEvent, in which case it records the event carried by the
// error. fullyQualifiedObjectType must be the form returned by
// GetFullyQualifiedType.
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
