package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/go-logr/logr"
	"gorm.io/datatypes"
)

// staleAckDurationSeconds is the threshold after which an acknowledgement
// timestamp is considered stale, indicating the operation was interrupted.
const staleAckDurationSeconds = 240

// infraSemaphore limits concurrent infrastructure operations to prevent OOM
// from too many simultaneous deployments.
var infraSemaphore = make(chan struct{}, 5)

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
	ResourceInventory    *datatypes.JSON
}

// InfraLifecycleConfig contains all provider-specific closures needed by the
// HandleInfraCreate and HandleInfraDelete state machines. A provider fills
// in the closures for its specific behavior; the lifecycle handler does
// everything else (ack/confirm checks, stale detection, goroutine wiring,
// NATS notifications).
type InfraLifecycleConfig struct {
	// GetReconciliation fetches the latest reconciliation state and resource
	// inventory from the API. Called at the start of each handler invocation
	// and again during deletion-during-create checks.
	GetReconciliation func() (*ReconciliationSnapshot, error)

	// BuildInfra constructs the InfraProvider implementor for this provider.
	// Called once per handler invocation before launching the goroutine.
	BuildInfra func() (InfraProvider, error)

	// IsCreateComplete checks whether the async create operation has finished
	// (e.g., ClusterOCID is set for OKE). Called during the confirmation
	// check when CreationAcknowledged is set but CreationConfirmed is not.
	IsCreateComplete func() (bool, error)

	// OnCreateConfirmed performs provider-specific post-creation work after
	// the create operation has been confirmed complete (e.g., get connection
	// info, update dependent objects). Called before ConfirmCreation.
	OnCreateConfirmed func(infra InfraProvider) error

	// SaveCreateOutputs saves provider-specific outputs (e.g., ClusterOCID)
	// and the final state from the goroutine's OnSuccess callback.
	SaveCreateOutputs func(infra InfraProvider, state *datatypes.JSON) error

	// OnDeleteConfirmed performs provider-specific post-deletion cleanup
	// after resources have been destroyed and inventory cleared
	// (e.g., delete compartment, delete stack state).
	OnDeleteConfirmed func(infra InfraProvider) error

	// AckCreation sets CreationAcknowledged and clears CreationFailed in the API.
	AckCreation func() error

	// RefreshCreationAck updates CreationAcknowledged to prevent stale detection.
	RefreshCreationAck func() error

	// SetCreationFailed marks CreationFailed=true in the API.
	SetCreationFailed func() error

	// ConfirmCreation sets CreationConfirmed and Reconciled=true in the API.
	ConfirmCreation func() error

	// AckDeletion sets DeletionAcknowledged in the API.
	AckDeletion func() error

	// RefreshDeletionAck updates DeletionAcknowledged to prevent stale detection.
	RefreshDeletionAck func() error

	// ConfirmDeletion sets DeletionConfirmed in the API.
	ConfirmDeletion func() error

	// SaveState persists intermediate state to the API for crash recovery.
	SaveState func(state *datatypes.JSON) error

	// ClearInventory sets ResourceInventory to "{}" to signal destroy complete.
	ClearInventory func() error

	// PublishCreateNotification publishes a NATS notification for creation.
	PublishCreateNotification func() error

	// PublishDeleteNotification publishes a NATS notification for deletion.
	PublishDeleteNotification func() error

	// Log is the structured logger for the operation.
	Log *logr.Logger
}

