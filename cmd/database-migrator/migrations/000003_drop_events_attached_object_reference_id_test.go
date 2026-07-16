package migrations

import (
	"context"
	"testing"
)

// TestDropEventAttachedObjectReferenceColumn covers dropEventAttachedObjectReferenceColumn:
// when v0_events carries the removed column, the drop removes it while
// preserving the rest of the row; re-running is a no-op via the hasColumn guard.
func TestDropEventAttachedObjectReferenceColumn(t *testing.T) {
	gormDb := newSqlite(t)

	// seed a v0_events table with the removed column present and a row whose id
	// must survive the drop, standing in for a database whose schema predates
	// the field removal.
	for _, stmt := range []string{
		"CREATE TABLE v0_events (id integer primary key, attached_object_reference_id integer)",
		"INSERT INTO v0_events (id, attached_object_reference_id) VALUES (1, 42)",
	} {
		if err := gormDb.Exec(stmt).Error; err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}

	// drop the column
	if err := dropEventAttachedObjectReferenceColumn(gormDb); err != nil {
		t.Fatalf("dropEventAttachedObjectReferenceColumn: %v", err)
	}

	// the column is gone: a select against it must error
	if err := gormDb.Exec("SELECT attached_object_reference_id FROM v0_events").Error; err == nil {
		t.Error("attached_object_reference_id should be gone after drop")
	}
	// the row survives with its id intact
	var id int
	if err := gormDb.Raw("SELECT id FROM v0_events WHERE id = 1").Scan(&id).Error; err != nil || id != 1 {
		t.Errorf("row not preserved across column drop: id=%d err=%v", id, err)
	}

	// re-running is a no-op via the hasColumn guard
	if err := dropEventAttachedObjectReferenceColumn(gormDb); err != nil {
		t.Fatalf("dropEventAttachedObjectReferenceColumn re-run: %v", err)
	}
}

// TestDropEventAttachedObjectReferenceColumnFreshDbNoOp covers the fresh-install
// path: the initial migration builds v0_events without the removed column, so
// the drop must skip via the hasColumn guard and leave the schema intact.
func TestDropEventAttachedObjectReferenceColumnFreshDbNoOp(t *testing.T) {
	gormDb := newSqlite(t)

	// seed a v0_events table shaped like the current model set: no attached
	// object reference id column.
	if err := gormDb.Exec("CREATE TABLE v0_events (id integer primary key, reason text)").Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	// dropEventAttachedObjectReferenceColumn must be a no-op: the column is not
	// present so the hasColumn guard short-circuits before the ALTER runs.
	if err := dropEventAttachedObjectReferenceColumn(gormDb); err != nil {
		t.Fatalf("dropEventAttachedObjectReferenceColumn on a fresh database: %v", err)
	}

	// the surviving columns are still readable
	if err := gormDb.Exec("SELECT reason FROM v0_events").Error; err != nil {
		t.Errorf("v0_events.reason should be intact: %v", err)
	}
}

// TestDown000003 covers the down migration: it re-adds the column when absent
// and is a no-op when the column is already present.
func TestDown000003(t *testing.T) {
	gormDb := newSqlite(t)

	// seed a v0_events table without the removed column, standing in for a
	// database that has already applied Up000003.
	if err := gormDb.Exec("CREATE TABLE v0_events (id integer primary key)").Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Down000003 reads the gorm db from the context under the "gormdb" key.
	ctx := context.WithValue(context.Background(), "gormdb", gormDb)
	if err := Down000003(ctx, nil); err != nil {
		t.Fatalf("Down000003: %v", err)
	}

	// the column is present after the down migration
	if err := gormDb.Exec("SELECT attached_object_reference_id FROM v0_events").Error; err != nil {
		t.Errorf("attached_object_reference_id should be re-added: %v", err)
	}

	// re-running is a no-op via the hasColumn guard
	if err := Down000003(ctx, nil); err != nil {
		t.Fatalf("Down000003 re-run: %v", err)
	}
}
