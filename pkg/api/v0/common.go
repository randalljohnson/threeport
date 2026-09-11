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

	// The last time creation was acknowledged as begun
	CreationAcknowledged *time.Time `validate:"optional"`

	// The time creation of the object was confirmed
	CreationConfirmed *time.Time `validate:"optional"`

	// Gets set to true if creation process fails.
	CreationFailed *bool `validate:"optional" gorm:"default:false"`

	// Used to inform reconcilers that an object is being deleted so they may
	// complete delete reconciliation before actually deleting the object from the database.
	DeletionScheduled *time.Time `validate:"optional"`

	// Used by controllers to acknowledge deletion and indicate that deletion
	// reconciliation has begun.
	DeletionAcknowledged *time.Time `validate:"optional"`

	// Used by controllers to confirm deletion of an object.
	DeletionConfirmed *time.Time `validate:"optional"`

	// A flag set to true if deletion of the object fails
	DeletionFailed *bool `validate:"optional" gorm:"default:false"`

	// InterruptReconciliation is used by the controller to indicated that future
	// reconcilation should be interrupted.  Useful in cases where there is a
	// situation where future reconciliation could be descructive such as
	// spinning up more infrastructure when there is a unresolved problem.
	InterruptReconciliation *bool `validate:"optional" gorm:"default:false"`
}

// ReconciliationStateChanged reports whether a and b differ on any
// reconciliation field except InterruptReconciliation.
func ReconciliationStateChanged(a, b Reconciliation) bool {
	return !boolPtrEqual(a.Reconciled, b.Reconciled) ||
		!timePtrSet(a.CreationAcknowledged, b.CreationAcknowledged) ||
		!timePtrEqual(a.CreationConfirmed, b.CreationConfirmed) ||
		!boolPtrEqual(a.CreationFailed, b.CreationFailed) ||
		!timePtrEqual(a.DeletionScheduled, b.DeletionScheduled) ||
		!timePtrSet(a.DeletionAcknowledged, b.DeletionAcknowledged) ||
		!timePtrEqual(a.DeletionConfirmed, b.DeletionConfirmed) ||
		!boolPtrEqual(a.DeletionFailed, b.DeletionFailed)
}

// boolPtrEqual returns whether two bool pointers are both nil or hold the same value.
func boolPtrEqual(a, b *bool) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// timePtrEqual returns whether two time pointers are both nil or name the
// same instant. Location and monotonic readings are ignored.
func timePtrEqual(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

// timePtrSet returns whether two time pointers are both nil or both non-nil.
func timePtrSet(a, b *time.Time) bool {
	return (a == nil) == (b == nil)
}