// HandleInfraCreate implements the create state machine for any infrastructure
// provider. It checks reconciliation state, manages ack/confirm transitions,
// and launches the create goroutine when needed.
func HandleInfraCreate(config InfraLifecycleConfig) (int64, error) {
	// fetch latest state from API
	snap, err := config.GetReconciliation()
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
		complete, err := config.IsCreateComplete()
		if err != nil {
			return 0, fmt.Errorf("failed to check create completion: %w", err)
		}

		if complete {
			// build infra for post-creation work (e.g., GetConnection)
			infra, err := config.BuildInfra()
			if err != nil {
				return 0, fmt.Errorf("failed to build infra for create confirmation: %w", err)
			}

			// perform provider-specific post-creation work
			if err := config.OnCreateConfirmed(infra); err != nil {
				return 0, fmt.Errorf("failed to run post-creation work: %w", err)
			}

			// confirm creation
			if err := config.ConfirmCreation(); err != nil {
				return 0, fmt.Errorf("failed to confirm creation: %w", err)
			}

			config.Log.Info("creation confirmed")
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

	// acknowledge creation
	if err := config.AckCreation(); err != nil {
		return 0, fmt.Errorf("failed to acknowledge creation: %w", err)
	}

	// build infra
	infra, err := config.BuildInfra()
	if err != nil {
		return 0, fmt.Errorf("failed to build infra for create: %w", err)
	}

	// check if deletion was scheduled while we were preparing to create
	snap, err = config.GetReconciliation()
	if err != nil {
		return 0, fmt.Errorf("failed to check deletion status before create: %w", err)
	}
	if snap.DeletionScheduled != nil {
		config.Log.Info("deletion scheduled, aborting create to let delete handler proceed")
		return 0, nil
	}

	// wire callbacks and launch goroutine
	callbacks := infraCreateCallbacks{
		RefreshAck:     config.RefreshCreationAck,
		SaveState:      config.SaveState,
		PersistFailure: config.SetCreationFailed,
		OnSuccess: func(state *datatypes.JSON) error {
			// save provider-specific outputs
			if err := config.SaveCreateOutputs(infra, state); err != nil {
				return fmt.Errorf("failed to save create outputs: %w", err)
			}

			// check if deletion was scheduled during the create operation
			latestSnap, err := config.GetReconciliation()
			if err == nil && latestSnap.DeletionScheduled != nil {
				config.Log.Info("deletion was scheduled during create, skipping create notification to let delete proceed")
				return nil
			}

			// publish notification
			if err := config.PublishCreateNotification(); err != nil {
				config.Log.Error(err, "failed to publish create notification")
			}

			return nil
		},
	}

	return launchInfraCreate(infraCreateConfig{
		Infra:         infra,
		ExistingState: snap.ResourceInventory,
		Callbacks:     callbacks,
		Log:           config.Log,
	})
}

// HandleInfraDelete implements the delete state machine for any infrastructure
// provider. It checks reconciliation state, manages ack/confirm transitions,
// handles cross-replica safety, and launches the delete goroutine when needed.
func HandleInfraDelete(config InfraLifecycleConfig) (int64, error) {
	// fetch latest state from API
	snap, err := config.GetReconciliation()
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
		config.Log.Info("create operation still in progress, requeueing delete")
		return 60, nil
	}

	// check if previously acknowledged
	if snap.DeletionAcknowledged != nil {
		// re-fetch to check if inventory has been cleared
		latestSnap, err := config.GetReconciliation()
		if err != nil {
			return 0, fmt.Errorf("failed to check deletion status: %w", err)
		}

		if inventoryCleared(latestSnap.ResourceInventory) {
			// refresh ack to prevent stale detection during cleanup
			if err := config.RefreshDeletionAck(); err != nil {
				config.Log.Error(err, "failed to refresh deletion ack during cleanup")
			}

			// build infra for post-deletion cleanup
			infra, err := config.BuildInfra()
			if err != nil {
				return 0, fmt.Errorf("failed to build infra for delete confirmation: %w", err)
			}

			// perform provider-specific post-deletion cleanup
			if err := config.OnDeleteConfirmed(infra); err != nil {
				config.Log.Error(err, "failed to run post-deletion cleanup, will retry")
				return 60, nil
			}

			// confirm deletion
			if err := config.ConfirmDeletion(); err != nil {
				return 0, fmt.Errorf("failed to confirm deletion: %w", err)
			}

			config.Log.Info("deletion confirmed")
			return 0, nil
		}

		// resources not yet destroyed — check if ack is stale
		if checkStaleAck(*snap.DeletionAcknowledged) {
			config.Log.Info("deletion acknowledgement is stale, re-launching delete goroutine")
			// fall through to re-launch
		} else {
			return 60, nil
		}
	}

	// acknowledge deletion
	if err := config.AckDeletion(); err != nil {
		return 0, fmt.Errorf("failed to acknowledge deletion: %w", err)
	}

	// build infra
	infra, err := config.BuildInfra()
	if err != nil {
		return 0, fmt.Errorf("failed to build infra for delete: %w", err)
	}

	// re-fetch for latest resource inventory
	snap, err = config.GetReconciliation()
	if err != nil {
		return 0, fmt.Errorf("failed to get resource inventory for delete: %w", err)
	}

	// wire callbacks and launch goroutine
	callbacks := infraDeleteCallbacks{
		RefreshAck: config.RefreshDeletionAck,
		SaveState:  config.SaveState,
		OnSuccess: func() error {
			// clear inventory to signal destroy complete
			if err := config.ClearInventory(); err != nil {
				config.Log.Error(err, "failed to clear resource inventory after deletion")
			}

			// publish notification
			if err := config.PublishDeleteNotification(); err != nil {
				config.Log.Error(err, "failed to publish delete notification")
			}

			return nil
		},
	}

	return launchInfraDelete(infraDeleteConfig{
		Infra:         infra,
		ExistingState: snap.ResourceInventory,
		Callbacks:     callbacks,
		Log:           config.Log,
	})
}

