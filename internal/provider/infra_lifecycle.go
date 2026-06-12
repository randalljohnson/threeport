package provider

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	"gorm.io/datatypes"
)

// Requeue intervals and the per-step deadline that bound the observe model.
// Each reconcile observes cloud reality, optionally kicks one bounded step,
// then asks the reconciler to requeue after one of these intervals so the next
// reconcile re-observes. There is no liveness clock: progress is read from the
// cloud on every pass, not inferred from a stamped timestamp.
const (
	// requeueProvisioning is the delay between reconciles while a create is in
	// flight, giving the kicked step time to advance before the next observe.
	requeueProvisioning = 60

	// requeueDeleting is the delay between reconciles while a destroy is in
	// flight.
	requeueDeleting = 60

	// requeueAfterFailure is the delay before retrying after a kicked apply or
	// destroy step returns an error. The partial state is persisted first so
	// the retry resumes against what already exists.
	requeueAfterFailure = 60

	// stepDeadline bounds a single Apply or Destroy kick so a stuck cloud
	// operation cannot pin a reconcile indefinitely; the step returns and the
	// next observe picks up where it left off.
	stepDeadline = 240 * time.Second
)

// infraSemaphore caps how many infrastructure provisioning steps run at once
// across this process, so a controller reconciling many runtimes in parallel
// cannot launch an unbounded number of memory-heavy cloud operations and
// exhaust its memory. A reconcile that cannot acquire a slot does not block: it
// requeues and tries again on the next pass. Capacity is fixed once at
// controller startup via SetInfraConcurrency; until then it defaults to a
// single slot.
var infraSemaphore = make(chan struct{}, 1)

// inFlight tracks how many infrastructure steps are executing right now, so the
// concurrency cap can be observed at any instant. It is incremented after a slot
// is acquired and decremented before the slot is released.
var inFlight int64

// SetInfraConcurrency sizes the infrastructure concurrency semaphore. It must be
// called once during controller startup, before any reconcilers launch, since
// the channel capacity is fixed at creation and re-making it is only safe while
// no goroutine is using it. A cap below 1 is clamped to 1. It is a no-op in
// effect for controllers that never run the infrastructure lifecycle, since the
// semaphore is only acquired on a provisioning step.
func SetInfraConcurrency(cap int) {
	if cap < 1 {
		cap = 1
	}
	infraSemaphore = make(chan struct{}, cap)
}

// acquireInfraSlot tries to take a semaphore slot without blocking. It reports
// whether a slot was acquired; on success the caller must call releaseInfraSlot
// exactly once when its step finishes. A failed acquire means the cap is full,
// and the caller requeues rather than waiting.
func acquireInfraSlot() bool {
	select {
	case infraSemaphore <- struct{}{}:
		atomic.AddInt64(&inFlight, 1)
		return true
	default:
		return false
	}
}

// releaseInfraSlot returns a previously acquired slot and decrements the
// in-flight count. It must be called exactly once per successful acquire.
func releaseInfraSlot() {
	atomic.AddInt64(&inFlight, -1)
	<-infraSemaphore
}

// inFlightCount reports how many infrastructure steps are executing right now.
func inFlightCount() int64 {
	return atomic.LoadInt64(&inFlight)
}

// emptyInventory is the placeholder written to the resource inventory once a
// destroy is confirmed, signalling that no cloud resources remain.
var emptyInventory = datatypes.JSON([]byte("{}"))

// ReconciliationSnapshot is the lifecycle-relevant projection of an API object.
// It carries only durable lifecycle facts: whether create and delete are
// confirmed, whether a delete is scheduled, whether the last step failed, and
// the persisted state. Phase is never stored here; it comes from Observe.
type ReconciliationSnapshot struct {
	// Reconciled is true once create is confirmed and the object is settled.
	Reconciled bool

	// CreationConfirmed is set once Observe first reported PhaseReady.
	CreationConfirmed *time.Time

	// CreationFailed records that the last apply step ended terminally.
	CreationFailed bool

	// DeletionScheduled is set by the API when the object is marked for delete.
	DeletionScheduled *time.Time

	// DeletionConfirmed is set once Observe first reported PhaseAbsent.
	DeletionConfirmed *time.Time

	// ResourceInventory is the persisted stack state for crash recovery and
	// idempotent resume.
	ResourceInventory *datatypes.JSON
}

// InfraLifecycleProvider is the provider-specific surface the HandleInfraCreate
// and HandleInfraDelete state machines call. The handler owns all phase logic,
// requeue timing, and reconciliation writes; the provider supplies only what is
// genuinely provider-specific: how to read state, how to build the infra
// object, post-create/post-delete hooks, and notifications.
type InfraLifecycleProvider interface {
	// GetReconciliation fetches the current lifecycle projection from the API.
	GetReconciliation() (*ReconciliationSnapshot, error)

	// UpdateReconciliation persists the next reconciliation snapshot in a
	// single write. The handler passes only the fields it intends to change.
	UpdateReconciliation(snapshot ReconciliationSnapshot) error

	// BuildInfra constructs the InfraProvider implementor for this provider.
	BuildInfra() (InfraProvider, error)

	// OnCreateConfirmed performs provider-specific post-creation work, e.g.
	// fetching connection info and updating the runtime instance.
	OnCreateConfirmed(infra InfraProvider) error

	// OnDeleteConfirmed performs provider-specific post-deletion cleanup.
	OnDeleteConfirmed(infra InfraProvider) error

	// PublishCreateNotification publishes a NATS notification for creation.
	PublishCreateNotification() error

	// PublishDeleteNotification publishes a NATS notification for deletion.
	PublishDeleteNotification() error
}

