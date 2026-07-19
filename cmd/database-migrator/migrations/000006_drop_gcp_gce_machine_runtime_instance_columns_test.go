package migrations

import (
	"context"
	"testing"
)

// TestDropGcpGceMachineRuntimeInstanceColumns covers
// dropGcpGceMachineRuntimeInstanceColumns: when
// v0_gcp_gce_machine_runtime_instances carries the removed columns, the drop
// removes them while preserving the rest of the row; re-running is a no-op via
// the hasColumn guard.
func TestDropGcpGceMachineRuntimeInstanceColumns(t *testing.T) {
	gormDb := newSqlite(t)

	// seed a v0_gcp_gce_machine_runtime_instances table with the removed
	// columns present and a row whose id must survive the drop, standing in for
	// a database whose schema predates the field removals.
	for _, stmt := range []string{
		"CREATE TABLE v0_gcp_gce_machine_runtime_instances (id integer primary key, zone text, network_id text, ssh_source_ranges jsonb)",
		`INSERT INTO v0_gcp_gce_machine_runtime_instances (id, zone, network_id, ssh_source_ranges) VALUES (1, 'zone-1', 'net-1', '["0.0.0.0/0"]')`,
	} {
		if err := gormDb.Exec(stmt).Error; err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}

	// drop the columns
	if err := dropGcpGceMachineRuntimeInstanceColumns(gormDb); err != nil {
		t.Fatalf("dropGcpGceMachineRuntimeInstanceColumns: %v", err)
	}

	// both columns are gone: a select against each must error
	for _, column := range []string{"network_id", "ssh_source_ranges"} {
		if err := gormDb.Exec("SELECT " + column + " FROM v0_gcp_gce_machine_runtime_instances").Error; err == nil {
			t.Errorf("%s should be gone after drop", column)
		}
	}
	// the row survives with its id and remaining columns intact
	var zone string
	if err := gormDb.Raw("SELECT zone FROM v0_gcp_gce_machine_runtime_instances WHERE id = 1").Scan(&zone).Error; err != nil || zone != "zone-1" {
		t.Errorf("row not preserved across column drop: zone=%q err=%v", zone, err)
	}

	// re-running is a no-op via the hasColumn guard
	if err := dropGcpGceMachineRuntimeInstanceColumns(gormDb); err != nil {
		t.Fatalf("dropGcpGceMachineRuntimeInstanceColumns re-run: %v", err)
	}
}

// TestDropGcpGceMachineRuntimeInstanceColumnsFreshDbNoOp covers the
// fresh-install path: the initial migration builds
// v0_gcp_gce_machine_runtime_instances without the removed columns, so the drop
// must skip via the hasColumn guard and leave the schema intact.
func TestDropGcpGceMachineRuntimeInstanceColumnsFreshDbNoOp(t *testing.T) {
	gormDb := newSqlite(t)

	// seed a v0_gcp_gce_machine_runtime_instances table shaped like the current
	// model set: neither the network identifier nor the source ranges column.
	if err := gormDb.Exec("CREATE TABLE v0_gcp_gce_machine_runtime_instances (id integer primary key, zone text)").Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	// dropGcpGceMachineRuntimeInstanceColumns must be a no-op: neither column
	// is present so the hasColumn guard short-circuits before the ALTER runs.
	if err := dropGcpGceMachineRuntimeInstanceColumns(gormDb); err != nil {
		t.Fatalf("dropGcpGceMachineRuntimeInstanceColumns on a fresh database: %v", err)
	}

	// the surviving column is still readable
	if err := gormDb.Exec("SELECT zone FROM v0_gcp_gce_machine_runtime_instances").Error; err != nil {
		t.Errorf("v0_gcp_gce_machine_runtime_instances.zone should be intact: %v", err)
	}
}

// TestDown000006 covers the down migration: it re-adds both columns when absent
// and is a no-op when they are already present.
func TestDown000006(t *testing.T) {
	gormDb := newSqlite(t)

	// seed a v0_gcp_gce_machine_runtime_instances table without the removed
	// columns, standing in for a database that has already applied Up000006.
	if err := gormDb.Exec("CREATE TABLE v0_gcp_gce_machine_runtime_instances (id integer primary key)").Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Down000006 reads the gorm db from the context under the "gormdb" key.
	ctx := context.WithValue(context.Background(), "gormdb", gormDb)
	if err := Down000006(ctx, nil); err != nil {
		t.Fatalf("Down000006: %v", err)
	}

	// both columns are present after the down migration
	for _, column := range []string{"network_id", "ssh_source_ranges"} {
		if err := gormDb.Exec("SELECT " + column + " FROM v0_gcp_gce_machine_runtime_instances").Error; err != nil {
			t.Errorf("%s should be re-added: %v", column, err)
		}
	}

	// re-running is a no-op via the hasColumn guard
	if err := Down000006(ctx, nil); err != nil {
		t.Fatalf("Down000006 re-run: %v", err)
	}
}
