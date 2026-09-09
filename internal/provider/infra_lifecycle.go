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

// LifecycleConfig is the timing and capacity for infrastructure create
// and delete.
type LifecycleConfig struct {
	// The unrefreshed age after which the operation may be re-launched
	StaleAckThreshold time.Duration

	// The interval a running operation refreshes its acknowledgement
	RefreshInterval time.Duration

	// The maximum concurrent operations in this process; extras are requeued, not held
	SemaphoreCapacity int

	// The attempts to persist a creation failure
	PersistRetries int

	// The wait between creation-failure persist attempts
	PersistRetryDelay time.Duration
}

// defaultSemaphoreCapacity is the process-wide worker pool size when
// PULUMI_CONCURRENCY is unset or not an integer.
const defaultSemaphoreCapacity = 20

// maxSemaphoreCapacity is the upper bound applied to PULUMI_CONCURRENCY.
const maxSemaphoreCapacity = 100

// defaultLifecycleConfig is the production configuration.
var defaultLifecycleConfig = LifecycleConfig{
	StaleAckThreshold: 240 * time.Second,
	RefreshInterval:   60 * time.Second,
	SemaphoreCapacity: defaultSemaphoreCapacity,
	PersistRetries:    30,
	PersistRetryDelay: 10 * time.Second,
}

// lifecycleMu guards lifecycleConfig, infraSemaphore, and lifecycleClock.
// A write-lock swap cannot race readers in running goroutines.
var lifecycleMu sync.RWMutex

// lifecycleConfig is the live timing and capacity.
var lifecycleConfig = defaultLifecycleConfig

// infraSemaphore is the process-wide worker pool for infrastructure
// operations across every stack.
var infraSemaphore chan struct{}

// init sizes the worker pool from PULUMI_CONCURRENCY.
func init() {
	// set worker pool size from PULUMI_CONCURRENCY
	lifecycleConfig.SemaphoreCapacity = resolveSemaphoreCapacity()
	infraSemaphore = make(chan struct{}, lifecycleConfig.SemaphoreCapacity)
}

// resolveSemaphoreCapacity returns the worker pool size from PULUMI_CONCURRENCY.
// Unset or unparseable values use the default; values are clamped to [1, 100].
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

// stackLocksMu guards the map of in-flight stack locks.
var stackLocksMu sync.Mutex

// stackLocks records the exclusive lock held for each in-flight stack key.
// Two operations on one key would race on the local state file and on
// Pulumi's stack lock.
var stackLocks = make(map[string]*stackLock)

// stackLock is the in-process exclusive lock for one infrastructure stack.
// A second acquire for the same key fails immediately rather than blocking.
type stackLock struct {
	mu       sync.Mutex
	refCount int
}

// tryAcquireStackLock takes the exclusive in-process lock for key.
// It returns false without waiting when the key is already held.
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

// releaseStackLock drops the exclusive lock for key and forgets it
// when no references remain.
func releaseStackLock(key string, sl *stackLock) {
	sl.mu.Unlock()

	stackLocksMu.Lock()
	sl.refCount--
	if sl.refCount == 0 {
		delete(stackLocks, key)
	}
	stackLocksMu.Unlock()
}

// currentConfig returns the live lifecycle config.
func currentConfig() LifecycleConfig {
	lifecycleMu.RLock()
	defer lifecycleMu.RUnlock()
	return lifecycleConfig
}

// currentSemaphore returns the live infrastructure operation semaphore.
// Capture the channel once so a swap cannot move the release.
func currentSemaphore() chan struct{} {
	lifecycleMu.RLock()
	defer lifecycleMu.RUnlock()
	return infraSemaphore
}

// withDefaults returns a copy with each zero or negative field replaced
// by the production default. A zero refresh interval would spin the
// ack-refresh loop, a zero capacity would requeue every instance, and
// a zero retry count would skip persisting a failure.
func (c LifecycleConfig) withDefaults() LifecycleConfig {
	if c.StaleAckThreshold <= 0 {
		c.StaleAckThreshold = defaultLifecycleConfig.StaleAckThreshold
	}
	if c.RefreshInterval <= 0 {
		c.RefreshInterval = defaultLifecycleConfig.RefreshInterval
	}
	if c.SemaphoreCapacity <= 0 {
		c.SemaphoreCapacity = defaultLifecycleConfig.SemaphoreCapacity
	}
	if c.PersistRetries <= 0 {
		c.PersistRetries = defaultLifecycleConfig.PersistRetries
	}
	if c.PersistRetryDelay <= 0 {
		c.PersistRetryDelay = defaultLifecycleConfig.PersistRetryDelay
	}
	return c
}