// HandleInfraCreate implements the create state machine for any infrastructure
// provider in the observe-and-requeue model. Each reconcile observes cloud
// reality, kicks one bounded apply step only when resources are absent or the
// last step failed, persists the refreshed state, and requeues. Phase is read
// from the cloud on every pass, so any replica computes the same next action
// and a retry resumes against existing state rather than provisioning a
// duplicate. The context carries a per-step deadline; the call site signature
// is unchanged so the reconciler scaffolds do not need editing.
func HandleInfraCreate(p InfraLifecycleProvider, log *logr.Logger) (int64, error) {
	ctx := context.Background()

	// fetch latest lifecycle projection from API
	snap, err := p.GetReconciliation()
	if err != nil {
		return 0, fmt.Errorf("failed to get reconciliation state: %w", err)
	}

	// terminal: create already confirmed and settled
	if snap.CreationConfirmed != nil {
		return 0, nil
	}

	// a delete raced in front of this create; yield to the delete handler
	if snap.DeletionScheduled != nil {
		log.Info("deletion scheduled, yielding to delete handler")
		return 0, nil
	}

	// build the provider's infra object and restore any persisted state so the
	// observe below refreshes against resources an earlier process created
	infra, err := p.BuildInfra()
	if err != nil {
		return 0, fmt.Errorf("failed to build infra for create: %w", err)
	}
	if hasExistingState(snap.ResourceInventory) {
		if err := infra.SetStackState(snap.ResourceInventory); err != nil {
			return 0, fmt.Errorf("failed to restore stack state: %w", err)
		}
	}

	// observe: refresh against the cloud and derive the current phase. A
	// refresh error surfaces here and we requeue without kicking, so a transient
	// failure is never mistaken for absent infrastructure.
	obs, err := infra.Observe(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to observe infrastructure: %w", err)
	}

	switch obs.Phase {
	case PhaseReady:
		// resources exist and are healthy; run post-creation work, then confirm
		if err := p.OnCreateConfirmed(infra); err != nil {
			return 0, fmt.Errorf("failed to run post-creation work: %w", err)
		}
		now := time.Now().UTC()
		if err := p.UpdateReconciliation(ReconciliationSnapshot{
			ResourceInventory: obs.State,
			CreationConfirmed: &now,
			Reconciled:        true,
			CreationFailed:    false,
		}); err != nil {
			return 0, fmt.Errorf("failed to confirm creation: %w", err)
		}
		if err := p.PublishCreateNotification(); err != nil {
			log.Error(err, "failed to publish create notification")
		}
		log.Info("creation confirmed")
		return 0, nil

	case PhaseProvisioning:
		// a create is already in flight; persist the refreshed state and requeue
		if err := p.UpdateReconciliation(ReconciliationSnapshot{
			ResourceInventory: obs.State,
		}); err != nil {
			return 0, fmt.Errorf("failed to persist provisioning state: %w", err)
		}
		return requeueProvisioning, nil

	default:
		// PhaseAbsent or PhaseFailed: kick one bounded apply step. Apply is
		// idempotent against the refreshed state, so this resumes a partial
		// create rather than starting a second one.

		// honor the global infrastructure concurrency cap: if every slot is
		// taken, requeue without kicking so another reconcile can make progress
		// first. The next pass re-observes and tries again.
		if !acquireInfraSlot() {
			return requeueProvisioning, nil
		}
		defer releaseInfraSlot()

		stepCtx, cancel := context.WithTimeout(ctx, stepDeadline)
		defer cancel()

		if err := infra.Apply(stepCtx); err != nil {
			// keep whatever partial state exists so the retry can resume
			state, stateErr := infra.GetStackState()
			if stateErr != nil {
				log.Error(stateErr, "failed to capture state after failed apply step")
			}
			if updateErr := p.UpdateReconciliation(ReconciliationSnapshot{
				ResourceInventory: state,
				CreationFailed:    true,
			}); updateErr != nil {
				log.Error(updateErr, "failed to persist state after failed apply step")
			}
			log.Error(err, "apply step failed, will retry")
			return requeueAfterFailure, nil
		}

		// persist the state the step produced and requeue to observe progress
		state, stateErr := infra.GetStackState()
		if stateErr != nil {
			log.Error(stateErr, "failed to capture state after apply step")
		}
		if err := p.UpdateReconciliation(ReconciliationSnapshot{
			ResourceInventory: state,
			CreationFailed:    false,
		}); err != nil {
			return 0, fmt.Errorf("failed to persist state after apply step: %w", err)
		}
		return requeueProvisioning, nil
	}
}

