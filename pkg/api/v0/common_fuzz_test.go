package v0

import (
	"reflect"
	"testing"
	"time"
)

// TestChangeDetectionFuzz feeds pairs of logically-equal values through
// the change-detection helpers and ReconciliationStateChanged() and
// records where a naive comparator (reflect.DeepEqual on *time.Time,
// pointer identity on *bool) would report a false positive but the
// helper correctly reports equal. Each pair is a shape that comes up
// on a DB round-trip and previously caused a publish-loop bug.
func TestChangeDetectionFuzz(t *testing.T) {
	// build a boolean pair: two nil pointers.
	// change-detection expects nil == nil.
	var nilBoolA, nilBoolB *bool
	if got := boolPtrEqual(nilBoolA, nilBoolB); !got {
		t.Errorf("boolPtrEqual(nil, nil) = false, want true")
	}

	// build a boolean pair: same value, distinct pointer identity.
	// change-detection expects value equality, not pointer equality.
	trueA := true
	trueB := true
	if got := boolPtrEqual(&trueA, &trueB); !got {
		t.Errorf("boolPtrEqual(&true, &true) with distinct backing = false, want true")
	}
	if &trueA == &trueB {
		t.Fatalf("test setup: expected distinct pointer identity")
	}

	// build a *time.Time pair: same UTC instant, one carries a monotonic
	// clock reading (fresh time.Now), the other has been stripped of it
	// (mirrors a value loaded from the DB via gorm).
	withMono := time.Now().UTC()
	stripped := withMono.Round(0)
	if got := timePtrEqual(&withMono, &stripped); !got {
		t.Errorf("timePtrEqual(withMono, stripped) = false, want true (same instant)")
	}
	// confirm reflect.DeepEqual is the naive comparator this helper defends against.
	if reflect.DeepEqual(withMono, stripped) {
		t.Logf("note: reflect.DeepEqual returned true here; monotonic reading may already be absent")
	}

	// build a *time.Time pair: same wall instant expressed in UTC vs a
	// Local location that happens to share the same offset. Naive
	// reflect.DeepEqual would compare the loc pointer and report changed.
	instant := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	sameInstantLocal := instant.In(time.Local)
	if got := timePtrEqual(&instant, &sameInstantLocal); !got {
		t.Errorf("timePtrEqual across loc = false, want true")
	}

	// exercise timePtrSet on the same time pairs: it only distinguishes
	// nil vs set, so both members being set must compare equal.
	if got := timePtrSet(&withMono, &stripped); !got {
		t.Errorf("timePtrSet(both set) = false, want true")
	}
	var nilTime *time.Time
	if got := timePtrSet(nilTime, nilTime); !got {
		t.Errorf("timePtrSet(nil, nil) = false, want true")
	}
	if got := timePtrSet(&withMono, nilTime); got {
		t.Errorf("timePtrSet(set, nil) = true, want false")
	}

	// build a []byte pair: two encrypted blobs whose plaintexts are equal
	// but whose ciphertexts differ (random nonce). There is no dedicated
	// helper for this shape on the Reconciliation type, so record the
	// naive comparator's answer for the record. The change-detection path
	// does not compare encrypted fields today; a helper would be needed
	// if a Reconciliation field were ever encrypted.
	cipherA := []byte{0x01, 0x02, 0x03, 0x04}
	cipherB := []byte{0x0a, 0x0b, 0x0c, 0x0d}
	if reflect.DeepEqual(cipherA, cipherB) {
		t.Fatalf("test setup: expected differing ciphertexts")
	}
	// no helper exists; this is a documentation-only observation.
	t.Logf("byte-slice pair with differing ciphertexts: no helper on Reconciliation; naive DeepEqual = false")

	// build a struct-copy pair: two independent copies of the same
	// Reconciliation value. Different memory, identical fields.
	// ReconciliationStateChanged must report false.
	base := makeReconciliation(true, false, instant, instant, instant, instant, instant)
	copyOf := makeReconciliation(true, false, instant, instant, instant, instant, instant)
	if got := ReconciliationStateChanged(base, copyOf); got {
		t.Errorf("ReconciliationStateChanged(identical copies) = true, want false")
	}

	// feed the monotonic-vs-stripped pair through ReconciliationStateChanged
	// on a one-shot field (CreationConfirmed): the round-trip strips
	// monotonic reading, and a naive DeepEqual would flag this as a change.
	fresh := makeReconciliation(true, false, withMono, withMono, withMono, withMono, withMono)
	fromDB := makeReconciliation(true, false, stripped, stripped, stripped, stripped, stripped)
	if got := ReconciliationStateChanged(fresh, fromDB); got {
		t.Errorf("ReconciliationStateChanged(fresh vs DB-round-trip) = true, want false")
	}

	// feed the UTC-vs-Local same-instant pair through ReconciliationStateChanged.
	utc := makeReconciliation(true, false, instant, instant, instant, instant, instant)
	local := makeReconciliation(true, false, sameInstantLocal, sameInstantLocal, sameInstantLocal, sameInstantLocal, sameInstantLocal)
	if got := ReconciliationStateChanged(utc, local); got {
		t.Errorf("ReconciliationStateChanged(UTC vs Local same instant) = true, want false")
	}

	// feed an ack re-stamp through ReconciliationStateChanged: only the
	// CreationAcknowledged / DeletionAcknowledged instant advances by a
	// full second (so it survives DB precision), all other markers hold.
	// timePtrSet must treat both as set and report no change.
	later := instant.Add(1 * time.Second)
	prev := makeReconciliation(true, false, instant, instant, instant, instant, instant)
	restamped := makeReconciliation(true, false, later, instant, instant, later, instant)
	if got := ReconciliationStateChanged(prev, restamped); got {
		t.Errorf("ReconciliationStateChanged(ack re-stamp) = true, want false")
	}

	// negative control: an unset-to-set transition on CreationConfirmed is
	// a genuine state change that must publish.
	noConfirm := Reconciliation{
		Reconciled:           ptrBool(true),
		CreationAcknowledged: ptrTime(instant),
	}
	withConfirm := Reconciliation{
		Reconciled:           ptrBool(true),
		CreationAcknowledged: ptrTime(instant),
		CreationConfirmed:    ptrTime(instant),
	}
	if got := ReconciliationStateChanged(noConfirm, withConfirm); !got {
		t.Errorf("ReconciliationStateChanged(unset -> set CreationConfirmed) = false, want true")
	}

	// negative control: Reconciled flipping from false to true must publish.
	unreconciled := Reconciliation{Reconciled: ptrBool(false)}
	reconciled := Reconciliation{Reconciled: ptrBool(true)}
	if got := ReconciliationStateChanged(unreconciled, reconciled); !got {
		t.Errorf("ReconciliationStateChanged(Reconciled flip) = false, want true")
	}
}

// makeReconciliation builds a Reconciliation whose every marker is set
// so a single field flip in a caller's copy shows up as the only diff.
func makeReconciliation(
	reconciled, creationFailed bool,
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
	}
}

// ptrBool returns a pointer to a distinct copy of b.
func ptrBool(b bool) *bool { return &b }

// ptrTime returns a pointer to a distinct copy of t.
func ptrTime(t time.Time) *time.Time { return &t }
