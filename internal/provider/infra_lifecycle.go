package provider

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/go-logr/logr"
	"gorm.io/datatypes"
)

// LifecycleConfig holds the tunable parameters of the infra
// lifecycle state machine. Production uses the defaults; tests
// override via setLifecycleConfig.
type LifecycleConfig struct {
	StaleAckThreshold time.Duration
	RefreshInterval   time.Duration
	SemaphoreCapacity int
	PersistRetries    int
	PersistRetryDelay time.Duration
}

// defaultSemaphoreCapacity is the fallback concurrency cap applied when
// PULUMI_CONCURRENCY is unset, unparseable, or out of range.
const defaultSemaphoreCapacity = 20

// maxSemaphoreCapacity is the upper bound enforced on PULUMI_CONCURRENCY;
// values above this are clamped down with a warning log.
const maxSemaphoreCapacity = 100

// defaultLifecycleConfig is the production configuration.
var defaultLifecycleConfig = LifecycleConfig{
	StaleAckThreshold: 240 * time.Second,
	RefreshInterval:   60 * time.Second,
	SemaphoreCapacity: defaultSemaphoreCapacity,
	PersistRetries:    30,
	PersistRetryDelay: 10 * time.Second,
}

// lifecycleMu guards lifecycleConfig and infraSemaphore. Production sets
// them once at init and only reads, so the read lock is uncontended;
// tests swap them via setLifecycleConfig, and the lock keeps that swap
// from racing the background goroutines that read the values.
var lifecycleMu sync.RWMutex

// lifecycleConfig is the active configuration read by the lifecycle
// state machine.
var lifecycleConfig = defaultLifecycleConfig

// infraSemaphore caps the total number of concurrent infrastructure
// operations across every stack, guarding overall memory use when many
// distinct stacks reconcile at once. The capacity is initialized in init
// from lifecycleConfig.SemaphoreCapacity so PULUMI_CONCURRENCY overrides
// apply before any operation runs.
var infraSemaphore chan struct{}

// init reads the PULUMI_CONCURRENCY override, applies it to
// lifecycleConfig, and sizes infraSemaphore to match.
func init() {
	// PULUMI_CONCURRENCY caps how many concurrent pulumi stack operations may run per controller instance.
	lifecycleConfig.SemaphoreCapacity = resolveSemaphoreCapacity()
	infraSemaphore = make(chan struct{}, lifecycleConfig.SemaphoreCapacity)
}

// resolveSemaphoreCapacity reads PULUMI_CONCURRENCY and returns a valid
// concurrency cap. An unset or unparseable value falls back to the
// default; a value below 1 is raised to 1 with a warning; a value above
// maxSemaphoreCapacity is clamped down with a warning.
func resolveSemaphoreCapacity() int {
	raw := os.Getenv("PULUMI_CONCURRENCY")
	if raw == "" {
		return defaultSemaphoreCapacity
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		log.Printf("PULUMI_CONCURRENCY=%q is not a valid integer, using default %d", raw, defaultSemaphoreCapacity)
		return defaultSemaphoreCapacity
	}
	if parsed < 1 {
		log.Printf("PULUMI_CONCURRENCY=%d is below minimum, using 1", parsed)
		return 1
	}
	if parsed > maxSemaphoreCapacity {
		log.Printf("PULUMI_CONCURRENCY=%d exceeds maximum, using %d", parsed, maxSemaphoreCapacity)
		return maxSemaphoreCapacity
	}
	return parsed
}

// stackLocksMu guards stackLocks and its reference counts. The map is
// small, only holds entries for keys currently being operated on, and is
// pruned when a key's reference count drops to zero so a long-running
// controller does not accumulate stale entries.
var stackLocksMu sync.Mutex

// stackLocks holds one entry per stack key that has at least one active
// or waiting operation. Serializing acquisition on a per-key mutex
// prevents two operations on the same stack from launching concurrent
// deploys, which would race on the local pulumi state file and on the
// cloud backend's own stack lock.
var stackLocks = make(map[string]*stackLock)

// stackLock is the per-stack serialization primitive plus a reference
// count so the entry can be removed from stackLocks once the last waiter
// releases it.
type stackLock struct {
	mu       sync.Mutex
	refCount int
}

// tryAcquireStackLock atomically checks whether any operation is
// currently in flight for the given stack; if none, creates the entry
// and holds the lock in a single critical section so callers never
// block. Returns (nil, false) when another operation already owns the
// stack; the caller should treat that the same as a full-pool
// rejection and requeue. Every successful acquire must be paired with
// releaseStackLock so the entry is pruned.
func tryAcquireStackLock(key string) (*stackLock, bool) {
	stackLocksMu.Lock()
	defer stackLocksMu.Unlock()
	if _, ok := stackLocks[key]; ok {
		return nil, false
	}
	sl := &stackLock{refCount: 1}
	sl.mu.Lock()
	stackLocks[key] = sl
	return sl, true
}