// infraCreateCallbacks contains provider-specific callback functions invoked
// at various points during the create goroutine lifecycle.
type infraCreateCallbacks struct {
	// RefreshAck updates the creation acknowledged timestamp to prevent
	// stale detection while the operation is still running.
	RefreshAck func() error

	// SaveState persists intermediate state to the API for crash recovery.
	SaveState func(state *datatypes.JSON) error

	// PersistFailure marks the operation as failed in the API so the
	// reconciler knows to retry.
	PersistFailure func() error

	// OnSuccess is called after successful infrastructure creation with the
	// final state. The provider should save state, provider-specific fields,
	// and publish a NATS notification.
	OnSuccess func(state *datatypes.JSON) error
}

// infraCreateConfig contains all parameters needed to launch an infrastructure
// create operation in a background goroutine.
type infraCreateConfig struct {
	// Infra is the provider's infrastructure object that implements InfraProvider.
	Infra InfraProvider

	// ExistingState is the previously saved state from ResourceInventory.
	// When non-nil and non-empty, state is restored before creating.
	ExistingState *datatypes.JSON

	// Callbacks contains provider-specific functions invoked during the operation.
	Callbacks infraCreateCallbacks

	// Log is the structured logger for the operation.
	Log *logr.Logger
}

// infraDeleteCallbacks contains provider-specific callback functions invoked
// at various points during the delete goroutine lifecycle.
type infraDeleteCallbacks struct {
	// RefreshAck updates the deletion acknowledged timestamp to prevent
	// stale detection while the operation is still running.
	RefreshAck func() error

	// SaveState persists intermediate state to the API for crash recovery.
	SaveState func(state *datatypes.JSON) error

	// OnSuccess is called after successful infrastructure deletion. The provider
	// should clear ResourceInventory and publish a NATS notification.
	OnSuccess func() error
}

// infraDeleteConfig contains all parameters needed to launch an infrastructure
// delete operation in a background goroutine.
type infraDeleteConfig struct {
	// Infra is the provider's infrastructure object that implements InfraProvider.
	Infra InfraProvider

	// ExistingState is the previously saved state from ResourceInventory.
	// When non-nil and non-empty, state is restored before destroying so the
	// provider knows which cloud resources to delete.
	ExistingState *datatypes.JSON

	// Callbacks contains provider-specific functions invoked during the operation.
	Callbacks infraDeleteCallbacks

	// Log is the structured logger for the operation.
	Log *logr.Logger
}

// checkStaleAck returns true if the given acknowledgement timestamp has gone
// stale, indicating the operation was interrupted (e.g. pod restart).
func checkStaleAck(ackTimestamp time.Time) bool {
	duration := time.Now().UTC().Sub(ackTimestamp)
	return duration.Seconds() > staleAckDurationSeconds
}

