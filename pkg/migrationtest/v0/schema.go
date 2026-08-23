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
// schema against the columns each model it is handed declares.  It reports
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
//
// A migration chain carrying grammar the in-memory engine cannot parse needs
// the variant that takes a live database instead.
func AssertMigrationsCoverModels(t *testing.T, versionTableName string, models []interface{}) {
	t.Helper()

	gormDb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	applyMigrations(t, gormDb, "sqlite3", versionTableName)
	assertCoverage(t, gormDb, models, gormColumns)
}

// AssertMigrationsCoverModelsOn is the same check against a database the
// caller supplies, for a migration chain whose statements only the deployed
// engine understands.  The database must be empty, so a migration built every
// table the check reads rather than the caller.
//
// The comparison skips columns the engine hides.  A hidden column belongs to
// the engine rather than to the schema a model declares: a table with no
// primary key gets one, and so does a table carrying a row-level
// time-to-live.
func AssertMigrationsCoverModelsOn(
	t *testing.T,
	gormDb *gorm.DB,
	versionTableName string,
	models []interface{},
) {
	t.Helper()

	applyMigrations(t, gormDb, "postgres", versionTableName)
	assertCoverage(t, gormDb, models, visibleColumns)
}

// applyMigrations runs every migration registered in the calling process.
func applyMigrations(t *testing.T, gormDb *gorm.DB, dialect, versionTableName string) {
	t.Helper()

	// share one connection pool so the migrations and the assertions see
	// the same database
	sqlDb, err := gormDb.DB()
	if err != nil {
		t.Fatalf("resolve sql db: %v", err)
	}

	if err := goose.SetDialect(dialect); err != nil {
		t.Fatalf("set goose dialect: %v", err)
	}
	goose.SetTableName(versionTableName)

	// the migrations read the gorm db from the context under the same key
	// the deployed migrator sets
	ctx := context.WithValue(context.Background(), "gormdb", gormDb)
	if err := goose.UpContext(ctx, sqlDb, "."); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
}

// assertCoverage compares each model's declared columns against the created ones.
func assertCoverage(
	t *testing.T,
	gormDb *gorm.DB,
	models []interface{},
	columnsOf func(*testing.T, *gorm.DB, interface{}, string) map[string]bool,
) {
	t.Helper()

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

		created := columnsOf(t, gormDb, model, stmt.Schema.Table)

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

// gormColumns reads a table's columns through the driver's own migrator.
func gormColumns(t *testing.T, gormDb *gorm.DB, model interface{}, _ string) map[string]bool {
	t.Helper()

	columnTypes, err := gormDb.Migrator().ColumnTypes(model)
	if err != nil {
		t.Fatalf("read columns for %T: %v", model, err)
	}
	created := make(map[string]bool, len(columnTypes))
	for _, columnType := range columnTypes {
		created[columnType.Name()] = true
	}

	return created
}

// visibleColumns reads a table's columns, skipping the ones the engine hides.
//
// The driver's own migrator reads the same catalog without filtering, and the
// column type it returns carries no way to ask, so this queries the catalog
// directly.
func visibleColumns(t *testing.T, gormDb *gorm.DB, _ interface{}, table string) map[string]bool {
	t.Helper()

	var names []string
	err := gormDb.Raw(`
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = ? AND is_hidden = 'NO'
	`, table).Scan(&names).Error
	if err != nil {
		t.Fatalf("read columns for %s: %v", table, err)
	}
	created := make(map[string]bool, len(names))
	for _, name := range names {
		created[name] = true
	}

	return created
}