// releaseStackLock releases the per-key mutex and removes the entry
// from stackLocks so a long-running controller does not accumulate
// dead entries for stacks it once operated on.
func releaseStackLock(key string, sl *stackLock) {
	sl.mu.Unlock()

	stackLocksMu.Lock()
	sl.refCount--
	if sl.refCount == 0 {
		delete(stackLocks, key)
	}
	stackLocksMu.Unlock()
}

// currentConfig returns the active lifecycle configuration.
func currentConfig() LifecycleConfig {
	lifecycleMu.RLock()
	defer lifecycleMu.RUnlock()
	return lifecycleConfig
}

// currentSemaphore returns the active semaphore channel. Callers that
// both acquire and release a slot must capture this once and reuse it,
// so the release lands on the same channel even if a test swaps the
// global in between.
func currentSemaphore() chan struct{} {
	lifecycleMu.RLock()
	defer lifecycleMu.RUnlock()
	return infraSemaphore
}

// setLifecycleConfig swaps the lifecycle tunables and re-creates the
// semaphore channel at the new capacity. Returns a restore func for
// t.Cleanup. Only for tests.
func setLifecycleConfig(c LifecycleConfig) (restore func()) {
	lifecycleMu.Lock()
	oldConfig := lifecycleConfig
	oldSemaphore := infraSemaphore
	lifecycleConfig = c
	infraSemaphore = make(chan struct{}, c.SemaphoreCapacity)
	lifecycleMu.Unlock()
	return func() {
		lifecycleMu.Lock()
		lifecycleConfig = oldConfig
		infraSemaphore = oldSemaphore
		lifecycleMu.Unlock()
	}
}

// Clock abstracts wall-clock reads for stale-ack logic so tests can
// inject a fixed or advanceable time.
type Clock interface{ Now() time.Time }

// realClock implements Clock using the system wall clock.
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// lifecycleClock is the clock used for stale-ack checks.
var lifecycleClock Clock = realClock{}

// setLifecycleClock swaps the lifecycle clock. Returns a restore func
// for t.Cleanup. Only for tests; not concurrency-safe, call before
// spawning goroutines.
func setLifecycleClock(c Clock) (restore func()) {
	oldClock := lifecycleClock
	lifecycleClock = c
	return func() {
		lifecycleClock = oldClock
	}
}

// inFlightOps counts infrastructure create and delete operations
// currently executing in background goroutines.
var inFlightOps int64

// inFlightCount returns the number of in-flight infrastructure
// operations. Only for tests.
func inFlightCount() int64 { return atomic.LoadInt64(&inFlightOps) }

// ReconciliationSnapshot captures the reconciliation timestamps and resource
// inventory for a provider instance at a point in time. This decouples the
// lifecycle handler from any specific API object type.
type ReconciliationSnapshot struct {
	CreationAcknowledged *time.Time
	CreationConfirmed    *time.Time
	CreationFailed       bool
	DeletionScheduled    *time.Time
	DeletionAcknowledged *time.Time
	DeletionConfirmed    *time.Time
	DeletionFailed       bool
	ResourceInventory    *datatypes.JSON
}

// InfraLifecycleProvider defines the provider-specific operations needed by the
// HandleInfraCreate and HandleInfraDelete state machines. Each infrastructure
// provider implements this interface; the lifecycle handler does everything
// else (ack/confirm checks, stale detection, goroutine wiring).
type InfraLifecycleProvider interface {
	// StackKey returns a stable identifier for the backing infrastructure
	// stack. The lifecycle handler serializes create and delete operations
	// per key so two reconciles for the same stack cannot run concurrent
	// deploys and race on shared state (pulumi state file, cloud API rate
	// limits, or state-lock contention).
	StackKey() string

	// GetReconciliation fetches the latest reconciliation state and resource
	// inventory from the API.
	GetReconciliation() (*ReconciliationSnapshot, error)

	// BuildInfra constructs the InfraProvider implementor for this provider.
	BuildInfra() (InfraProvider, error)

	// IsCreateComplete checks whether the async create operation has finished.
	IsCreateComplete() (bool, error)

	// OnCreateConfirmed performs provider-specific post-creation work.
	OnCreateConfirmed(infra InfraProvider) error

	// SaveCreateOutputs saves provider-specific outputs and final state.
	SaveCreateOutputs(infra InfraProvider, state *datatypes.JSON) error

	// OnDeleteConfirmed performs provider-specific post-deletion cleanup.
	OnDeleteConfirmed(infra InfraProvider) error

	// AckCreation sets CreationAcknowledged and clears CreationFailed in the API.
	AckCreation() error

	// RefreshCreationAck updates CreationAcknowledged to prevent stale detection.
	RefreshCreationAck() error

	// SetCreationFailed marks CreationFailed=true in the API.
	SetCreationFailed() error

	// ConfirmCreation sets CreationConfirmed and Reconciled=true in the API.
	ConfirmCreation() error

	// AckDeletion sets DeletionAcknowledged in the API.
	AckDeletion() error

	// RefreshDeletionAck updates DeletionAcknowledged to prevent stale detection.
	RefreshDeletionAck() error

	// SetDeletionFailed marks DeletionFailed=true in the API.
	SetDeletionFailed() error

	// ConfirmDeletion sets DeletionConfirmed in the API.
	ConfirmDeletion() error

	// SaveState persists intermediate state to the API for crash recovery.
	SaveState(state *datatypes.JSON) error

	// ClearInventory sets ResourceInventory to "{}" to signal destroy complete.
	ClearInventory() error

	// PublishCreateNotification publishes a NATS notification for creation.
	PublishCreateNotification() error

	// PublishDeleteNotification publishes a NATS notification for deletion.
	PublishDeleteNotification() error
}

