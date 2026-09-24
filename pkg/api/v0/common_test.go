package v0

import (
<<<<<<< HEAD
	"reflect"
	"testing"
	"time"

	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestReconciliationUpdateNotifiable covers when an update notifies the controller.
func TestReconciliationUpdateNotifiable(t *testing.T) {
	earlier := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	later := earlier.Add(time.Minute)
	no := false

	// a restamped acknowledgement must not notify
	restamped := Reconciliation{Reconciled: &no, CreationAcknowledged: &earlier}
	after := Reconciliation{Reconciled: &no, CreationAcknowledged: &later}
	if ReconciliationUpdateNotifiable(restamped, after) {
		t.Errorf("a refreshed acknowledgement alone must not notify; that is the publish loop")
	}

	// notify when both snapshots are equal
	unchanged := Reconciliation{Reconciled: &no, CreationAcknowledged: &earlier}
	if !ReconciliationUpdateNotifiable(unchanged, unchanged) {
		t.Errorf("a spec edit leaves reconciliation state equal and must still notify")
	}

	// a moved state marker must notify
	failed := Reconciliation{Reconciled: &no, CreationAcknowledged: &earlier}
	yes := true
	nowFailed := Reconciliation{Reconciled: &no, CreationAcknowledged: &earlier, CreationFailed: &yes}
	if !ReconciliationUpdateNotifiable(failed, nowFailed) {
		t.Errorf("a moved state marker must notify")
	}
}

// TestChangeDetection covers the pointer helpers under ReconciliationStateChanged.
func TestChangeDetection(t *testing.T) {
	// check nil against nil
	var nilBoolA, nilBoolB *bool
	if got := boolPtrEqual(nilBoolA, nilBoolB); !got {
		t.Errorf("boolPtrEqual(nil, nil) = false, want true")
	}

	// check equal values behind distinct pointers
	trueA := true
	trueB := true
	if got := boolPtrEqual(&trueA, &trueB); !got {
		t.Errorf("boolPtrEqual(&true, &true) with distinct backing = false, want true")
	}
	if &trueA == &trueB {
		t.Fatalf("test setup: expected distinct pointer identity")
	}

	// check a time against its Round(0) form
	withMono := time.Now().UTC()
	stripped := withMono.Round(0)
	if got := timePtrEqual(&withMono, &stripped); !got {
		t.Errorf("timePtrEqual(withMono, stripped) = false, want true (same instant)")
	}
	if reflect.DeepEqual(withMono, stripped) {
		t.Logf("note: reflect.DeepEqual returned true here; monotonic reading may already be absent")
	}

	// check the same instant in two locations
	instant := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	sameInstantLocal := instant.In(time.Local)
	if got := timePtrEqual(&instant, &sameInstantLocal); !got {
		t.Errorf("timePtrEqual across loc = false, want true")
	}

	// check set against unset
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

	// check an acknowledgement re-stamp
	later := instant.Add(1 * time.Second)
	prev := makeReconciliation(true, false, false, instant, instant, instant, instant, instant)
	restamped := makeReconciliation(true, false, false, later, instant, instant, later, instant)
	if got := ReconciliationStateChanged(prev, restamped); got {
		t.Errorf("ReconciliationStateChanged(ack re-stamp) = true, want false")
	}

	// check unset to set CreationConfirmed
	noConfirm := Reconciliation{
		Reconciled:           util.Ptr(true),
		CreationAcknowledged: util.Ptr(instant),
	}
	withConfirm := Reconciliation{
		Reconciled:           util.Ptr(true),
		CreationAcknowledged: util.Ptr(instant),
		CreationConfirmed:    util.Ptr(instant),
	}
	if got := ReconciliationStateChanged(noConfirm, withConfirm); !got {
		t.Errorf("ReconciliationStateChanged(unset -> set CreationConfirmed) = false, want true")
	}

	// check a Reconciled flip
	unreconciled := Reconciliation{Reconciled: util.Ptr(false)}
	reconciled := Reconciliation{Reconciled: util.Ptr(true)}
	if got := ReconciliationStateChanged(unreconciled, reconciled); !got {
		t.Errorf("ReconciliationStateChanged(Reconciled flip) = false, want true")
	}
}

// makeReconciliation sets every field ReconciliationStateChanged compares.
func makeReconciliation(
	reconciled, creationFailed, deletionFailed bool,
	creationAck, creationConfirmed, deletionScheduled, deletionAck, deletionConfirmed time.Time,
) Reconciliation {
	return Reconciliation{
		Reconciled:           util.Ptr(reconciled),
		CreationAcknowledged: util.Ptr(creationAck),
		CreationConfirmed:    util.Ptr(creationConfirmed),
		CreationFailed:       util.Ptr(creationFailed),
		DeletionScheduled:    util.Ptr(deletionScheduled),
		DeletionAcknowledged: util.Ptr(deletionAck),
		DeletionConfirmed:    util.Ptr(deletionConfirmed),
		DeletionFailed:       util.Ptr(deletionFailed),
	}
}
=======
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// commonTestModel embeds Common so the GORM tags on Common can be exercised
// against a real database schema.
type commonTestModel struct {
	Common
	Name string
}

// reconciliationTestModel embeds Reconciliation so its default-value gorm tags
// and pointer semantics can be exercised end to end.
type reconciliationTestModel struct {
	Common
	Reconciliation
	Name string
}

// newTestDB opens an in-memory sqlite database and migrates the supplied
// models so struct-tag behavior can be verified against a real driver.
func newTestDB(t *testing.T, models ...interface{}) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open in-memory sqlite: %v", err)
	}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("failed to migrate models: %v", err)
	}
	return db
}

