package migrations

import (
	"context"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newSqlite opens a fresh in-memory sqlite database. sqlite stands in for the
// deployed CockroachDB; the migration uses no CockroachDB-specific SQL.
func newSqlite(t *testing.T) *gorm.DB {
	t.Helper()
	gormDb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	return gormDb
}

func TestRenameWorkloadSchema(t *testing.T) {
	gormDb := newSqlite(t)

	// seed the pre-migration workload tables a deployed database holds, with
	// rows whose survival across the rename we assert.
	for _, stmt := range []string{
		"CREATE TABLE v0_workload_definitions (id integer primary key, name text)",
		"INSERT INTO v0_workload_definitions (id, name) VALUES (1, 'wd-1')",
		"CREATE TABLE v0_gateway_definitions (id integer primary key, workload_definition_id integer)",
		"INSERT INTO v0_gateway_definitions (id, workload_definition_id) VALUES (1, 1)",
		"CREATE TABLE v0_workload_events (id integer primary key)",
	} {
		if err := gormDb.Exec(stmt).Error; err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}

	if err := renameWorkloadSchema(gormDb); err != nil {
		t.Fatalf("renameWorkloadSchema: %v", err)
	}

	m := gormDb.Migrator()

	// the workload table is renamed and its row survives.
	if !m.HasTable("v0_kubernetes_workload_definitions") || m.HasTable("v0_workload_definitions") {
		t.Error("workload definitions table should be renamed to kubernetes workload definitions")
	}
	var name string
	if err := gormDb.Raw("SELECT name FROM v0_kubernetes_workload_definitions WHERE id = 1").Scan(&name).Error; err != nil || name != "wd-1" {
		t.Errorf("row not preserved across table rename: name=%q err=%v", name, err)
	}

	// the foreign-key column is renamed on a referencing table and its value
	// survives, the orphaning AutoMigrate alone would cause. Raw queries are the
	// definitive check: the new column is readable with the preserved value, and
	// the old column no longer exists.
	var fk int
	if err := gormDb.Raw("SELECT kubernetes_workload_definition_id FROM v0_gateway_definitions WHERE id = 1").Scan(&fk).Error; err != nil {
		t.Errorf("renamed fk column should be queryable: %v", err)
	} else if fk != 1 {
		t.Errorf("fk value not preserved across column rename: got %d", fk)
	}
	if err := gormDb.Exec("SELECT workload_definition_id FROM v0_gateway_definitions").Error; err == nil {
		t.Error("old fk column workload_definition_id should be gone after rename")
	}

	// the workload event table is dropped.
	if m.HasTable("v0_workload_events") {
		t.Error("v0_workload_events should be dropped")
	}
}

func TestAutoMigrateIsPortable(t *testing.T) {
	gormDb := newSqlite(t)

	// AutoMigrate of the full model set must run on sqlite for the migration to
	// be portable; a fresh empty database has no rows to trip a not-null add.
	if err := gormDb.AutoMigrate(dbInterfaces000002()...); err != nil {
		t.Fatalf("AutoMigrate of the full model set failed on sqlite: %v", err)
	}
	m := gormDb.Migrator()
	if !m.HasTable("v0_gcp_gce_machine_runtime_instances") {
		t.Error("AutoMigrate should have created v0_gcp_gce_machine_runtime_instances")
	}
	if !m.HasTable("v0_kubernetes_workload_definitions") {
		t.Error("AutoMigrate should have created v0_kubernetes_workload_definitions")
	}
}

// TestRenameWorkloadSchemaFreshDbNoOp covers the fresh-install path: the initial
// migration already creates the kubernetes workload tables, so renameWorkloadSchema
// must skip every rename without error and leave that schema intact.
func TestRenameWorkloadSchemaFreshDbNoOp(t *testing.T) {
	gormDb := newSqlite(t)

	// simulate a fresh install: the initial migration's AutoMigrate created the
	// kubernetes workload tables directly and never the old workload tables.
	if err := gormDb.AutoMigrate(dbInterfaces000002()...); err != nil {
		t.Fatalf("AutoMigrate of the full model set failed on sqlite: %v", err)
	}

	// renameWorkloadSchema must be a no-op: the old workload tables never existed,
	// so the HasTable guards skip every rename and the drop.
	if err := renameWorkloadSchema(gormDb); err != nil {
		t.Fatalf("renameWorkloadSchema on a fresh database: %v", err)
	}

	m := gormDb.Migrator()
	// the kubernetes workload tables survive untouched
	if !m.HasTable("v0_kubernetes_workload_definitions") {
		t.Error("kubernetes workload table should survive the no-op rename")
	}
	// no old workload table is created on a fresh database
	if m.HasTable("v0_workload_definitions") {
		t.Error("no old workload table should exist on a fresh database")
	}
}

// TestDown000002 covers the down migration: it reverses the table and column
// renames, preserving rows, and drops the gce machine runtime tables. The dropped
// workload event table is forward-only and is not restored.
func TestDown000002(t *testing.T) {
	gormDb := newSqlite(t)

	// seed the post-up schema: a kubernetes workload table with a row, a
	// referencing table carrying the renamed foreign-key column, and the gce
	// tables the down migration drops.
	for _, stmt := range []string{
		"CREATE TABLE v0_kubernetes_workload_definitions (id integer primary key, name text)",
		"INSERT INTO v0_kubernetes_workload_definitions (id, name) VALUES (1, 'wd-1')",
		"CREATE TABLE v0_gateway_definitions (id integer primary key, kubernetes_workload_definition_id integer)",
		"INSERT INTO v0_gateway_definitions (id, kubernetes_workload_definition_id) VALUES (1, 1)",
		"CREATE TABLE v0_gcp_gce_machine_runtime_definitions (id integer primary key)",
		"CREATE TABLE v0_gcp_gce_machine_runtime_instances (id integer primary key)",
	} {
		if err := gormDb.Exec(stmt).Error; err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}

	// Down000002 reads the gorm db from the context under the "gormdb" key.
	ctx := context.WithValue(context.Background(), "gormdb", gormDb)
	if err := Down000002(ctx, nil); err != nil {
		t.Fatalf("Down000002: %v", err)
	}

	m := gormDb.Migrator()

	// the table rename is reversed and its row survives
	if !m.HasTable("v0_workload_definitions") || m.HasTable("v0_kubernetes_workload_definitions") {
		t.Error("kubernetes workload definitions table should be renamed back")
	}
	var name string
	if err := gormDb.Raw("SELECT name FROM v0_workload_definitions WHERE id = 1").Scan(&name).Error; err != nil || name != "wd-1" {
		t.Errorf("row not preserved across reverse table rename: name=%q err=%v", name, err)
	}

	// the foreign-key column rename is reversed and its value survives
	var fk int
	if err := gormDb.Raw("SELECT workload_definition_id FROM v0_gateway_definitions WHERE id = 1").Scan(&fk).Error; err != nil {
		t.Errorf("reversed fk column should be queryable: %v", err)
	} else if fk != 1 {
		t.Errorf("fk value not preserved across reverse column rename: got %d", fk)
	}

	// the gce machine runtime tables are dropped
	if m.HasTable("v0_gcp_gce_machine_runtime_definitions") || m.HasTable("v0_gcp_gce_machine_runtime_instances") {
		t.Error("gce machine runtime tables should be dropped by the down migration")
	}
}