// HandleInfraCreate implements the create state machine for any infrastructure
// provider. It checks reconciliation state, manages ack/confirm transitions,
// and launches the create goroutine when needed.
func HandleInfraCreate(p InfraLifecycleProvider, log *logr.Logger) (int64, error) {
	// fetch latest state from API
	snap, err := p.GetReconciliation()
	if err != nil {
		return 0, fmt.Errorf("failed to get reconciliation state: %w", err)
	}

	// check if already reconciled
	if snap.CreationConfirmed != nil {
		return 0, nil
	}

	// check if previously acknowledged and not failed
	if snap.CreationAcknowledged != nil && !snap.CreationFailed {
		// check if creation is complete
		complete, err := p.IsCreateComplete()
		if err != nil {
			return 0, fmt.Errorf("failed to check create completion: %w", err)
		}

		if complete {
			// re-fetch immediately before the post-creation work so a
			// confirmation that landed since the first fetch (a concurrent
			// reconcile or another replica) short-circuits here. The
			// post-creation work is not idempotent: it mints a fresh
			// connection token and flips the runtime instance back to
			// unreconciled, so it must run at most once per confirmation.
			confirmSnap, err := p.GetReconciliation()
			if err != nil {
				return 0, fmt.Errorf("failed to re-check reconciliation before confirmation: %w", err)
			}
			if confirmSnap.CreationConfirmed != nil {
				return 0, nil
			}

			// build infra for post-creation work (e.g., GetConnection)
			infra, err := p.BuildInfra()
			if err != nil {
				return 0, fmt.Errorf("failed to build infra for create confirmation: %w", err)
			}

			// perform provider-specific post-creation work, then confirm
			// creation as the very next call so the crash window between the
			// two is as narrow as possible
			if err := p.OnCreateConfirmed(infra); err != nil {
				return 0, fmt.Errorf("failed to run post-creation work: %w", err)
			}

			// confirm creation
			if err := p.ConfirmCreation(); err != nil {
				return 0, fmt.Errorf("failed to confirm creation: %w", err)
			}

			log.Info("creation confirmed")
			return 0, nil
		}

		// not complete yet — check if acknowledgement is stale
		if !checkStaleAck(*snap.CreationAcknowledged) {
			return 120, nil
		}
	}

	// one of the following is true:
	// 1. creation has not been acknowledged — new create request
	// 2. creation has previously failed — time to retry
	// 3. the last acknowledgement is stale — creation was interrupted

	// check if deletion was scheduled before acknowledging creation. The
	// acknowledgement must come after this check: a fresh acknowledgement
	// written ahead of a scheduled delete would trip the delete handler's
	// cross-replica guard and stall the delete for the full stale-ack
	// window.
	snap, err = p.GetReconciliation()
	if err != nil {
		return 0, fmt.Errorf("failed to check deletion status before create: %w", err)
	}
	if snap.DeletionScheduled != nil {
		log.Info("deletion scheduled, aborting create to let delete handler proceed")
		return 0, nil
	}

	// acknowledge creation
	if err := p.AckCreation(); err != nil {
		return 0, fmt.Errorf("failed to acknowledge creation: %w", err)
	}

	// build infra
	infra, err := p.BuildInfra()
	if err != nil {
		return 0, fmt.Errorf("failed to build infra for create: %w", err)
	}

	// wire callbacks and launch goroutine
	callbacks := infraCallbacks{
		RefreshAck:     p.RefreshCreationAck,
		SaveState:      p.SaveState,
		PersistFailure: p.SetCreationFailed,
		OnSuccess: func(state *datatypes.JSON) error {
			// save provider-specific outputs
			if err := p.SaveCreateOutputs(infra, state); err != nil {
				return fmt.Errorf("failed to save create outputs: %w", err)
			}

			// check if deletion was scheduled during the create operation
			latestSnap, err := p.GetReconciliation()
			if err == nil && latestSnap.DeletionScheduled != nil {
				log.Info("deletion was scheduled during create, skipping create notification to let delete proceed")
				return nil
			}

			// publish notification
			if err := p.PublishCreateNotification(); err != nil {
				log.Error(err, "failed to publish create notification")
			}

			return nil
		},
	}

	return launchInfraCreate(infraConfig{
		StackKey:      p.StackKey(),
		Infra:         infra,
		ExistingState: snap.ResourceInventory,
		Callbacks:     callbacks,
		Log:           log,
	})
}

