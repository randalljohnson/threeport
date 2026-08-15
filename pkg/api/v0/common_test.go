package v0

import (
	"reflect"
	"testing"
	"time"
)

// TestReconciliationUpdateNotifiable covers the three writes that reach the
// notify gate on an unreconciled object: a reconciler refreshing an
// acknowledgement timestamp, an edit that leaves reconciliation state alone,
// and a write that moves a state marker. Only the first stays quiet.
func TestReconciliationUpdateNotifiable(t *testing.T) {
	earlier := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	later := earlier.Add(time.Minute)
	no := false

	// a reconciler pass refreshes CreationAcknowledged and changes nothing else
	restamped := Reconciliation{Reconciled: &no, CreationAcknowledged: &earlier}
	after := Reconciliation{Reconciled: &no, CreationAcknowledged: &later}
	if ReconciliationUpdateNotifiable(restamped, after) {
		t.Errorf("a refreshed acknowledgement alone must not notify; that is the publish loop")
	}

	// an edit to the object's spec leaves every reconciliation field untouched.
	// This is the retry an operator makes after a failed reconcile, and the
	// clear of InterruptReconciliation that resumes a halted object
	unchanged := Reconciliation{Reconciled: &no, CreationAcknowledged: &earlier}
	if !ReconciliationUpdateNotifiable(unchanged, unchanged) {
		t.Errorf("a spec edit leaves reconciliation state equal and must still notify")
	}

	// a write that moves a state marker notifies through ReconciliationStateChanged
	failed := Reconciliation{Reconciled: &no, CreationAcknowledged: &earlier}
	yes := true
	nowFailed := Reconciliation{Reconciled: &no, CreationAcknowledged: &earlier, CreationFailed: &yes}
	if !ReconciliationUpdateNotifiable(failed, nowFailed) {
		t.Errorf("a moved state marker must notify")
	}
}

// TestChangeDetection feeds pairs of logically equal values through the
// comparators and through ReconciliationStateChanged. Each pair is a shape a
// database round trip produces, where a naive comparator reports a change that
// is not there and starts the publish loop. Two negative controls at the end
// confirm a real transition still reports changed.
func TestChangeDetection(t *testing.T) {
	// two nil bool pointers are equal
	var nilBoolA, nilBoolB *bool
	if got := boolPtrEqual(nilBoolA, nilBoolB); !got {
		t.Errorf("boolPtrEqual(nil, nil) = false, want true")
	}

	// equal bool values behind distinct pointers compare by value, not by
	// pointer identity
	trueA := true
	trueB := true
	if got := boolPtrEqual(&trueA, &trueB); !got {
		t.Errorf("boolPtrEqual(&true, &true) with distinct backing = false, want true")
	}
	if &trueA == &trueB {
		t.Fatalf("test setup: expected distinct pointer identity")
	}

	// one instant carrying a monotonic clock reading against the same instant
	// stripped of it, which is what gorm hands back from the database
	withMono := time.Now().UTC()
	stripped := withMono.Round(0)
	if got := timePtrEqual(&withMono, &stripped); !got {
		t.Errorf("timePtrEqual(withMono, stripped) = false, want true (same instant)")
	}
	// reflect.DeepEqual is the naive comparator timePtrEqual replaces
	if reflect.DeepEqual(withMono, stripped) {
		t.Logf("note: reflect.DeepEqual returned true here; monotonic reading may already be absent")
	}

	// the same instant in two locations. reflect.DeepEqual compares the loc
	// pointer and reports changed
	instant := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	sameInstantLocal := instant.In(time.Local)
	if got := timePtrEqual(&instant, &sameInstantLocal); !got {
		t.Errorf("timePtrEqual across loc = false, want true")
	}

	// timePtrSet reads only set versus unset, so it ignores the instant
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

	// two ciphertexts of the same plaintext differ because the nonce is random.
	// No Reconciliation field is encrypted, so no comparator handles this shape
	// and this records what a naive comparison would answer if one ever were
	cipherA := []byte{0x01, 0x02, 0x03, 0x04}
	cipherB := []byte{0x0a, 0x0b, 0x0c, 0x0d}
	if reflect.DeepEqual(cipherA, cipherB) {
		t.Fatalf("test setup: expected differing ciphertexts")
	}
	t.Logf("byte-slice pair with differing ciphertexts: no helper on Reconciliation; naive DeepEqual = false")

	// two independent copies of the same value: different memory, same fields
	base := makeReconciliation(true, false, false, instant, instant, instant, instant, instant)
	copyOf := makeReconciliation(true, false, false, instant, instant, instant, instant, instant)
	if got := ReconciliationStateChanged(base, copyOf); got {
		t.Errorf("ReconciliationStateChanged(identical copies) = true, want false")
	}

	// the monotonic-versus-stripped pair on every marker, which is the whole
	// object read back from the database
	fresh := makeReconciliation(true, false, false, withMono, withMono, withMono, withMono, withMono)
	fromDB := makeReconciliation(true, false, false, stripped, stripped, stripped, stripped, stripped)
	if got := ReconciliationStateChanged(fresh, fromDB); got {
		t.Errorf("ReconciliationStateChanged(fresh vs DB-round-trip) = true, want false")
	}

	// the same instant expressed in two locations, on every marker
	utc := makeReconciliation(true, false, false, instant, instant, instant, instant, instant)
	local := makeReconciliation(true, false, false, sameInstantLocal, sameInstantLocal, sameInstantLocal, sameInstantLocal, sameInstantLocal)
	if got := ReconciliationStateChanged(utc, local); got {
		t.Errorf("ReconciliationStateChanged(UTC vs Local same instant) = true, want false")
	}

	// a reconciler pass: both acknowledgement timestamps advance by a full
	// second so the move survives database precision, and no other marker moves
	later := instant.Add(1 * time.Second)
	prev := makeReconciliation(true, false, false, instant, instant, instant, instant, instant)
	restamped := makeReconciliation(true, false, false, later, instant, instant, later, instant)
	if got := ReconciliationStateChanged(prev, restamped); got {
		t.Errorf("ReconciliationStateChanged(ack re-stamp) = true, want false")
	}

	// negative control: CreationConfirmed going from unset to set is a real
	// transition and must report changed
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

	// negative control: Reconciled flipping from false to true
	unreconciled := Reconciliation{Reconciled: ptrBool(false)}
	reconciled := Reconciliation{Reconciled: ptrBool(true)}
	if got := ReconciliationStateChanged(unreconciled, reconciled); !got {
		t.Errorf("ReconciliationStateChanged(Reconciled flip) = false, want true")
	}
}

// makeReconciliation builds a Reconciliation with every marker set, so a single
// field flip in a caller's copy shows up as the only difference.
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
