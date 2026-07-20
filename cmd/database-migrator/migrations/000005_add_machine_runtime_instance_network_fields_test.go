package migrations

import (
	"context"
	"testing"

	v0 "github.com/threeport/threeport/pkg/api/v0"
)

// TestAddMachineRuntimeInstanceNetworkColumns covers
// addMachineRuntimeInstanceNetworkColumns: when v0_machine_runtime_instances is
// missing the network columns, the add creates them while preserving existing
// rows; re-running is a no-op via the HasColumn guard.
func TestAddMachineRuntimeInstanceNetworkColumns(t *testing.T) {
	gormDb := newSqlite(t)

	// seed a v0_machine_runtime_instances table without the new columns and a
	// row whose id must survive the ADD COLUMN, standing in for a database
	// whose schema predates the field additions.
	for _, stmt := range []string{
		"CREATE TABLE v0_machine_runtime_instances (id integer primary key, name text)",
		"INSERT INTO v0_machine_runtime_instances (id, name) VALUES (1, 'mri-1')",
	} {
		if err := gormDb.Exec(stmt).Error; err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}

	// add the columns
	if err := addMachineRuntimeInstanceNetworkColumns(gormDb); err != nil {
		t.Fatalf("addMachineRuntimeInstanceNetworkColumns: %v", err)
	}

	// every column is present under the name the model declares for its field;
	// a select against each must succeed.
	for _, column := range []string{
		"ingress_rules",
		"network_cidr",
		"subnet_cidr",
		"assign_public_ip",
	} {
		if err := gormDb.Exec("SELECT " + column + " FROM v0_machine_runtime_instances").Error; err != nil {
			t.Errorf("%s should be present after add: %v", column, err)
		}
	}
	// the row survives with its id intact
	var id int
	if err := gormDb.Raw("SELECT id FROM v0_machine_runtime_instances WHERE id = 1").Scan(&id).Error; err != nil || id != 1 {
		t.Errorf("row not preserved across column add: id=%d err=%v", id, err)
	}

	// re-running is a no-op via the HasColumn guard
	if err := addMachineRuntimeInstanceNetworkColumns(gormDb); err != nil {
		t.Fatalf("addMachineRuntimeInstanceNetworkColumns re-run: %v", err)
	}
}

// TestAddMachineRuntimeInstanceNetworkColumnsFreshDbNoOp covers the
// fresh-install path: the initial migration already builds
// v0_machine_runtime_instances with the network columns, so the add must skip
// via the HasColumn guard and leave the schema intact.
func TestAddMachineRuntimeInstanceNetworkColumnsFreshDbNoOp(t *testing.T) {
	gormDb := newSqlite(t)

	// seed a v0_machine_runtime_instances table shaped like the current model
	// set: every network column already present.
	if err := gormDb.Exec("CREATE TABLE v0_machine_runtime_instances (id integer primary key, ingress_rules jsonb, network_cidr text, subnet_cidr text, assign_public_ip numeric)").Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	// addMachineRuntimeInstanceNetworkColumns must be a no-op: the columns are
	// already present so the HasColumn guard short-circuits before the ALTER
	// runs.
	if err := addMachineRuntimeInstanceNetworkColumns(gormDb); err != nil {
		t.Fatalf("addMachineRuntimeInstanceNetworkColumns on a fresh database: %v", err)
	}

	// the surviving columns are still readable
	if err := gormDb.Exec("SELECT network_cidr, subnet_cidr FROM v0_machine_runtime_instances").Error; err != nil {
		t.Errorf("v0_machine_runtime_instances network columns should be intact: %v", err)
	}
}

// TestAddMachineRuntimeInstanceNetworkColumnsMissingTableNoOp asserts the add
// skips a database that has no v0_machine_runtime_instances table at all,
// rather than erroring on the ALTER.
func TestAddMachineRuntimeInstanceNetworkColumnsMissingTableNoOp(t *testing.T) {
	// start from an empty database, standing in for a schema the initial
	// migration has not yet built.
	gormDb := newSqlite(t)

	// the HasTable guard short-circuits before any column work
	if err := addMachineRuntimeInstanceNetworkColumns(gormDb); err != nil {
		t.Fatalf("addMachineRuntimeInstanceNetworkColumns with no table: %v", err)
	}
}

// TestDown000005 covers the down migration: it drops the network columns when
// present and is a no-op when they are already absent.
func TestDown000005(t *testing.T) {
	gormDb := newSqlite(t)

	// seed the table from the model, standing in for a database that has
	// already applied Up000005: building it through the migrator gives it every
	// network column under the name the model declares.
	if err := gormDb.Migrator().CreateTable(&v0.MachineRuntimeInstance{}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Down000005 reads the gorm db from the context under the "gormdb" key.
	ctx := context.WithValue(context.Background(), "gormdb", gormDb)
	if err := Down000005(ctx, nil); err != nil {
		t.Fatalf("Down000005: %v", err)
	}

	// every network column is gone after the down migration
	for _, column := range []string{
		"ingress_rules",
		"network_cidr",
		"subnet_cidr",
		"assign_public_ip",
	} {
		if err := gormDb.Exec("SELECT " + column + " FROM v0_machine_runtime_instances").Error; err == nil {
			t.Errorf("%s should be gone after Down000005", column)
		}
	}

	// re-running is a no-op via the HasColumn guard
	if err := Down000005(ctx, nil); err != nil {
		t.Fatalf("Down000005 re-run: %v", err)
	}
}