// HandleInfraDelete implements the delete state machine for any infrastructure
// provider. It checks reconciliation state, manages ack/confirm transitions,
// handles cross-replica safety, and launches the delete goroutine when needed.
func HandleInfraDelete(p InfraLifecycleProvider, log *logr.Logger) (int64, error) {
	// fetch latest state from API
	snap, err := p.GetReconciliation()
	if err != nil {
		return 0, fmt.Errorf("failed to get reconciliation state: %w", err)
	}

	// validate that deletion is scheduled
	if snap.DeletionScheduled == nil {
		return 0, errors.New("deletion notification received but not scheduled")
	}

	// check if already confirmed
	if snap.DeletionConfirmed != nil {
		return 0, nil
	}

	// cross-replica safety: if a create operation is still in progress on
	// another replica, requeue to let it finish
	if snap.CreationAcknowledged != nil &&
		!checkStaleAck(*snap.CreationAcknowledged) &&
		snap.CreationConfirmed == nil {
		log.Info("create operation still in progress, requeueing delete")
		return 60, nil
	}

	// check if previously acknowledged and not failed; a failed destroy
	// falls through to re-acknowledge and re-launch immediately rather than
	// waiting for the acknowledgement to go stale
	if snap.DeletionAcknowledged != nil && !snap.DeletionFailed {
		// re-fetch to check if inventory has been cleared
		latestSnap, err := p.GetReconciliation()
		if err != nil {
			return 0, fmt.Errorf("failed to check deletion status: %w", err)
		}

		if inventoryCleared(latestSnap.ResourceInventory) {
			// refresh ack to prevent stale detection during cleanup
			if err := p.RefreshDeletionAck(); err != nil {
				log.Error(err, "failed to refresh deletion ack during cleanup")
			}

			// build infra for post-deletion cleanup
			infra, err := p.BuildInfra()
			if err != nil {
				return 0, fmt.Errorf("failed to build infra for delete confirmation: %w", err)
			}

			// perform provider-specific post-deletion cleanup
			if err := p.OnDeleteConfirmed(infra); err != nil {
				log.Error(err, "failed to run post-deletion cleanup, will retry")
				return 60, nil
			}

			// confirm deletion
			if err := p.ConfirmDeletion(); err != nil {
				return 0, fmt.Errorf("failed to confirm deletion: %w", err)
			}

			log.Info("deletion confirmed")
			return 0, nil
		}

		// resources not yet destroyed — check if ack is stale
		if checkStaleAck(*snap.DeletionAcknowledged) {
			log.Info("deletion acknowledgement is stale, re-launching delete goroutine")
			// fall through to re-launch
		} else {
			return 60, nil
		}
	}

	// one of the following is true:
	// 1. deletion has not been acknowledged: new delete request
	// 2. deletion has previously failed: time to retry
	// 3. the last acknowledgement is stale: deletion was interrupted

	// acknowledge deletion
	if err := p.AckDeletion(); err != nil {
		return 0, fmt.Errorf("failed to acknowledge deletion: %w", err)
	}

	// build infra
	infra, err := p.BuildInfra()
	if err != nil {
		return 0, fmt.Errorf("failed to build infra for delete: %w", err)
	}

	// re-fetch for latest resource inventory
	snap, err = p.GetReconciliation()
	if err != nil {
		return 0, fmt.Errorf("failed to get resource inventory for delete: %w", err)
	}

	// wire callbacks and launch goroutine
	callbacks := infraCallbacks{
		RefreshAck:     p.RefreshDeletionAck,
		SaveState:      p.SaveState,
		PersistFailure: p.SetDeletionFailed,
		OnSuccess: func(_ *datatypes.JSON) error {
			// clear inventory to signal destroy complete
			if err := p.ClearInventory(); err != nil {
				log.Error(err, "failed to clear resource inventory after deletion")
			}

			// publish notification
			if err := p.PublishDeleteNotification(); err != nil {
				log.Error(err, "failed to publish delete notification")
			}

			return nil
		},
	}

	return launchInfraDelete(infraConfig{
		StackKey:      p.StackKey(),
		Infra:         infra,
		ExistingState: snap.ResourceInventory,
		Callbacks:     callbacks,
		Log:           log,
	})
}

