package v0

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/nats-io/nats.go"
)

// NotificationOperation informs a reconciler what operation was performed in
// the API to trigger the notification.
type NotificationOperation string

const (
	NotificationOperationCreated = "Created"
	NotificationOperationUpdated = "Updated"
	NotificationOperationDeleted = "Deleted"
)

// Notification is the message that is sent to NATS to alert a controller that a
// change has been made to an object.  A notification is sent by the API Server
// when a change is make by a client, or by a controller when reconciliation was
// not completed and it needs to be requeued.
type Notification struct {
	// The operation performed to trigger the notification.
	// One of:
	// * Created
	// * Updated
	// * Deleted
	Operation NotificationOperation

	// Tracks the backoff delay last used in a requeue so that it may
	// incremented or repeated (when at max delay) as appropriate.
	CreationTime *int64

	// The API object that has been changed.
	Object interface{}

	// The API object version.  This allows controller code to determine which
	// version of the object has been delivered in the notification payload so
	// as to process it properly.
	ObjectVersion string
}

// EnsureConsumer adds the JetStream consumer described by config, reconciling
// its settings when the consumer already exists so that initialization is
// idempotent across controller restarts. A consumer persists in JetStream
// independent of the pod, so a plain add on every start fails with
// ErrConsumerNameAlreadyInUse once the consumer has been created on a prior
// start; without the update path the live consumer keeps whatever settings it
// was originally created with even after the emitted config changes. When
// update returns success but the server silently discards fields it refuses to
// change in place, the live consumer is deleted and re-added once so the
// desired config actually takes effect.
func EnsureConsumer(
	js nats.JetStreamContext,
	stream string,
	config *nats.ConsumerConfig,
) (*nats.ConsumerInfo, error) {
	// add the consumer on first start
	info, err := js.AddConsumer(stream, config)
	if err == nil {
		return info, nil
	}

	// surface any add failure that is not the already-exists signal
	if !errors.Is(err, nats.ErrConsumerNameAlreadyInUse) {
		return nil, fmt.Errorf("failed to add consumer %s: %w", config.Durable, err)
	}

	// reconcile the live consumer with the emitted config
	updateInfo, updateErr := js.UpdateConsumer(stream, config)
	if updateErr != nil {
		return nil, fmt.Errorf("failed to update existing consumer %s: %w", config.Durable, updateErr)
	}

	// detect fields the server silently ignores on update
	drifted := driftedConsumerFields(&updateInfo.Config, config)
	if len(drifted) == 0 {
		return updateInfo, nil
	}

	// record the one-shot recreate so operators can trace adopted drift
	log.Printf(
		"recreated consumer to adopt drifted config consumer=%s stream=%s driftedFields=%v",
		config.Durable, stream, drifted,
	)

	// delete then re-add once so the drifted fields actually take effect
	if deleteErr := js.DeleteConsumer(stream, config.Durable); deleteErr != nil {
		return nil, fmt.Errorf("failed to delete drifted consumer %s: %w", config.Durable, deleteErr)
	}
	recreated, addErr := js.AddConsumer(stream, config)
	if addErr != nil {
		return nil, fmt.Errorf("failed to re-add consumer %s after drift recreate: %w", config.Durable, addErr)
	}
	return recreated, nil
}

// backoffEqual() reports whether two backoff schedules match element for
// element, since a nil and an empty slice are equivalent for NATS.
func backoffEqual(a, b []time.Duration) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// driftedConsumerFields() returns the names of consumer config fields whose
// live value differs from the desired value across the set NATS commonly
// ignores on UpdateConsumer.
func driftedConsumerFields(live, desired *nats.ConsumerConfig) []string {
	var drifted []string
	if !backoffEqual(live.BackOff, desired.BackOff) {
		drifted = append(drifted, "BackOff")
	}
	if live.DeliverPolicy != desired.DeliverPolicy {
		drifted = append(drifted, "DeliverPolicy")
	}
	if live.FilterSubject != desired.FilterSubject {
		drifted = append(drifted, "FilterSubject")
	}
	if live.MaxDeliver != desired.MaxDeliver {
		drifted = append(drifted, "MaxDeliver")
	}
	return drifted
}

// ConsumeMessage generates a Notificatiion object from a json notification from
// NATS to a controller.
func ConsumeMessage(msgData []byte) (*Notification, error) {
	var notif Notification
	decoder := json.NewDecoder(bytes.NewReader(msgData))
	decoder.UseNumber()
	if err := decoder.Decode(&notif); err != nil {
		return nil, fmt.Errorf("failed to decode notification json from NATS message data: %w", err)
	}

	return &notif, nil
}
