package v0

import (
	"time"

	"gorm.io/gorm"
)

// Common includes standard fields included in most objects.
type Common struct {
	ID        *uint           `json:",omitempty" gorm:"primarykey"`
	CreatedAt *time.Time      `json:",omitempty"`
	UpdatedAt *time.Time      `json:",omitempty"`
	DeletedAt *gorm.DeletedAt `json:",omitempty" gorm:"index"`
}

// Reconciliation records how far a controller has gotten with an object. Every
// field is a one-shot marker or a flag except the two acknowledgement
// timestamps, which a reconciler re-stamps on every pass to prove it is alive.
type Reconciliation struct {
	// Reconciled indicates that the controller has brought the system
	// into the state this object describes.
	Reconciled *bool `json:",omitempty" validate:"optional" gorm:"default:false"`

	// CreationAcknowledged is the time the controller last picked up creation of
	// this object, and it is re-stamped on every creation pass.
	CreationAcknowledged *time.Time `json:",omitempty" validate:"optional"`

	// CreationConfirmed is the time the controller finished creating
	// the resources this object describes.
	CreationConfirmed *time.Time `json:",omitempty" validate:"optional"`

	// CreationFailed indicates that creation did not succeed.
	CreationFailed *bool `json:",omitempty" validate:"optional" gorm:"default:false"`

	// DeletionScheduled is the time a delete was requested, which lets reconcilers
	// clean up before the object leaves the database.
	DeletionScheduled *time.Time `json:",omitempty" validate:"optional"`

	// DeletionAcknowledged is the time the controller last picked up deletion of
	// this object, and it is re-stamped on every deletion pass.
	DeletionAcknowledged *time.Time `json:",omitempty" validate:"optional"`

	// DeletionConfirmed is the time the controller finished cleaning up,
	// which clears the object for removal from the database.
	DeletionConfirmed *time.Time `json:",omitempty" validate:"optional"`

	// InterruptReconciliation halts further reconciliation, for cases where
	// continuing would be destructive, such as provisioning more
	// infrastructure over an unresolved failure.
	InterruptReconciliation *bool `json:",omitempty" validate:"optional" gorm:"default:false"`
}

// ReconciliationStateChanged reports whether two snapshots of an object's
// reconciliation state differ. InterruptReconciliation is not compared; it
// gates whether a controller acts, not how far it has gotten.
//
// Timestamps compare by instant rather than by struct equality, which reads one
// moment as two once a database round trip drops the monotonic clock reading
// or changes the location pointer. That difference publishes an update, wakes
// the reconciler, which re-stamps an acknowledgement and publishes again.
func ReconciliationStateChanged(a, b Reconciliation) bool {
	return !boolPtrEqual(a.Reconciled, b.Reconciled) ||
		!timePtrSet(a.CreationAcknowledged, b.CreationAcknowledged) ||
		!timePtrEqual(a.CreationConfirmed, b.CreationConfirmed) ||
		!boolPtrEqual(a.CreationFailed, b.CreationFailed) ||
		!timePtrEqual(a.DeletionScheduled, b.DeletionScheduled) ||
		!timePtrSet(a.DeletionAcknowledged, b.DeletionAcknowledged) ||
		!timePtrEqual(a.DeletionConfirmed, b.DeletionConfirmed)
}

// ReconciliationUpdateNotifiable reports whether an update to an unreconciled
// object should wake its controller. The gate is asymmetric on purpose.
//
// A write that changed no reconciliation state still notifies. That is what a
// spec edit looks like, including an operator's retry after a failed reconcile,
// and staying quiet strands the object, since nothing else flips a marker for
// it. The one quiet case is a reconciler pass that moved an acknowledgement
// timestamp and nothing else, where publishing wakes the reconciler, which
// stamps again, which publishes again.
func ReconciliationUpdateNotifiable(a, b Reconciliation) bool {
	if ReconciliationStateChanged(a, b) {
		return true
	}
	return !acknowledgementRefreshed(a, b)
}

// acknowledgementRefreshed reports whether either acknowledgement timestamp
// moved while both snapshots keep it set, which is the signature of a
// reconciler pass rather than an edit from a caller.
func acknowledgementRefreshed(a, b Reconciliation) bool {
	return timePtrRefreshed(a.CreationAcknowledged, b.CreationAcknowledged) ||
		timePtrRefreshed(a.DeletionAcknowledged, b.DeletionAcknowledged)
}

// timePtrRefreshed reports whether both pointers are set and name different
// instants. Crossing between nil and set is a transition rather than a
// refresh, and the broader state comparison already reports that.
func timePtrRefreshed(a, b *time.Time) bool {
	return a != nil && b != nil && !a.Equal(*b)
}

// boolPtrEqual compares two bool pointers by value. Two nil pointers are
// equal, and a nil paired with a non-nil pointer is not.
func boolPtrEqual(a, b *bool) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// timePtrEqual compares two time pointers by instant, so a moment survives a
// database round trip. Two nil pointers are equal; nil and non-nil are not.
func timePtrEqual(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

// timePtrSet compares two time pointers on set versus unset, which fits a
// timestamp where only the first stamp means anything.
func timePtrSet(a, b *time.Time) bool {
	return (a == nil) == (b == nil)
}