// infraCallbacks contains callback functions invoked at various points during
// create and delete goroutine lifecycles.
type infraCallbacks struct {
	// RefreshAck updates the acknowledged timestamp to prevent stale detection
	// while the operation is still running.
	RefreshAck func() error

	// SaveState persists intermediate state to the API for crash recovery.
	SaveState func(state *datatypes.JSON) error

	// PersistFailure marks the operation as failed in the API so the
	// reconciler knows to retry promptly. Create operations set
	// CreationFailed; delete operations set DeletionFailed.
	PersistFailure func() error

	// OnSuccess is called after successful infrastructure create or delete.
	// For create, state contains the final Pulumi state; for delete, state is nil.
	OnSuccess func(state *datatypes.JSON) error
}

// infraConfig contains all parameters needed to launch an infrastructure
// create or delete operation in a background goroutine.
type infraConfig struct {
	// StackKey identifies the backing stack for per-key serialization.
	// Two operations sharing a key cannot launch concurrent deploys.
	StackKey string

	// Infra is the provider's infrastructure object that implements InfraProvider.
	Infra InfraProvider

	// ExistingState is the previously saved state from ResourceInventory.
	// When non-nil and non-empty, state is restored before operating.
	ExistingState *datatypes.JSON

	// Callbacks contains functions invoked during the operation.
	Callbacks infraCallbacks

	// Log is the structured logger for the operation.
	Log *logr.Logger
}

// checkStaleAck returns true if the given acknowledgement timestamp has gone
// stale, indicating the operation was interrupted (e.g. pod restart).
func checkStaleAck(ackTimestamp time.Time) bool {
	duration := lifecycleClock.Now().UTC().Sub(ackTimestamp)
	return duration > currentConfig().StaleAckThreshold
}

// launchInfraCreate tries the per-stack lock non-blockingly, then the
// global cap, and only launches when both succeed. A second call for
// the same stack while its deploy is still running is rejected the same
// as a full pool and requeued at 30, so the reconciler never blocks and
// two reconciles cannot race concurrent deploys on the same stack.
// Returns a requeue delay for the reconciler.
func launchInfraCreate(config infraConfig) (int64, error) {
	// reject non-blockingly if the stack already has an operation in
	// flight; the running goroutine will release the lock when it
	// finishes and the next reconcile pass can pick it up. The log is
	// at debug level so a poll-heavy reconciler does not spam it once
	// per rejected reconcile
	sl, ok := tryAcquireStackLock(config.StackKey)
	if !ok {
		config.Log.V(1).Info("stack operation already in flight, requeuing")
		return 30, nil
	}

	// acquire the global concurrency cap; capture the channel once so the
	// release lands on the same channel even if a test swaps the global.
	// On rejection, release the per-stack lock here since no goroutine will
	// pick up its release
	sem := currentSemaphore()
	select {
	case sem <- struct{}{}:
		// acquired slot
	default:
		releaseStackLock(config.StackKey, sl)
		config.Log.V(1).Info("infrastructure worker pool full, requeuing")
		return 30, nil
	}

	// launch creation in background goroutine; the goroutine releases the
	// per-stack lock when the deploy returns, so a queued caller waiting on
	// the same key unblocks only after this deploy is done
	go func() {
		defer releaseStackLock(config.StackKey, sl)
		defer func() { <-sem }()
		defer func() {
			if r := recover(); r != nil {
				config.Log.Error(fmt.Errorf("panic: %v", r), "recovered panic in infrastructure create goroutine")
				persistFailure(config.Callbacks.PersistFailure, config.Log)
			}
		}()
		executeInfraCreate(config)
	}()

	return 120, nil
}