// setLifecycleConfig applies defaults to c, installs it as the live config,
// rebuilds the semaphore at that capacity, and returns a restore function.
func setLifecycleConfig(c LifecycleConfig) (restore func()) {
	// ensure every field is set before installing
	c = c.withDefaults()

	// replace the live config and semaphore
	lifecycleMu.Lock()
	oldConfig := lifecycleConfig
	oldSemaphore := infraSemaphore
	lifecycleConfig = c
	infraSemaphore = make(chan struct{}, c.SemaphoreCapacity)
	lifecycleMu.Unlock()

	// put the previous config and semaphore back
	return func() {
		lifecycleMu.Lock()
		lifecycleConfig = oldConfig
		infraSemaphore = oldSemaphore
		lifecycleMu.Unlock()
	}
}

// Clock is a source of the current time for stale-ack checks.
type Clock interface{ Now() time.Time }

// realClock is a Clock that reads the wall clock.
type realClock struct{}

// Now returns the current wall-clock time.
func (realClock) Now() time.Time { return time.Now() }

// lifecycleClock is the live clock used for stale-ack checks.
var lifecycleClock Clock = realClock{}

// currentClock returns the live clock.
func currentClock() Clock {
	lifecycleMu.RLock()
	defer lifecycleMu.RUnlock()
	return lifecycleClock
}

// setLifecycleClock installs c as the live clock and returns a restore
// function that puts the previous clock back.
func setLifecycleClock(c Clock) (restore func()) {
	// replace the live clock
	lifecycleMu.Lock()
	oldClock := lifecycleClock
	lifecycleClock = c
	lifecycleMu.Unlock()

	// put the previous clock back
	return func() {
		lifecycleMu.Lock()
		lifecycleClock = oldClock
		lifecycleMu.Unlock()
	}
}

// inFlightOps counts create and delete operations running in goroutines.
var inFlightOps int64

