// Package migrationtest holds helpers that check a database migration chain
// against the models the chain is meant to persist.  It imports the testing
// package, so only test files import it.
package migrationtest

import (
	"context"
	"sort"
	"testing"

	goose "github.com/pressly/goose/v3"
	sqlite "gorm.io/driver/sqlite"
	gorm "gorm.io/gorm"
)

// AssertMigrationsCoverModels applies every migration registered in the
// calling process to a fresh in-memory database, then checks the resulting
// schema against the columns each of the given models declares.  It reports
// both fields left without a column and columns left without a field, so one
// run shows the whole drift.
//
// The caller owns the migrations under test: they register themselves as a
// side effect of the caller importing the package that holds them, and a
// caller that imports none asserts nothing.  Migrations are read from the
// caller's working directory, which for a test is the directory of the
// package under test.
//
// versionTableName is where the migration tool records which migrations it
// has applied, and it matches the name the deployed migrator sets.  The tool
// keeps that setting and the chosen dialect in process-wide state, so this
// must not run in parallel with anything else that migrates.
func AssertMigrationsCoverModels(t *testing.T, versionTableName string, models []interface{}) {
	t.Helper()

	// build the schema a deployed database would have after an upgrade
	gormDb := migratedSchema(t, versionTableName)

	for _, model := range models {
		// resolve the columns the model's fields declare, which accounts
		// for embedded structs, column overrides and excluded fields
		stmt := &gorm.Statement{DB: gormDb}
		if err := stmt.Parse(model); err != nil {
			t.Fatalf("parse %T: %v", model, err)
		}
		declared := make(map[string]bool, len(stmt.Schema.DBNames))
		for _, name := range stmt.Schema.DBNames {
			declared[name] = true
		}

		// a model no migration creates reads as total drift
		if !gormDb.Migrator().HasTable(model) {
			t.Errorf("no migration creates table %s for %T", stmt.Schema.Table, model)
			continue
		}

		// read the columns the migration chain actually created
		columnTypes, err := gormDb.Migrator().ColumnTypes(model)
		if err != nil {
			t.Fatalf("read columns for %T: %v", model, err)
		}
		created := make(map[string]bool, len(columnTypes))
		for _, columnType := range columnTypes {
			created[columnType.Name()] = true
		}

		// a declared field with no column means a migration was never written
		var missingColumns []string
		for name := range declared {
			if !created[name] {
				missingColumns = append(missingColumns, name)
			}
		}

		// a column with no declared field means a migration never dropped it
		var missingFields []string
		for name := range created {
			if !declared[name] {
				missingFields = append(missingFields, name)
			}
		}

		// report both directions so one run shows the whole drift
		sort.Strings(missingColumns)
		sort.Strings(missingFields)
		if len(missingColumns) > 0 {
			t.Errorf("%s has fields with no column: %v", stmt.Schema.Table, missingColumns)
		}
		if len(missingFields) > 0 {
			t.Errorf("%s has columns with no field: %v", stmt.Schema.Table, missingFields)
		}
	}
}

// migratedSchema applies every migration registered in the calling process to
// a fresh in-memory database and returns a handle on the resulting schema.
// versionTableName is where the migration tool records which migrations it has
// applied.
func migratedSchema(t *testing.T, versionTableName string) *gorm.DB {
	t.Helper()

	gormDb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	// share one connection pool so the migrations and the assertions see
	// the same in-memory database
	sqlDb, err := gormDb.DB()
	if err != nil {
		t.Fatalf("resolve sql db: %v", err)
	}

	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("set goose dialect: %v", err)
	}
	goose.SetTableName(versionTableName)

	// the migrations read the gorm db from the context under the same key
	// the deployed migrator sets
	ctx := context.WithValue(context.Background(), "gormdb", gormDb)
	if err := goose.UpContext(ctx, sqlDb, "."); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	return gormDb
}
