package migrations

import (
	"context"
	"testing"
)

// TestAddMachineRuntimeInstanceSubnetIdColumn covers
// addMachineRuntimeInstanceSubnetIdColumn: when v0_machine_runtime_instances is
// missing the column, the add creates it while preserving existing rows;
// re-running is a no-op via the hasColumn guard.
func TestAddMachineRuntimeInstanceSubnetIdColumn(t *testing.T) {
	gormDb := newSqlite(t)

	// seed a v0_machine_runtime_instances table without the new column and a
	// row whose id must survive the ADD COLUMN, standing in for a database
	// whose schema predates the field addition.
	for _, stmt := range []string{
		"CREATE TABLE v0_machine_runtime_instances (id integer primary key, name text)",
		"INSERT INTO v0_machine_runtime_instances (id, name) VALUES (1, 'mri-1')",
	} {
		if err := gormDb.Exec(stmt).Error; err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}

	// add the column
	if err := addMachineRuntimeInstanceSubnetIdColumn(gormDb); err != nil {
		t.Fatalf("addMachineRuntimeInstanceSubnetIdColumn: %v", err)
	}

	// the column is present: a select against it must succeed
	if err := gormDb.Exec("SELECT subnet_id FROM v0_machine_runtime_instances").Error; err != nil {
		t.Errorf("subnet_id should be present after add: %v", err)
	}
	// the row survives with its id intact
	var id int
	if err := gormDb.Raw("SELECT id FROM v0_machine_runtime_instances WHERE id = 1").Scan(&id).Error; err != nil || id != 1 {
		t.Errorf("row not preserved across column add: id=%d err=%v", id, err)
	}

	// re-running is a no-op via the hasColumn guard
	if err := addMachineRuntimeInstanceSubnetIdColumn(gormDb); err != nil {
		t.Fatalf("addMachineRuntimeInstanceSubnetIdColumn re-run: %v", err)
	}
}

// TestAddMachineRuntimeInstanceSubnetIdColumnFreshDbNoOp covers the
// fresh-install path: the initial migration already builds
// v0_machine_runtime_instances with the column, so the add must skip via the
// hasColumn guard and leave the schema intact.
func TestAddMachineRuntimeInstanceSubnetIdColumnFreshDbNoOp(t *testing.T) {
	gormDb := newSqlite(t)

	// seed a v0_machine_runtime_instances table shaped like the current model
	// set: subnet_id already present.
	if err := gormDb.Exec("CREATE TABLE v0_machine_runtime_instances (id integer primary key, subnet_id text)").Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	// addMachineRuntimeInstanceSubnetIdColumn must be a no-op: the column is
	// already present so the hasColumn guard short-circuits before the ALTER
	// runs.
	if err := addMachineRuntimeInstanceSubnetIdColumn(gormDb); err != nil {
		t.Fatalf("addMachineRuntimeInstanceSubnetIdColumn on a fresh database: %v", err)
	}

	// the surviving column is still readable
	if err := gormDb.Exec("SELECT subnet_id FROM v0_machine_runtime_instances").Error; err != nil {
		t.Errorf("v0_machine_runtime_instances.subnet_id should be intact: %v", err)
	}
}

// TestDown000004 covers the down migration: it drops the column when present
// and is a no-op when the column is already absent.
func TestDown000004(t *testing.T) {
	gormDb := newSqlite(t)

	// seed a v0_machine_runtime_instances table with the column present,
	// standing in for a database that has already applied Up000004.
	if err := gormDb.Exec("CREATE TABLE v0_machine_runtime_instances (id integer primary key, subnet_id text)").Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Down000004 reads the gorm db from the context under the "gormdb" key.
	ctx := context.WithValue(context.Background(), "gormdb", gormDb)
	if err := Down000004(ctx, nil); err != nil {
		t.Fatalf("Down000004: %v", err)
	}

	// the column is gone after the down migration
	if err := gormDb.Exec("SELECT subnet_id FROM v0_machine_runtime_instances").Error; err == nil {
		t.Error("subnet_id should be gone after Down000004")
	}

	// re-running is a no-op via the hasColumn guard
	if err := Down000004(ctx, nil); err != nil {
		t.Fatalf("Down000004 re-run: %v", err)
	}
}