// launchInfraCreate acquires the concurrency semaphore, then launches
// executeInfraCreate in a background goroutine. Returns a requeue delay
// for the reconciler.
func launchInfraCreate(config infraCreateConfig) (int64, error) {
	// acquire infrastructure concurrency semaphore
	select {
	case infraSemaphore <- struct{}{}:
		// acquired slot
	default:
		config.Log.Info("infrastructure worker pool full, requeuing")
		return 30, nil
	}

	// launch creation in background goroutine
	go func() {
		defer func() { <-infraSemaphore }()
		executeInfraCreate(config)
	}()

	return 120, nil
}

// launchInfraDelete acquires the concurrency semaphore, then launches
// executeInfraDelete in a background goroutine. Returns a requeue delay
// for the reconciler.
func launchInfraDelete(config infraDeleteConfig) (int64, error) {
	// acquire infrastructure concurrency semaphore
	select {
	case infraSemaphore <- struct{}{}:
		// acquired slot
	default:
		config.Log.Info("infrastructure worker pool full, requeuing")
		return 30, nil
	}

	// launch deletion in background goroutine
	go func() {
		defer func() { <-infraSemaphore }()
		executeInfraDelete(config)
	}()

	return 300, nil
}

// executeInfraCreate runs the full infrastructure create lifecycle in a
// goroutine. It handles state restoration, optional streaming for providers
// that support it, and captures final state on success or failure.
func executeInfraCreate(config infraCreateConfig) {
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
func executeInfraDelete(config infraDeleteConfig) {
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
		return
	}

	// call provider-specific success handler
	if err := config.Callbacks.OnSuccess(); err != nil {
		config.Log.Error(err, "failed to execute delete success callback")
	}
}

// streamState watches the state file via fsnotify and pushes changes to the
// API using the saveState callback on every Write/Create event. Only called
// for providers that implement StreamableProvider.
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

			// push state via callback
			if err := saveState(state); err != nil {
				log.Error(err, "failed to update resource inventory during state streaming")
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Error(err, "fsnotify watcher error")
		}
	}
}

// refreshAck calls the provided refresh function every 60 seconds until told
// to quit, preventing stale acknowledgement detection from re-launching the
// operation while it is still running.
func refreshAck(
	refresh func() error,
	quitChan chan bool,
	log *logr.Logger,
) {
	for {
		select {
		case <-quitChan:
			return
		case <-time.After(60 * time.Second):
			if err := refresh(); err != nil {
				log.Error(err, "failed to refresh acknowledged timestamp")
			}
		}
	}
}

// persistFailure calls the provided persist function to mark the operation as
// failed. If the call fails, it is retried every 10 seconds up to 30 times
// (5 minutes). After exhausting retries, the goroutine returns and stale ack
// detection will recover the operation.
func persistFailure(
	persist func() error,
	log *logr.Logger,
) {
	const maxRetries = 30
	for attempt := 0; attempt < maxRetries; attempt++ {
		if err := persist(); err != nil {
			log.Error(err, "failed to persist creation failure - retrying in 10 sec",
				"attempt", attempt+1, "maxRetries", maxRetries)
			time.Sleep(time.Second * 10)
			continue
		}
		return
	}
	log.Error(
		fmt.Errorf("exhausted %d retries", maxRetries),
		"failed to persist creation failure, stale ack detection will recover",
	)
}

// verifyState checks the integrity of a state JSON object to ensure it
// represents a valid deployment with resources.
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

	// check for resources in either format:
	// - checkpoint format: checkpoint.latest.resources
	// - deployment format: deployment.resources
	resourceCount := 0
	if checkpoint, ok := parsed["checkpoint"].(map[string]interface{}); ok {
		if latest, ok := checkpoint["latest"].(map[string]interface{}); ok {
			if resources, ok := latest["resources"].([]interface{}); ok {
				resourceCount = len(resources)
			}
		}
	}
	if resourceCount == 0 {
		if deployment, ok := parsed["deployment"].(map[string]interface{}); ok {
			if resources, ok := deployment["resources"].([]interface{}); ok {
				resourceCount = len(resources)
			}
		}
	}

	if resourceCount == 0 {
		return fmt.Errorf("state contains no resources")
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
