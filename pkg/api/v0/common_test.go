package v0

import "time"

// makeReconciliation returns a Reconciliation with every pointer field
// set except InterruptReconciliation.
func makeReconciliation(
	reconciled, creationFailed, deletionFailed bool,
	creationAck, creationConfirmed, deletionScheduled, deletionAck, deletionConfirmed time.Time,
) Reconciliation {
	return Reconciliation{
		Reconciled:           ptrBool(reconciled),
		CreationAcknowledged: ptrTime(creationAck),
		CreationConfirmed:    ptrTime(creationConfirmed),
		CreationFailed:       ptrBool(creationFailed),
		DeletionScheduled:    ptrTime(deletionScheduled),
		DeletionAcknowledged: ptrTime(deletionAck),
		DeletionConfirmed:    ptrTime(deletionConfirmed),
		DeletionFailed:       ptrBool(deletionFailed),
	}
}

// ptrBool returns a pointer to a distinct copy of b.
func ptrBool(b bool) *bool { return &b }

// ptrTime returns a pointer to a distinct copy of t.
func ptrTime(t time.Time) *time.Time { return &t }