// launchInfraDelete tries the per-stack lock non-blockingly, then the
// global cap, and only launches when both succeed. A second call for
// the same stack while its destroy is still running is rejected the
// same as a full pool and requeued at 30, so the reconciler never
// blocks and two reconciles cannot race concurrent destroys on the
// same stack. Returns a requeue delay for the reconciler.
func launchInfraDelete(config infraConfig) (int64, error) {
	// reject non-blockingly if the stack already has an operation in
	// flight; the running goroutine will release the lock when it
	// finishes and the next reconcile pass can pick it up
	sl, ok := tryAcquireStackLock(config.StackKey)
	if !ok {
		config.Log.Info("stack operation already in flight, requeuing")
		return 30, nil
	}

	// acquire the global concurrency cap; capture the channel once so the
	// release lands on the same channel even if a test swaps the global.
	// On rejection, release the per-stack lock here since no goroutine will
	// pick up its release
	sem := currentSemaphore()
	select {
	case sem <- struct{}{}:
		// acquired slot
	default:
		releaseStackLock(config.StackKey, sl)
		config.Log.Info("infrastructure worker pool full, requeuing")
		return 30, nil
	}

	// launch deletion in background goroutine; the goroutine releases the
	// per-stack lock when the destroy returns, so a queued caller waiting on
	// the same key unblocks only after this destroy is done
	go func() {
		defer releaseStackLock(config.StackKey, sl)
		defer func() { <-sem }()
		defer func() {
			if r := recover(); r != nil {
				config.Log.Error(fmt.Errorf("panic: %v", r), "recovered panic in infrastructure delete goroutine")
				persistFailure(config.Callbacks.PersistFailure, config.Log)
			}
		}()
		executeInfraDelete(config)
	}()

	return 300, nil
}

// executeInfraCreate runs the full infrastructure create lifecycle in a
// goroutine. It handles state restoration, optional streaming for providers
// that support it, and captures final state on success or failure.
func executeInfraCreate(config infraConfig) {
	// track in-flight operations for observability in tests
	atomic.AddInt64(&inFlightOps, 1)
	defer atomic.AddInt64(&inFlightOps, -1)

	// refresh the creation acknowledgement until this function returns
	quitAck := make(chan bool, 1)
	go refreshAck(config.Callbacks.RefreshAck, quitAck, config.Log)
	defer func() { quitAck <- true }()

	// restore state from ResourceInventory if available (retry after failure
	// or pod restart so the provider knows about previously created resources)
	if hasExistingState(config.ExistingState) {
		if err := config.Infra.SetStackState(config.ExistingState); err != nil {
			config.Log.Error(err, "failed to restore stack state for retry")
			persistFailure(config.Callbacks.PersistFailure, config.Log)
			return
		}
		config.Log.Info("restored state from database for creation retry")

		// refresh state to sync with cloud reality if provider supports it
		if refreshable, ok := config.Infra.(RefreshableProvider); ok {
			if err := refreshable.RefreshStack(); err != nil {
				config.Log.Error(err, "failed to refresh stack state")
				persistFailure(config.Callbacks.PersistFailure, config.Log)
				return
			}
			config.Log.Info("refreshed stack state against cloud reality")
		}
	}

	// start state streaming if provider supports it
	var quitStream chan bool
	streamStopped := false
	if streamable, ok := config.Infra.(StreamableProvider); ok {
		quitStream = make(chan bool, 1)
		go streamState(streamable, config.Callbacks.SaveState, quitStream, config.Log)
		defer func() {
			if !streamStopped {
				quitStream <- true
			}
		}()
	}

	// create infrastructure
	err := config.Infra.DeployInfra()

	// stop the stream watcher before capturing final state to prevent
	// a late fsnotify event from overwriting the authoritative state
	if quitStream != nil && !streamStopped {
		quitStream <- true
		streamStopped = true
	}

	if err != nil {
		config.Log.Error(err, "failed to create infrastructure")

		// capture state even on failure so retries can restore it and
		// avoid creating duplicate cloud resources
		stateJSON, stateErr := config.Infra.GetStackState()
		if stateErr != nil {
			config.Log.Error(stateErr, "failed to get stack state after failed creation")
		} else if stateJSON != nil {
			if saveErr := config.Callbacks.SaveState(stateJSON); saveErr != nil {
				config.Log.Error(saveErr, "failed to save partial state after failed creation")
			}
		}

		// classify the error: a transient error is expected to clear on
		// the next reconcile pass, so leave CreationFailed unset and let
		// the natural requeue re-fire. Flipping CreationFailed=true on a
		// transient error widens the reconciler into a permanent-failure
		// path that short-circuits subsequent reconciles.
		if isTransientPulumiError(err) {
			config.Log.Info("treating create error as transient; deferring to next reconcile pass")
			return
		}

		persistFailure(config.Callbacks.PersistFailure, config.Log)
		return
	}

	// capture final state
	stateJSON, err := config.Infra.GetStackState()
	if err != nil {
		config.Log.Error(err, "failed to get stack state after creation")
		persistFailure(config.Callbacks.PersistFailure, config.Log)
		return
	}

	// verify state integrity before declaring success
	if err := verifyState(stateJSON, config.Log); err != nil {
		config.Log.Error(err, "state verification failed after creation")
		persistFailure(config.Callbacks.PersistFailure, config.Log)
		return
	}

	// call provider-specific success handler
	if err := config.Callbacks.OnSuccess(stateJSON); err != nil {
		config.Log.Error(err, "failed to execute success callback")
	}
}