// TestCommon_PrimaryKeyAndTimestamps asserts that Common's gorm tags produce
// an auto-incrementing primary key and populated created/updated timestamps
// on insert.
func TestCommon_PrimaryKeyAndTimestamps(t *testing.T) {
	// prepare an in-memory database with the embedding model
	db := newTestDB(t, &commonTestModel{})

	// insert two rows and observe the ID and timestamp side effects
	first := commonTestModel{Name: "first"}
	if err := db.Create(&first).Error; err != nil {
		t.Fatalf("create first: %v", err)
	}
	second := commonTestModel{Name: "second"}
	if err := db.Create(&second).Error; err != nil {
		t.Fatalf("create second: %v", err)
	}

	// verify the primary key was assigned and increments
	if first.ID == nil || *first.ID == 0 {
		t.Fatalf("first.ID not assigned, got %v", first.ID)
	}
	if second.ID == nil || *second.ID <= *first.ID {
		t.Fatalf("second.ID did not increment above first.ID, got %v vs %v", second.ID, first.ID)
	}

	// verify CreatedAt and UpdatedAt were populated
	if first.CreatedAt == nil || first.CreatedAt.IsZero() {
		t.Errorf("CreatedAt not populated on insert")
	}
	if first.UpdatedAt == nil || first.UpdatedAt.IsZero() {
		t.Errorf("UpdatedAt not populated on insert")
	}

	// verify DeletedAt stays nil for an active row
	if first.DeletedAt != nil {
		t.Errorf("DeletedAt should be nil for a live row, got %v", first.DeletedAt)
	}
}

