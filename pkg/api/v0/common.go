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

// Reconciliation includes the fields for reconciled objects.  These are
// leveraged by controllers to persist information related to the reconciliation
// of system state for objects.
type Reconciliation struct {
	// Indicates if object is considered to be reconciled by the object's controller.
	Reconciled *bool `json:",omitempty" validate:"optional" gorm:"default:false"`

	// Used by controllers to acknowledge deletion and indicate that deletion
	// reconciliation has begun so that subsequent reconciliation attempts can
	// act accordingly. Re-stamped liveness marker; change-detection uses nil-vs-set only.
	CreationAcknowledged *time.Time `json:",omitempty" validate:"optional"`

	// Used by controllers to confirm deletion of an object.
	CreationConfirmed *time.Time `json:",omitempty" validate:"optional"`

	// Gets set to true if creation process fails.
	CreationFailed *bool `json:",omitempty" validate:"optional" gorm:"default:false"`

	// Used to inform reconcilers that an object is being deleted so they may
	// complete delete reconciliation before actually deleting the object from the database.
	DeletionScheduled *time.Time `json:",omitempty" validate:"optional"`

	// Used by controllers to acknowledge deletion and indicate that deletion
	// reconciliation has begun so that subsequent reconciliation attempts can
	// act accordingly. Re-stamped liveness marker; change-detection uses nil-vs-set only.
	DeletionAcknowledged *time.Time `json:",omitempty" validate:"optional"`

	// Used by controllers to confirm deletion of an object.
	DeletionConfirmed *time.Time `json:",omitempty" validate:"optional"`

	// InterruptReconciliation is used by the controller to indicated that future
	// reconcilation should be interrupted.  Useful in cases where there is a
	// situation where future reconciliation could be descructive such as
	// spinning up more infrastructure when there is a unresolved problem.
	InterruptReconciliation *bool `json:",omitempty" validate:"optional" gorm:"default:false"`
}

// ReconciliationStateChanged reports whether any reconciliation state
// marker on the two values differs semantically. Reconciled and
// CreationFailed compare by value. CreationConfirmed, DeletionScheduled,
// and DeletionConfirmed are one-shot markers and compare by instant so
// monotonic clock reading, location pointer, and precision drift on the
// same instant are treated as equal. CreationAcknowledged and
// DeletionAcknowledged are refreshed on every reconcile pass and compare
// on nil-vs-set: only the transition from unset to set counts as a change,
// so re-stamping the same semantic state does not publish an update
// notification. A bump to InterruptReconciliation or a sibling
// ResourceInventory field never publishes an update notification on its
// own; callers that want to notify on inventory changes must also flip
// one of the state markers.
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
// object should wake its controller. Three kinds of write reach this point and
// only one of them must stay quiet.
//
// A write that moved a reconciliation state marker is notifiable. So is a write
// that left reconciliation state untouched: that is what an edit to the object's
// spec looks like, including the edit an operator makes to retry after a failed
// reconcile and the one that clears InterruptReconciliation to resume a halted
// object. Suppressing those strands the object, because nothing else will flip a
// marker on its behalf.
//
// The write that must stay quiet is a reconciler pass that refreshed an
// acknowledgement timestamp and changed nothing else. Publishing on that wakes
// the reconciler, which refreshes again, which is the loop this gate exists to
// break.
func ReconciliationUpdateNotifiable(a, b Reconciliation) bool {
	if ReconciliationStateChanged(a, b) {
		return true
	}
	return !acknowledgementRefreshed(a, b)
}

// acknowledgementRefreshed reports whether either acknowledgement marker names a
// different instant on the two values while both remain set. Reconcilers stamp
// these on every pass, so a moved instant with no other change is the signature
// of a reconciler write rather than a caller's edit.
func acknowledgementRefreshed(a, b Reconciliation) bool {
	return timePtrRefreshed(a.CreationAcknowledged, b.CreationAcknowledged) ||
		timePtrRefreshed(a.DeletionAcknowledged, b.DeletionAcknowledged)
}

// timePtrRefreshed reports whether both pointers are set and name different
// instants. A pointer moving between nil and set is a state change rather than a
// refresh, so it reports false and ReconciliationStateChanged covers it.
func timePtrRefreshed(a, b *time.Time) bool {
	return a != nil && b != nil && !a.Equal(*b)
}

// boolPtrEqual reports whether two nil-safe bool pointers refer to the
// same value. Two nil pointers are equal; a nil and a non-nil pointer
// are not.
func boolPtrEqual(a, b *bool) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// timePtrEqual reports whether two nil-safe time pointers refer to the
// same instant. Two nil pointers are equal; a nil and a non-nil pointer
// are not. Uses time.Time.Equal so monotonic clock reading, location
// pointer, and precision differences on the same instant are treated
// as equal.
func timePtrEqual(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

// timePtrSet reports whether two nil-safe time pointers agree on being
// set vs unset. Used for liveness ack timestamps that legitimately
// re-stamp to time.Now() on every reconcile pass; treating them as
// "changed" on every re-stamp would produce a notification storm.
func timePtrSet(a, b *time.Time) bool {
	return (a == nil) == (b == nil)
}