// executeInfraDelete runs the full infrastructure delete lifecycle in a
// goroutine. It handles state restoration, optional refresh for providers
// that support it, and captures updated state on failure.
func executeInfraDelete(config infraConfig) {
	// track in-flight operations for observability in tests
	atomic.AddInt64(&inFlightOps, 1)
	defer atomic.AddInt64(&inFlightOps, -1)

	// refresh the deletion acknowledgement until this function returns
	quitAck := make(chan bool, 1)
	go refreshAck(config.Callbacks.RefreshAck, quitAck, config.Log)
	defer func() { quitAck <- true }()

	// restore state from ResourceInventory if available so the provider
	// knows which cloud resources to destroy
	if hasExistingState(config.ExistingState) {
		// validate state JSON before restoring — corrupt/truncated state from
		// a partial fsnotify write would cause SetStackState to fail
		if !json.Valid(*config.ExistingState) {
			config.Log.Error(
				fmt.Errorf("existing state is not valid JSON (%d bytes)", len(*config.ExistingState)),
				"skipping state restoration for delete, will attempt destroy without state",
			)
		} else {
			if err := config.Infra.SetStackState(config.ExistingState); err != nil {
				config.Log.Error(err, "failed to restore stack state for delete, proceeding without state")
			} else {
				config.Log.Info("restored state from database for deletion")

				// refresh state to sync with cloud reality if provider supports it
				if refreshable, ok := config.Infra.(RefreshableProvider); ok {
					if err := refreshable.RefreshStack(); err != nil {
						config.Log.Error(err, "failed to refresh stack state before delete, proceeding with destroy")
					} else {
						config.Log.Info("refreshed stack state against cloud reality")
					}
				}
			}
		}
	}

	// destroy infrastructure
	if err := config.Infra.DestroyInfra(); err != nil {
		config.Log.Error(err, "failed to delete infrastructure, will retry on next reconciliation")

		// capture updated state so retries know which resources remain
		stateJSON, stateErr := config.Infra.GetStackState()
		if stateErr != nil {
			config.Log.Error(stateErr, "failed to get stack state after failed deletion")
		} else if stateJSON != nil {
			if saveErr := config.Callbacks.SaveState(stateJSON); saveErr != nil {
				config.Log.Error(saveErr, "failed to save state after failed deletion")
			}
		}

		// persist the failure so the next reconciliation retries promptly
		// instead of waiting for the acknowledgement to go stale
		persistFailure(config.Callbacks.PersistFailure, config.Log)
		return
	}

	// call provider-specific success handler
	if err := config.Callbacks.OnSuccess(nil); err != nil {
		config.Log.Error(err, "failed to execute delete success callback")
	}
}

// streamState watches the state file via fsnotify and pushes changes to
// the API. On every Write/Create event it reads the file and compares
// its bytes against the last bytes it successfully saved; equal reads
// skip the API call. Only writes that actually change the state file
// trigger a PATCH, which keeps the persisted state in tight sync with
// the file without triggering an API PATCH per fsnotify tick during a
// pulumi run. Pulumi rewrites its state file many times with unchanged
// content and each redundant PATCH republishes an update notification
// that wakes the reconciler; skipping the equal writes breaks that
// self-triggering loop while adding no polling cost. Only called for
// providers that implement StreamableProvider.
func streamState(
	provider StreamableProvider,
	saveState func(state *datatypes.JSON) error,
	quit chan bool,
	log *logr.Logger,
) {
	// get state file path and pre-create directory
	stateFilePath, err := provider.GetStateFilePath()
	if err != nil {
		log.Error(err, "failed to get state file path for streaming")
		return
	}
	stateDir := filepath.Dir(stateFilePath)
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		log.Error(err, "failed to create state directory for watcher")
		return
	}

	// create fsnotify watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Error(err, "failed to create fsnotify watcher")
		return
	}
	defer watcher.Close()

	// watch the directory containing the state file
	if err := watcher.Add(stateDir); err != nil {
		log.Error(err, "failed to add directory to watcher")
		return
	}

	stateFileName := filepath.Base(stateFilePath)

	// lastSaved holds the bytes we most recently pushed via saveState.
	// This goroutine is the only writer for this stack's state field so
	// the cache is authoritative without polling the API.
	var lastSaved []byte

	for {
		select {
		case <-quit:
			return

		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			// only react to write/create events for the state file
			if filepath.Base(event.Name) != stateFileName {
				continue
			}
			if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) {
				continue
			}

			// read and upload state immediately
			state, err := provider.ReadStateFile()
			if err != nil {
				log.Error(err, "failed to read state file during streaming")
				continue
			}
			if state == nil {
				continue
			}

			// validate JSON before uploading to prevent partial writes
			// from overwriting good state in the database
			if !json.Valid(*state) {
				log.V(1).Info("skipping partial state file write (invalid JSON)")
				continue
			}

			// skip the save when the file bytes match the last bytes
			// we already persisted from this goroutine; pulumi
			// rewrites the state file many times per resource op with
			// identical content, and each unnecessary save
			// republishes an update notification that wakes the
			// reconciler
			if bytes.Equal([]byte(*state), lastSaved) {
				continue
			}

			// push state via callback
			if err := saveState(state); err != nil {
				log.Error(err, "failed to update resource inventory during state streaming")
				continue
			}
			lastSaved = append(lastSaved[:0], (*state)...)

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Error(err, "fsnotify watcher error")
		}
	}
}