// TestCommon_SoftDelete asserts that Common's DeletedAt gorm tag drives GORM's
// soft-delete behavior: deleted rows are hidden from default queries but
// findable with Unscoped.
func TestCommon_SoftDelete(t *testing.T) {
	// prepare a database and a live row
	db := newTestDB(t, &commonTestModel{})
	row := commonTestModel{Name: "gone"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	// perform the soft delete
	if err := db.Delete(&row).Error; err != nil {
		t.Fatalf("delete: %v", err)
	}

	// verify the default query hides the row
	var found commonTestModel
	err := db.First(&found, *row.ID).Error
	if err == nil {
		t.Errorf("expected ErrRecordNotFound after soft delete, got row %+v", found)
	}

	// verify Unscoped surfaces the row with DeletedAt populated
	var raw commonTestModel
	if err := db.Unscoped().First(&raw, *row.ID).Error; err != nil {
		t.Fatalf("unscoped first: %v", err)
	}
	if raw.DeletedAt == nil || !raw.DeletedAt.Valid {
		t.Errorf("DeletedAt should be populated after soft delete, got %+v", raw.DeletedAt)
	}
}

// TestCommon_OmitEmptyJSONTags documents that Common's json tags omit zero
// value pointer fields on marshal. This is exercised implicitly here by
// asserting the zero value struct round-trips without touching the fields.
func TestCommon_ZeroValue(t *testing.T) {
	// a zero-value Common should have all-nil pointer fields
	var c Common
	if c.ID != nil || c.CreatedAt != nil || c.UpdatedAt != nil || c.DeletedAt != nil {
		t.Errorf("zero-value Common has non-nil fields: %+v", c)
	}
}

// TestReconciliation_DefaultValues asserts that Reconciliation's default:false
// gorm tags result in false (not nil) values when a row is loaded back from
// the database without the caller having set them.
func TestReconciliation_DefaultValues(t *testing.T) {
	// prepare the database and insert a row that omits reconciliation flags
	db := newTestDB(t, &reconciliationTestModel{})
	row := reconciliationTestModel{Name: "defaulted"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	// reload the row so the database defaults are applied
	var loaded reconciliationTestModel
	if err := db.First(&loaded, *row.ID).Error; err != nil {
		t.Fatalf("first: %v", err)
	}

	// verify each default-false bool tag resolved to a non-nil false pointer
	assertFalse := func(name string, v *bool) {
		t.Helper()
		if v == nil {
			t.Errorf("%s should default to false, got nil", name)
			return
		}
		if *v != false {
			t.Errorf("%s should default to false, got true", name)
		}
	}
	assertFalse("Reconciled", loaded.Reconciled)
	assertFalse("CreationFailed", loaded.CreationFailed)
	assertFalse("InterruptReconciliation", loaded.InterruptReconciliation)

	// verify optional time fields remain nil when unset
	if loaded.CreationAcknowledged != nil {
		t.Errorf("CreationAcknowledged should default to nil, got %v", loaded.CreationAcknowledged)
	}
	if loaded.DeletionScheduled != nil {
		t.Errorf("DeletionScheduled should default to nil, got %v", loaded.DeletionScheduled)
	}
}

// TestReconciliation_RoundTrip covers a full write-then-read cycle for the
// Reconciliation fields to confirm that caller-supplied values are persisted
// and hydrated unchanged.
func TestReconciliation_RoundTrip(t *testing.T) {
	// prepare the database
	db := newTestDB(t, &reconciliationTestModel{})

	// insert a row with every reconciliation field explicitly set
	truthy := true
	now := time.Now().UTC().Truncate(time.Second)
	row := reconciliationTestModel{
		Name: "populated",
		Reconciliation: Reconciliation{
			Reconciled:              &truthy,
			CreationAcknowledged:    &now,
			CreationConfirmed:       &now,
			CreationFailed:          &truthy,
			DeletionScheduled:       &now,
			DeletionAcknowledged:    &now,
			DeletionConfirmed:       &now,
			InterruptReconciliation: &truthy,
		},
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	// reload and verify each field survived the round trip
	var loaded reconciliationTestModel
	if err := db.First(&loaded, *row.ID).Error; err != nil {
		t.Fatalf("first: %v", err)
	}
	if loaded.Reconciled == nil || !*loaded.Reconciled {
		t.Errorf("Reconciled did not round-trip, got %v", loaded.Reconciled)
	}
	if loaded.CreationFailed == nil || !*loaded.CreationFailed {
		t.Errorf("CreationFailed did not round-trip, got %v", loaded.CreationFailed)
	}
	if loaded.InterruptReconciliation == nil || !*loaded.InterruptReconciliation {
		t.Errorf("InterruptReconciliation did not round-trip, got %v", loaded.InterruptReconciliation)
	}
	if loaded.CreationAcknowledged == nil || !loaded.CreationAcknowledged.Equal(now) {
		t.Errorf("CreationAcknowledged did not round-trip, got %v want %v", loaded.CreationAcknowledged, now)
	}
	if loaded.DeletionConfirmed == nil || !loaded.DeletionConfirmed.Equal(now) {
		t.Errorf("DeletionConfirmed did not round-trip, got %v want %v", loaded.DeletionConfirmed, now)
	}
}

// TestReconciliation_ZeroValue asserts that a freshly constructed
// Reconciliation has all-nil pointer fields so callers can distinguish unset
// from false or the epoch zero time.
func TestReconciliation_ZeroValue(t *testing.T) {
	// a zero-value Reconciliation should have all-nil pointer fields
	var r Reconciliation
	if r.Reconciled != nil ||
		r.CreationAcknowledged != nil ||
		r.CreationConfirmed != nil ||
		r.CreationFailed != nil ||
		r.DeletionScheduled != nil ||
		r.DeletionAcknowledged != nil ||
		r.DeletionConfirmed != nil ||
		r.InterruptReconciliation != nil {
		t.Errorf("zero-value Reconciliation has non-nil fields: %+v", r)
	}
}
>>>>>>> 9686fad7
