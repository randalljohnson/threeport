package v0

import (
	"time"

	"gorm.io/gorm"
)

// Common includes standard fields included in most objects.
type Common struct {
	ID        *uint `gorm:"primarykey"`
	CreatedAt *time.Time
	UpdatedAt *time.Time
	DeletedAt *gorm.DeletedAt `gorm:"index"`
}

// Reconciliation includes the fields for reconciled objects.  These are
// leveraged by controllers to persist information related to the reconciliation
// of system state for objects.
type Reconciliation struct {
	// Indicates if object is considered to be reconciled by the object's controller.
	Reconciled *bool `validate:"optional" gorm:"default:false"`

	// The timestamp of the latest creation-reconciliation acknowledgement,
	// refreshed while creation is still in progress
	CreationAcknowledged *time.Time `validate:"optional"`

	// Used by controllers to confirm creation of an object.
	CreationConfirmed *time.Time `validate:"optional"`

	// Gets set to true if creation process fails.
	CreationFailed *bool `validate:"optional" gorm:"default:false"`

	// Used to inform reconcilers that an object is being deleted so they may
	// complete delete reconciliation before actually deleting the object from the database.
	DeletionScheduled *time.Time `validate:"optional"`

	// The timestamp of the latest deletion-reconciliation acknowledgement,
	// refreshed while deletion is still in progress
	DeletionAcknowledged *time.Time `validate:"optional"`

	// Used by controllers to confirm deletion of an object.
	DeletionConfirmed *time.Time `validate:"optional"`

	// The flag that records a failed deletion reconciliation
	DeletionFailed *bool `validate:"optional" gorm:"default:false"`

	// InterruptReconciliation is used by the controller to indicate that future
	// reconciliation should be interrupted. Useful in cases where there is a
	// situation where future reconciliation could be destructive such as
	// spinning up more infrastructure when there is a unresolved problem.
	InterruptReconciliation *bool `validate:"optional" gorm:"default:false"`
}

// ReconciliationStateChanged reports whether reconciliation state differs
// between two values in a way that should publish an update notification.
// Reconciled and CreationFailed compare by value. CreationConfirmed,
// DeletionScheduled, and DeletionConfirmed compare by instant, so the same
// moment with a different monotonic reading or location counts as equal.
// CreationAcknowledged and DeletionAcknowledged compare only nil versus set,
// so a liveness re-stamp while work is in progress is not a change.
// DeletionFailed, InterruptReconciliation, and a sibling resource-inventory
// field are omitted; a caller that must notify on an inventory change also
// flips a state marker.
func ReconciliationStateChanged(a, b Reconciliation) bool {
	return !boolPtrEqual(a.Reconciled, b.Reconciled) ||
		!timePtrSet(a.CreationAcknowledged, b.CreationAcknowledged) ||
		!timePtrEqual(a.CreationConfirmed, b.CreationConfirmed) ||
		!boolPtrEqual(a.CreationFailed, b.CreationFailed) ||
		!timePtrEqual(a.DeletionScheduled, b.DeletionScheduled) ||
		!timePtrSet(a.DeletionAcknowledged, b.DeletionAcknowledged) ||
		!timePtrEqual(a.DeletionConfirmed, b.DeletionConfirmed)
}

// boolPtrEqual reports whether two *bool values are equal, treating two nils as equal.
func boolPtrEqual(a, b *bool) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// timePtrEqual reports whether two *time.Time values refer to the same instant.
// Uses Equal so monotonic reading and location differences on that instant do not count.
func timePtrEqual(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

// timePtrSet reports whether two *time.Time values agree on nil versus set.
// Ack timestamps re-stamp every reconcile pass; comparing by instant would treat each re-stamp as a change.
func timePtrSet(a, b *time.Time) bool {
	return (a == nil) == (b == nil)
}