// refreshAck calls the provided refresh function on the configured refresh
// interval until told to quit, preventing stale acknowledgement detection
// from re-launching the operation while it is still running.
func refreshAck(
	refresh func() error,
	quitChan chan bool,
	log *logr.Logger,
) {
	for {
		select {
		case <-quitChan:
			return
		case <-time.After(currentConfig().RefreshInterval):
			if err := refresh(); err != nil {
				log.Error(err, "failed to refresh acknowledged timestamp")
			}
		}
	}
}

// persistFailure calls the provided persist function to mark the operation as
// failed. If the call fails, it is retried on the configured delay up to the
// configured retry count. After exhausting retries, the goroutine returns and
// stale ack detection will recover the operation.
func persistFailure(
	persist func() error,
	log *logr.Logger,
) {
	cfg := currentConfig()
	maxRetries := cfg.PersistRetries
	for attempt := 0; attempt < maxRetries; attempt++ {
		if err := persist(); err != nil {
			log.Error(err, "failed to persist operation failure, retrying after delay",
				"attempt", attempt+1, "maxRetries", maxRetries)
			time.Sleep(cfg.PersistRetryDelay)
			continue
		}
		return
	}
	log.Error(
		fmt.Errorf("exhausted %d retries", maxRetries),
		"failed to persist operation failure, stale ack detection will recover",
	)
}

// verifyState checks the integrity of a state JSON object to confirm it is a
// recognizable, well-formed deployment record. It assumes Pulumi-stack-backed
// state and inspects the two on-disk Pulumi schemas (checkpoint and
// deployment); a backend with a different state layout would need its own
// verification. A stack whose resource list is present but empty is treated as
// a legitimately empty stack and passes, so a deployment that creates no
// resources does not retry forever; only state that matches neither schema is
// rejected.
func verifyState(state *datatypes.JSON, log *logr.Logger) error {
	if state == nil {
		return fmt.Errorf("state is nil")
	}
	if len(*state) == 0 {
		return fmt.Errorf("state is empty")
	}

	// parse as generic JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal(*state, &parsed); err != nil {
		return fmt.Errorf("state is not valid JSON: %w", err)
	}

	// look for a resources list in either Pulumi schema:
	// - checkpoint format: checkpoint.latest.resources
	// - deployment format: deployment.resources
	// recognizedSchema records whether the state carried a resources list at
	// all, distinguishing a legitimately empty stack from state that does not
	// match a known Pulumi layout.
	recognizedSchema := false
	resourceCount := 0
	if checkpoint, ok := parsed["checkpoint"].(map[string]interface{}); ok {
		if latest, ok := checkpoint["latest"].(map[string]interface{}); ok {
			if resources, ok := latest["resources"].([]interface{}); ok {
				recognizedSchema = true
				resourceCount = len(resources)
			}
		}
	}
	if resourceCount == 0 {
		if deployment, ok := parsed["deployment"].(map[string]interface{}); ok {
			if resources, ok := deployment["resources"].([]interface{}); ok {
				recognizedSchema = true
				resourceCount = len(resources)
			}
		}
	}

	// state that matches neither schema is unrecognized and rejected; an empty
	// resource list under a recognized schema is a valid empty stack
	if !recognizedSchema {
		return fmt.Errorf("state does not match a known Pulumi stack schema")
	}

	log.Info("state verification passed", "resourceCount", resourceCount)
	return nil
}

// inventoryCleared returns true if the ResourceInventory is nil, empty,
// or contains only "{}" or "null".
func inventoryCleared(inventory *datatypes.JSON) bool {
	return inventory == nil ||
		len(*inventory) == 0 ||
		string(*inventory) == "{}" ||
		string(*inventory) == "null"
}

// hasExistingState returns true if the state is non-nil, non-empty, and not
// a placeholder value ("{}" or "null").
func hasExistingState(state *datatypes.JSON) bool {
	return state != nil &&
		len(*state) > 0 &&
		string(*state) != "{}" &&
		string(*state) != "null"
}
