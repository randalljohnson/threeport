package v0

import (
	"reflect"
	"testing"
	"time"
)

// TestChangeDetectionFuzz feeds logically-equal value pairs through the
// change-detection helpers and ReconciliationStateChanged. Each pair is a
// DB round-trip shape where reflect.DeepEqual can false-positive.
func TestChangeDetectionFuzz(t *testing.T) {
	// build a boolean pair: two nil pointers; change-detection expects nil == nil
	var nilBoolA, nilBoolB *bool
	if got := boolPtrEqual(nilBoolA, nilBoolB); !got {
		t.Errorf("boolPtrEqual(nil, nil) = false, want true")
	}

	// build a boolean pair: same value, distinct pointer identity
	trueA := true
	trueB := true
	if got := boolPtrEqual(&trueA, &trueB); !got {
		t.Errorf("boolPtrEqual(&true, &true) with distinct backing = false, want true")
	}
	if &trueA == &trueB {
		t.Fatalf("test setup: expected distinct pointer identity")
	}

	// build a *time.Time pair: same UTC instant, one with monotonic reading
	// (time.Now) and one stripped (mirrors a value loaded from the DB via gorm)
	withMono := time.Now().UTC()
	stripped := withMono.Round(0)
	if got := timePtrEqual(&withMono, &stripped); !got {
		t.Errorf("timePtrEqual(withMono, stripped) = false, want true (same instant)")
	}
	// confirm reflect.DeepEqual is the naive comparator this helper defends against
	if reflect.DeepEqual(withMono, stripped) {
		t.Logf("note: reflect.DeepEqual returned true here; monotonic reading may already be absent")
	}

	// build a *time.Time pair: same wall instant in UTC vs Local
	instant := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	sameInstantLocal := instant.In(time.Local)
	if got := timePtrEqual(&instant, &sameInstantLocal); !got {
		t.Errorf("timePtrEqual across loc = false, want true")
	}

	// exercise timePtrSet: it only distinguishes nil vs set
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

	// build a []byte pair with differing ciphertexts; no Reconciliation helper
	// compares encrypted fields today, so this is documentation-only
	cipherA := []byte{0x01, 0x02, 0x03, 0x04}
	cipherB := []byte{0x0a, 0x0b, 0x0c, 0x0d}
	if reflect.DeepEqual(cipherA, cipherB) {
		t.Fatalf("test setup: expected differing ciphertexts")
	}
	t.Logf("byte-slice pair with differing ciphertexts: no helper on Reconciliation; naive DeepEqual = false")

	// build a struct-copy pair: independent copies, identical fields
	base := makeReconciliation(true, false, false, instant, instant, instant, instant, instant)
	copyOf := makeReconciliation(true, false, false, instant, instant, instant, instant, instant)
	if got := ReconciliationStateChanged(base, copyOf); got {
		t.Errorf("ReconciliationStateChanged(identical copies) = true, want false")
	}

	// feed monotonic-vs-stripped through ReconciliationStateChanged
	fresh := makeReconciliation(true, false, false, withMono, withMono, withMono, withMono, withMono)
	fromDB := makeReconciliation(true, false, false, stripped, stripped, stripped, stripped, stripped)
	if got := ReconciliationStateChanged(fresh, fromDB); got {
		t.Errorf("ReconciliationStateChanged(fresh vs DB-round-trip) = true, want false")
	}

	// feed UTC-vs-Local same-instant through ReconciliationStateChanged
	utc := makeReconciliation(true, false, false, instant, instant, instant, instant, instant)
	local := makeReconciliation(true, false, false, sameInstantLocal, sameInstantLocal, sameInstantLocal, sameInstantLocal, sameInstantLocal)
	if got := ReconciliationStateChanged(utc, local); got {
		t.Errorf("ReconciliationStateChanged(UTC vs Local same instant) = true, want false")
	}

	// feed an ack re-stamp: only CreationAcknowledged / DeletionAcknowledged advance
	later := instant.Add(1 * time.Second)
	prev := makeReconciliation(true, false, false, instant, instant, instant, instant, instant)
	restamped := makeReconciliation(true, false, false, later, instant, instant, later, instant)
	if got := ReconciliationStateChanged(prev, restamped); got {
		t.Errorf("ReconciliationStateChanged(ack re-stamp) = true, want false")
	}

	// negative control: unset-to-set CreationConfirmed must publish
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

	// negative control: Reconciled flipping false to true must publish
	unreconciled := Reconciliation{Reconciled: ptrBool(false)}
	reconciled := Reconciliation{Reconciled: ptrBool(true)}
	if got := ReconciliationStateChanged(unreconciled, reconciled); !got {
		t.Errorf("ReconciliationStateChanged(Reconciled flip) = false, want true")
	}
}

// makeReconciliation builds a Reconciliation with every marker set so a
// single field flip in a caller's copy shows up as the only diff.
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