// HandleInfraDelete implements the delete state machine for any infrastructure
// provider in the observe-and-requeue model. Each reconcile observes cloud
// reality, kicks one bounded destroy step only while resources remain,
// persists the refreshed state, and requeues. Deletion is confirmed only on an
// observed PhaseAbsent that came from a successful refresh, so a refresh error
// never clears the inventory while resources still exist.
func HandleInfraDelete(p InfraLifecycleProvider, log *logr.Logger) (int64, error) {
	ctx := context.Background()

	// fetch latest lifecycle projection from API
	snap, err := p.GetReconciliation()
	if err != nil {
		return 0, fmt.Errorf("failed to get reconciliation state: %w", err)
	}

	// a delete notification only makes sense once the API has scheduled it
	if snap.DeletionScheduled == nil {
		return 0, errors.New("deletion notification received but not scheduled")
	}

	// terminal: delete already confirmed
	if snap.DeletionConfirmed != nil {
		return 0, nil
	}

	// build the provider's infra object and restore any persisted state so the
	// observe below knows which cloud resources to tear down
	infra, err := p.BuildInfra()
	if err != nil {
		return 0, fmt.Errorf("failed to build infra for delete: %w", err)
	}
	if hasExistingState(snap.ResourceInventory) {
		if err := infra.SetStackState(snap.ResourceInventory); err != nil {
			return 0, fmt.Errorf("failed to restore stack state: %w", err)
		}
	}

	// observe: refresh against the cloud and derive the current phase. A
	// refresh error surfaces here and we requeue without confirming, so a
	// transient failure never clears the inventory prematurely.
	obs, err := infra.Observe(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to observe infrastructure: %w", err)
	}

	switch obs.Phase {
	case PhaseAbsent:
		// no cloud resources remain; run post-deletion cleanup, then confirm
		if err := p.OnDeleteConfirmed(infra); err != nil {
			log.Error(err, "failed to run post-deletion cleanup, will retry")
			return requeueDeleting, nil
		}
		now := time.Now().UTC()
		if err := p.UpdateReconciliation(ReconciliationSnapshot{
			ResourceInventory: &emptyInventory,
			DeletionConfirmed: &now,
		}); err != nil {
			return 0, fmt.Errorf("failed to confirm deletion: %w", err)
		}
		if err := p.PublishDeleteNotification(); err != nil {
			log.Error(err, "failed to publish delete notification")
		}
		log.Info("deletion confirmed")
		return 0, nil

	case PhaseDeleting:
		// a destroy is already in flight; persist the refreshed state and requeue
		if err := p.UpdateReconciliation(ReconciliationSnapshot{
			ResourceInventory: obs.State,
		}); err != nil {
			return 0, fmt.Errorf("failed to persist deleting state: %w", err)
		}
		return requeueDeleting, nil

	default:
		// PhaseReady, PhaseProvisioning, or PhaseFailed: resources still present,
		// kick one bounded destroy step. Destroy is idempotent against the
		// refreshed state, so this resumes a partial teardown.

		// honor the global infrastructure concurrency cap: if every slot is
		// taken, requeue without kicking so another reconcile can make progress
		// first. The next pass re-observes and tries again.
		if !acquireInfraSlot() {
			return requeueDeleting, nil
		}
		defer releaseInfraSlot()

		stepCtx, cancel := context.WithTimeout(ctx, stepDeadline)
		defer cancel()

		if err := infra.Destroy(stepCtx); err != nil {
			// keep whatever state remains so the retry knows what is left
			state, stateErr := infra.GetStackState()
			if stateErr != nil {
				log.Error(stateErr, "failed to capture state after failed destroy step")
			}
			if updateErr := p.UpdateReconciliation(ReconciliationSnapshot{
				ResourceInventory: state,
			}); updateErr != nil {
				log.Error(updateErr, "failed to persist state after failed destroy step")
			}
			log.Error(err, "destroy step failed, will retry")
			return requeueAfterFailure, nil
		}

		// persist the state the step produced and requeue to observe progress
		state, stateErr := infra.GetStackState()
		if stateErr != nil {
			log.Error(stateErr, "failed to capture state after destroy step")
		}
		if err := p.UpdateReconciliation(ReconciliationSnapshot{
			ResourceInventory: state,
		}); err != nil {
			return 0, fmt.Errorf("failed to persist state after destroy step: %w", err)
		}
		return requeueDeleting, nil
	}
}

// hasExistingState returns true if the state is non-nil, non-empty, and not a
// placeholder value ("{}" or "null"). It gates whether SetStackState is called
// before observing, so a fresh process restores resources an earlier process
// created.
func hasExistingState(state *datatypes.JSON) bool {
	return state != nil &&
		len(*state) > 0 &&
		string(*state) != "{}" &&
		string(*state) != "null"
}
