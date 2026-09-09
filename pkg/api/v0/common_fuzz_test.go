package v0

import (
	"reflect"
	"testing"
	"time"
)

// TestChangeDetectionFuzz covers ReconciliationStateChanged and its
// pointer helpers on times, copies, and ack re-stamps.
func TestChangeDetectionFuzz(t *testing.T) {
	// setup two nil bool pointers
	var nilBoolA, nilBoolB *bool
	// assert boolPtrEqual on two nils
	if got := boolPtrEqual(nilBoolA, nilBoolB); !got {
		t.Errorf("boolPtrEqual(nil, nil) = false, want true")
	}

	// setup distinct pointers to true
	trueA := true
	trueB := true
	// assert boolPtrEqual ignores pointer identity
	if got := boolPtrEqual(&trueA, &trueB); !got {
		t.Errorf("boolPtrEqual(&true, &true) with distinct backing = false, want true")
	}
	// check the pointers are distinct
	if &trueA == &trueB {
		t.Fatalf("test setup: expected distinct pointer identity")
	}

	// setup a UTC Now reading and a Round(0) copy
	withMono := time.Now().UTC()
	stripped := withMono.Round(0)
	// assert timePtrEqual on the same instant
	if got := timePtrEqual(&withMono, &stripped); !got {
		t.Errorf("timePtrEqual(withMono, stripped) = false, want true (same instant)")
	}
	// log if DeepEqual already agrees, since UTC strips monotonic
	if reflect.DeepEqual(withMono, stripped) {
		t.Logf("note: reflect.DeepEqual returned true here; monotonic reading may already be absent")
	}

	// setup the same instant in UTC and Local
	instant := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	sameInstantLocal := instant.In(time.Local)
	// assert timePtrEqual across locations; DeepEqual compares Location
	if got := timePtrEqual(&instant, &sameInstantLocal); !got {
		t.Errorf("timePtrEqual across loc = false, want true")
	}

	// assert timePtrSet when both times are set
	if got := timePtrSet(&withMono, &stripped); !got {
		t.Errorf("timePtrSet(both set) = false, want true")
	}
	// setup a nil time pointer
	var nilTime *time.Time
	// assert timePtrSet on two nils
	if got := timePtrSet(nilTime, nilTime); !got {
		t.Errorf("timePtrSet(nil, nil) = false, want true")
	}
	// assert timePtrSet rejects set vs nil
	if got := timePtrSet(&withMono, nilTime); got {
		t.Errorf("timePtrSet(set, nil) = true, want false")
	}

	// setup differing byte slices
	cipherA := []byte{0x01, 0x02, 0x03, 0x04}
	cipherB := []byte{0x0a, 0x0b, 0x0c, 0x0d}
	// check DeepEqual reports them unequal
	if reflect.DeepEqual(cipherA, cipherB) {
		t.Fatalf("test setup: expected differing ciphertexts")
	}
	// log that Reconciliation has no byte-slice helper
	t.Logf("byte-slice pair with differing ciphertexts: no helper on Reconciliation; naive DeepEqual = false")

	// setup identical Reconciliation copies
	base := makeReconciliation(true, false, false, instant, instant, instant, instant, instant)
	copyOf := makeReconciliation(true, false, false, instant, instant, instant, instant, instant)
	// assert ReconciliationStateChanged rejects the copies
	if got := ReconciliationStateChanged(base, copyOf); got {
		t.Errorf("ReconciliationStateChanged(identical copies) = true, want false")
	}

	// setup a UTC Now reading vs a Round(0) copy
	fresh := makeReconciliation(true, false, false, withMono, withMono, withMono, withMono, withMono)
	fromDB := makeReconciliation(true, false, false, stripped, stripped, stripped, stripped, stripped)
	// assert ReconciliationStateChanged rejects the pair
	if got := ReconciliationStateChanged(fresh, fromDB); got {
		t.Errorf("ReconciliationStateChanged(fresh vs DB-round-trip) = true, want false")
	}

	// setup UTC vs Local copies of the same instant
	utc := makeReconciliation(true, false, false, instant, instant, instant, instant, instant)
	local := makeReconciliation(true, false, false, sameInstantLocal, sameInstantLocal, sameInstantLocal, sameInstantLocal, sameInstantLocal)
	// assert ReconciliationStateChanged rejects the location pair
	if got := ReconciliationStateChanged(utc, local); got {
		t.Errorf("ReconciliationStateChanged(UTC vs Local same instant) = true, want false")
	}

	// setup CreationAcknowledged and DeletionAcknowledged one second later
	later := instant.Add(1 * time.Second)
	prev := makeReconciliation(true, false, false, instant, instant, instant, instant, instant)
	restamped := makeReconciliation(true, false, false, later, instant, instant, later, instant)
	// assert ReconciliationStateChanged rejects the re-stamp
	if got := ReconciliationStateChanged(prev, restamped); got {
		t.Errorf("ReconciliationStateChanged(ack re-stamp) = true, want false")
	}

	// setup CreationConfirmed unset vs set
	noConfirm := Reconciliation{
		Reconciled:           ptrBool(true),
		CreationAcknowledged: ptrTime(instant),
	}
	withConfirm := Reconciliation{
		Reconciled:           ptrBool(true),
		CreationAcknowledged: ptrTime(instant),
		CreationConfirmed:    ptrTime(instant),
	}
	// assert ReconciliationStateChanged accepts the unset-to-set
	if got := ReconciliationStateChanged(noConfirm, withConfirm); !got {
		t.Errorf("ReconciliationStateChanged(unset -> set CreationConfirmed) = false, want true")
	}

	// setup a Reconciled false-to-true flip
	unreconciled := Reconciliation{Reconciled: ptrBool(false)}
	reconciled := Reconciliation{Reconciled: ptrBool(true)}
	// assert ReconciliationStateChanged accepts the flip
	if got := ReconciliationStateChanged(unreconciled, reconciled); !got {
		t.Errorf("ReconciliationStateChanged(Reconciled flip) = false, want true")
	}

	// setup a DeletionFailed false-to-true flip
	deleteOk := Reconciliation{DeletionFailed: ptrBool(false)}
	deleteFailed := Reconciliation{DeletionFailed: ptrBool(true)}
	// assert ReconciliationStateChanged accepts the flip
	if got := ReconciliationStateChanged(deleteOk, deleteFailed); !got {
		t.Errorf("ReconciliationStateChanged(DeletionFailed flip) = false, want true")
	}
}
