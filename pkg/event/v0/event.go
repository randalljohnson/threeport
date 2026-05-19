package v0

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
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

// RecordEvent records a new event with the given information.
func (r *EventRecorder) RecordEvent(
	event *api.Event,
	objectId uint,
	objectVersion string,
	objectType string,
) error {
	formatString := "reason=%s&note=%s&type=%s&objectid=%d"
	formatArgs := []any{
		url.QueryEscape(*event.Reason),
		url.QueryEscape(*event.Note),
		url.QueryEscape(*event.Type),
		objectId,
	}

	query := fmt.Sprintf(formatString, formatArgs...)
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
		createdEvent, err := client_v0.CreateEvent(r.APIClient, r.APIServer, event)
		if err != nil {
			return fmt.Errorf("failed to create event: %w", err)
		}

		// link the event to its base object via an attached object reference.
		// modules pass a qualified ObjectType ("<api-namespace>/<version>.<Type>")
		// from obj.GetType(); core call sites pass a bare type name and rely
		// on version+type concatenation. detect the qualified form to avoid
		// double-prefixing.
		storedObjectType := objectType
		if !strings.Contains(objectType, "/") {
			storedObjectType = fmt.Sprintf("%s.%s", objectVersion, objectType)
		}
		if _, err := client_v0.CreateAttachedObjectReference(
			r.APIClient,
			r.APIServer,
			&api.AttachedObjectReference{
				ObjectType:         util.Ptr(storedObjectType),
				ObjectID:           util.Ptr(objectId),
				AttachedObjectType: util.Ptr(util.TypeName(api.Event{})),
				AttachedObjectID:   createdEvent.ID,
				Relationship:       util.Ptr(api.RelationshipDescribes),
			},
		); err != nil {
			return fmt.Errorf("failed to create attached object reference for event: %w", err)
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

// HandleEventOverride records the specified event
// unless the provided error is an ErrWithEvent,
// in which case it records the event provided
func (r *EventRecorder) HandleEventOverride(
	event *api.Event,
	objectId uint,
	objectVersion string,
	objectType string,
	err error,
	log *logr.Logger,
) {
	var errWithEvent *tp_errors.ErrWithEvent
	switch {
	case errors.As(err, &errWithEvent):
		if err := r.RecordEvent(
			&errWithEvent.Event,
			objectId,
			objectVersion,
			objectType,
		); err != nil {
			log.Error(err, "failed to record event")
		}
	default:
		if err := r.RecordEvent(
			event,
			objectId,
			objectVersion,
			objectType,
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
