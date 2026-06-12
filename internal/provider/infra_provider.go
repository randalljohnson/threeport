package provider

import (
	"context"

	"gorm.io/datatypes"
)

// Phase classifies where an infrastructure operation stands, as seen by a
// single Observe call. It is derived from cloud reality plus persisted state,
// never from an in-process goroutine, so any replica computes the same phase.
type Phase int

const (
	// PhaseAbsent means no cloud resources for this stack exist yet.
	PhaseAbsent Phase = iota

	// PhaseProvisioning means resources exist but are not all ready; a create
	// is in flight or was interrupted and must be resumed.
	PhaseProvisioning

	// PhaseReady means all resources exist and are healthy; create is complete.
	PhaseReady

	// PhaseDeleting means a destroy is in flight; some resources remain.
	PhaseDeleting

	// PhaseFailed means the last apply or destroy step ended in a terminal
	// error that retry will not clear without intervention.
	PhaseFailed
)

// Observation is the result of a single non-blocking Observe call. It is the
// provider's complete answer to "what is true in the cloud right now", from
// which the lifecycle handler decides whether to kick, requeue, or confirm.
type Observation struct {
	// Phase is the current lifecycle phase derived from cloud reality.
	Phase Phase

	// State is the authoritative stack state after refreshing against the
	// cloud, suitable for persisting to the resource inventory. Nil when absent.
	State *datatypes.JSON

	// Message carries provider detail for logging (e.g. the failing resource).
	Message string
}

// InfraProvider is the minimal interface an infrastructure backend implements
// to participate in observe-and-requeue lifecycle management. Pulumi,
// Terraform, and direct-SDK backends all satisfy it. No method blocks for the
// duration of a cloud operation: Observe reads, Apply and Destroy kick.
type InfraProvider interface {
	// Observe refreshes persisted state against cloud reality and reports the
	// current phase. It is the single read path: it subsumes refresh against
	// the cloud and the readiness check that the lifecycle handler depends on.
	// It must return promptly and must not start or wait on a create or
	// destroy. A failed refresh must surface as an error so the handler
	// requeues without kicking; it must never be reported as PhaseAbsent,
	// which would let Apply run against infrastructure that already exists.
	Observe(ctx context.Context) (Observation, error)

	// Apply advances the create toward PhaseReady and returns without waiting
	// for completion. Implementations run a single bounded reconcile step under
	// the context deadline and persist incremental state as they go; the next
	// Observe reports progress. Apply must be idempotent: calling it against
	// partially-created infra resumes rather than duplicates, because it
	// operates on the refreshed persisted state.
	Apply(ctx context.Context) error

	// Destroy advances the teardown toward PhaseAbsent and returns without
	// waiting for completion, with the same single-bounded-step, idempotent
	// contract as Apply.
	Destroy(ctx context.Context) error

	// SetStackState restores previously persisted state before observing, so a
	// fresh process knows about resources an earlier process created.
	SetStackState(state *datatypes.JSON) error

	// GetStackState returns the current persisted state for the API to store in
	// the resource inventory.
	GetStackState() (*datatypes.JSON, error)
}
