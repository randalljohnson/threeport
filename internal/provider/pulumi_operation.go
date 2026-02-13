package provider

import (
	"encoding/json"
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

// pulumiSemaphore limits concurrent Pulumi operations to prevent OOM from
// too many simultaneous infrastructure deployments.
var pulumiSemaphore = make(chan struct{}, 5)

// PulumiInfra defines the interface each provider's infrastructure object
// must implement to use the shared Pulumi operation orchestration.
type PulumiInfra interface {
	DeployInfra() error
	DestroyInfra() error
	SetStackState(state *datatypes.JSON) error
	GetStackState() (*datatypes.JSON, error)
	ReadStateFile() (*datatypes.JSON, error)
	GetStateFilePath() (string, error)
	RefreshStack() error
}

// PulumiCreateCallbacks contains provider-specific callback functions invoked
// at various points during the create goroutine lifecycle.
type PulumiCreateCallbacks struct {
	// RefreshAck updates the creation acknowledged timestamp to prevent
	// stale detection while the operation is still running.
	RefreshAck func() error

	// SaveState persists intermediate Pulumi state to the API for crash recovery.
	SaveState func(state *datatypes.JSON) error

	// PersistFailure marks the operation as failed in the API so the
	// reconciler knows to retry.
	PersistFailure func() error

	// OnSuccess is called after successful infrastructure creation with the
	// final Pulumi state. The provider should save state, provider-specific
	// fields, and publish a NATS notification.
	OnSuccess func(state *datatypes.JSON) error
}

// PulumiCreateConfig contains all parameters needed to launch a Pulumi create
// operation in a background goroutine.
type PulumiCreateConfig struct {
	// Infra is the provider's infrastructure object that implements PulumiInfra.
	Infra PulumiInfra

	// ExistingState is the previously saved Pulumi state from ResourceInventory.
	// When non-nil and non-empty, state is restored before creating.
	ExistingState *datatypes.JSON

	// Callbacks contains provider-specific functions invoked during the operation.
	Callbacks PulumiCreateCallbacks

	// Log is the structured logger for the operation.
	Log *logr.Logger
}

// PulumiDeleteCallbacks contains provider-specific callback functions invoked
// at various points during the delete goroutine lifecycle.
type PulumiDeleteCallbacks struct {
	// RefreshAck updates the deletion acknowledged timestamp to prevent
	// stale detection while the operation is still running.
	RefreshAck func() error

	// SaveState persists intermediate Pulumi state to the API for crash recovery.
	SaveState func(state *datatypes.JSON) error

	// OnSuccess is called after successful infrastructure deletion. The provider
	// should clear ResourceInventory and publish a NATS notification.
	OnSuccess func() error
}

// PulumiDeleteConfig contains all parameters needed to launch a Pulumi delete
// operation in a background goroutine.
type PulumiDeleteConfig struct {
	// Infra is the provider's infrastructure object that implements PulumiInfra.
	Infra PulumiInfra

	// ExistingState is the previously saved Pulumi state from ResourceInventory.
	// When non-nil and non-empty, state is restored before destroying so Pulumi
	// knows which cloud resources to delete.
	ExistingState *datatypes.JSON

	// Callbacks contains provider-specific functions invoked during the operation.
	Callbacks PulumiDeleteCallbacks

	// Log is the structured logger for the operation.
	Log *logr.Logger
}

// LaunchPulumiCreate acquires the operation guard and concurrency semaphore,
// then launches executePulumiCreate in a background goroutine. Returns a
// requeue delay for the reconciler.
func LaunchPulumiCreate(config PulumiCreateConfig) (int64, error) {
	// acquire Pulumi concurrency semaphore
	select {
	case pulumiSemaphore <- struct{}{}:
		// acquired slot
	default:
		config.Log.Info("Pulumi worker pool full, requeuing")
		return 30, nil
	}

	// launch creation in background goroutine
	go func() {
		defer func() { <-pulumiSemaphore }()
		executePulumiCreate(config)
	}()

	return 120, nil
}

// LaunchPulumiDelete acquires the operation guard and concurrency semaphore,
// then launches executePulumiDelete in a background goroutine. Returns a
// requeue delay for the reconciler.
func LaunchPulumiDelete(config PulumiDeleteConfig) (int64, error) {
	// acquire Pulumi concurrency semaphore
	select {
	case pulumiSemaphore <- struct{}{}:
		// acquired slot
	default:
		config.Log.Info("Pulumi worker pool full, requeuing")
		return 30, nil
	}

	// launch deletion in background goroutine
	go func() {
		defer func() { <-pulumiSemaphore }()
		executePulumiDelete(config)
	}()

	return 300, nil
}

// CheckStaleAck returns true if the given acknowledgement timestamp has gone
// stale, indicating the operation was interrupted (e.g. pod restart).
func CheckStaleAck(ackTimestamp time.Time) bool {
	duration := time.Now().UTC().Sub(ackTimestamp)
	return duration.Seconds() > staleAckDurationSeconds
}

// executePulumiCreate runs the full Pulumi create lifecycle in a goroutine.
func executePulumiCreate(config PulumiCreateConfig) {
	// refresh the creation acknowledgement until this function returns
	quitAck := make(chan bool, 1)
	go refreshAck(config.Callbacks.RefreshAck, quitAck, config.Log)
	defer func() { quitAck <- true }()

	// restore Pulumi state from ResourceInventory if available (retry after
	// failure or pod restart so Pulumi knows about previously created resources)
	if config.ExistingState != nil &&
		len(*config.ExistingState) > 0 &&
		string(*config.ExistingState) != "{}" &&
		string(*config.ExistingState) != "null" {
		if err := config.Infra.SetStackState(config.ExistingState); err != nil {
			config.Log.Error(err, "failed to restore Pulumi stack state for retry")
			persistFailure(config.Callbacks.PersistFailure, config.Log)
			return
		}
		config.Log.Info("restored Pulumi state from database for creation retry")

		// refresh stack to sync state with cloud reality and clear stale
		// pending operations from interrupted runs
		if err := config.Infra.RefreshStack(); err != nil {
			config.Log.Error(err, "failed to refresh Pulumi stack state")
			persistFailure(config.Callbacks.PersistFailure, config.Log)
			return
		}
		config.Log.Info("refreshed Pulumi stack state against cloud reality")
	}

	// start state streaming via fsnotify
	quitStream := make(chan bool, 1)
	streamStopped := false
	go streamState(config.Infra, config.Callbacks.SaveState, quitStream, config.Log)
	defer func() {
		if !streamStopped {
			quitStream <- true
		}
	}()

	// create infrastructure
	err := config.Infra.DeployInfra()

	// stop the stream watcher before capturing final state to prevent
	// a late fsnotify event from overwriting the authoritative state
	quitStream <- true
	streamStopped = true

	if err != nil {
		config.Log.Error(err, "failed to create infrastructure")

		// capture Pulumi state even on failure so retries can restore it
		// and avoid creating duplicate cloud resources
		stateJSON, stateErr := config.Infra.GetStackState()
		if stateErr != nil {
			config.Log.Error(stateErr, "failed to get Pulumi stack state after failed creation")
		} else if stateJSON != nil {
			if saveErr := config.Callbacks.SaveState(stateJSON); saveErr != nil {
				config.Log.Error(saveErr, "failed to save partial Pulumi state after failed creation")
			}
		}

		persistFailure(config.Callbacks.PersistFailure, config.Log)
		return
	}

	// capture final Pulumi state
	stateJSON, err := config.Infra.GetStackState()
	if err != nil {
		config.Log.Error(err, "failed to get Pulumi stack state after creation")
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

// executePulumiDelete runs the full Pulumi delete lifecycle in a goroutine.
func executePulumiDelete(config PulumiDeleteConfig) {
	// refresh the deletion acknowledgement until this function returns
	quitAck := make(chan bool, 1)
	go refreshAck(config.Callbacks.RefreshAck, quitAck, config.Log)
	defer func() { quitAck <- true }()

	// restore Pulumi state from ResourceInventory if available so Pulumi
	// knows which cloud resources to destroy
	if config.ExistingState != nil &&
		len(*config.ExistingState) > 0 &&
		string(*config.ExistingState) != "{}" &&
		string(*config.ExistingState) != "null" {
		// validate state JSON before restoring — corrupt/truncated state from
		// a partial fsnotify write would cause SetStackState to fail
		if !json.Valid(*config.ExistingState) {
			config.Log.Error(
				fmt.Errorf("existing state is not valid JSON (%d bytes)", len(*config.ExistingState)),
				"skipping state restoration for delete, will attempt destroy without state",
			)
		} else {
			if err := config.Infra.SetStackState(config.ExistingState); err != nil {
				config.Log.Error(err, "failed to restore Pulumi stack state for delete, proceeding without state")
			} else {
				config.Log.Info("restored Pulumi state from database for deletion")

				// refresh stack to sync state with cloud reality
				if err := config.Infra.RefreshStack(); err != nil {
					config.Log.Error(err, "failed to refresh Pulumi stack state before delete, proceeding with destroy")
				} else {
					config.Log.Info("refreshed Pulumi stack state against cloud reality")
				}
			}
		}
	}

	// destroy Pulumi stack
	if err := config.Infra.DestroyInfra(); err != nil {
		config.Log.Error(err, "failed to delete infrastructure, will retry on next reconciliation")

		// capture updated Pulumi state so retries know which resources remain
		stateJSON, stateErr := config.Infra.GetStackState()
		if stateErr != nil {
			config.Log.Error(stateErr, "failed to get Pulumi stack state after failed deletion")
		} else if stateJSON != nil {
			if saveErr := config.Callbacks.SaveState(stateJSON); saveErr != nil {
				config.Log.Error(saveErr, "failed to save Pulumi state after failed deletion")
			}
		}
		return
	}

	// call provider-specific success handler
	if err := config.Callbacks.OnSuccess(); err != nil {
		config.Log.Error(err, "failed to execute delete success callback")
	}
}

// streamState watches the Pulumi state file via fsnotify and pushes changes
// to the API using the saveState callback on every Write/Create event.
func streamState(
	infra PulumiInfra,
	saveState func(state *datatypes.JSON) error,
	quit chan bool,
	log *logr.Logger,
) {
	// get state file path and pre-create directory
	stateFilePath, err := infra.GetStateFilePath()
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
			state, err := infra.ReadStateFile()
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

// verifyState checks the integrity of a Pulumi state JSON object to ensure it
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