// inFlightCount returns the running create and delete operation count.
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
	// StackKey returns a stable identity used to serialize operations
	// on one stack.
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
			// re-check confirmation so a confirmed create does not write another connection token
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

			// run post-creation work immediately before confirmation
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

	// check deletion status before ack; a fresh ack would stall the delete
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

			// publish create notification
			if err := p.PublishCreateNotification(); err != nil {
				return fmt.Errorf("failed to publish create notification: %w", err)
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

	// check if acknowledged and not failed; a failed destroy re-launches now
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

			// publish delete notification
			if err := p.PublishDeleteNotification(); err != nil {
				return fmt.Errorf("failed to publish delete notification: %w", err)
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

	// PersistFailure marks the operation failed in the API so the next
	// reconcile retries.
	PersistFailure func() error

	// OnSuccess is called after successful infrastructure create or delete.
	// For create, state contains the final Pulumi state; for delete, state is nil.
	OnSuccess func(state *datatypes.JSON) error
}

// infraConfig contains all parameters needed to launch an infrastructure
// create or delete operation in a background goroutine.
type infraConfig struct {
	// StackKey is the identity used to serialize operations on one stack.
	// Two operations sharing a key cannot run at once in this process.
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
	duration := currentClock().Now().UTC().Sub(ackTimestamp)
	return duration > currentConfig().StaleAckThreshold
}

// launchInfraCreate acquires a per-stack lock and a worker-pool slot, then
// starts the create in the background. Contention requeues in 30 seconds.
func launchInfraCreate(config infraConfig) (int64, error) {
	// acquire the per-stack lock without waiting
	sl, ok := tryAcquireStackLock(config.StackKey)
	if !ok {
		config.Log.V(1).Info("stack operation already in flight, requeuing")
		return 30, nil
	}

	// hold this channel so the release matches the acquire
	sem := currentSemaphore()
	select {
	case sem <- struct{}{}:
		// acquired slot
	default:
		releaseStackLock(config.StackKey, sl)
		config.Log.V(1).Info("infrastructure worker pool full, requeuing")
		return 30, nil
	}

	// run create in the background
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

// launchInfraDelete acquires a per-stack lock and a worker-pool slot, then
// starts the delete in the background. Contention requeues in 30 seconds.
func launchInfraDelete(config infraConfig) (int64, error) {
	// acquire the per-stack lock without waiting
	sl, ok := tryAcquireStackLock(config.StackKey)
	if !ok {
		config.Log.V(1).Info("stack operation already in flight, requeuing")
		return 30, nil
	}

	// hold this channel so the release matches the acquire
	sem := currentSemaphore()
	select {
	case sem <- struct{}{}:
		// acquired slot
	default:
		releaseStackLock(config.StackKey, sl)
		config.Log.V(1).Info("infrastructure worker pool full, requeuing")
		return 30, nil
	}

	// run delete in the background
	go func() {
		defer releaseStackLock(config.StackKey, sl)
		defer func() { <-sem }()
		// recover a panic without marking the operation failed
		defer func() {
			if r := recover(); r != nil {
				config.Log.Error(fmt.Errorf("panic: %v", r), "recovered panic in infrastructure delete goroutine")
				persistFailure(config.Callbacks.PersistFailure, config.Log)
			}
		}()
		executeInfraDelete(config)
	}()

	// requeue after 300 seconds while the delete runs
	return 300, nil
}

// executeInfraCreate runs the full infrastructure create lifecycle in a
// goroutine. It handles state restoration, optional streaming for providers
// that support it, and captures final state on success or failure.
func executeInfraCreate(config infraConfig) {
	// count this operation as in flight
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
				// mark creation failed and do not deploy
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
			// skip quit when the stream was already stopped
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

		// leave the failed flag unset so the next pass does not re-launch
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

	// run create success callbacks; a failure marks the operation failed
	if err := config.Callbacks.OnSuccess(stateJSON); err != nil {
		config.Log.Error(err, "failed to execute success callback")
		// mark the operation failed so the next reconcile retries
		persistFailure(config.Callbacks.PersistFailure, config.Log)
	}
}

// executeInfraDelete runs the full infrastructure delete lifecycle in a
// goroutine. It handles state restoration, optional refresh for providers
// that support it, and captures updated state on failure.
func executeInfraDelete(config infraConfig) {
	// count this operation as in flight
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

		persistFailure(config.Callbacks.PersistFailure, config.Log)
		return
	}

	// run delete success callbacks; a failure marks the operation failed
	if err := config.Callbacks.OnSuccess(nil); err != nil {
		config.Log.Error(err, "failed to execute delete success callback")
		persistFailure(config.Callbacks.PersistFailure, config.Log)
	}
}

// streamState watches the provider's state file and persists complete JSON for crash recovery.
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

	// lastSaved holds the last uploaded payload so unchanged writes are skipped
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

			// skip a write that matches the last uploaded payload
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

// refreshAck calls refresh every RefreshInterval until quitChan is signaled.
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

// persistFailure calls persist to mark the operation failed, retrying on
// PersistRetryDelay up to PersistRetries times. Exhausted retries leave
// recovery to stale-ack detection.
func persistFailure(
	persist func() error,
	log *logr.Logger,
) {
	cfg := currentConfig()
	maxRetries := cfg.PersistRetries

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		lastErr = persist()
		if lastErr == nil {
			return
		}

		if attempt == maxRetries-1 {
			// skip sleep after the last attempt
			break
		}

		log.Error(lastErr, "failed to persist operation failure, retrying",
			"attempt", attempt+1, "maxRetries", maxRetries,
			"retryDelay", cfg.PersistRetryDelay)
		time.Sleep(cfg.PersistRetryDelay)
	}

	exhausted := fmt.Errorf("exhausted %d retries", maxRetries)
	if lastErr != nil {
		exhausted = fmt.Errorf("exhausted %d retries: %w", maxRetries, lastErr)
	}
	log.Error(
		exhausted,
		"failed to persist operation failure, stale ack detection will recover",
	)
}

// verifyState accepts Pulumi checkpoint and deployment JSON.
// An empty resource list passes so a stack that created nothing does not retry forever.
func verifyState(state *datatypes.JSON, log *logr.Logger) error {
	// reject a missing state
	if state == nil {
		return errors.New("state is nil")
	}
	if len(*state) == 0 {
		return errors.New("state is empty")
	}

	// parse as generic JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal(*state, &parsed); err != nil {
		return fmt.Errorf("state is not valid JSON: %w", err)
	}

	// recognizedSchema distinguishes an empty stack from an unknown layout
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

	// reject JSON that is not a Pulumi stack snapshot
	if !recognizedSchema {
		return errors.New("state does not match a known Pulumi stack schema")
	}

	// log the verified resource count
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
