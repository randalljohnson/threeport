package cockroach

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	migrations "github.com/threeport/threeport/cmd/database-migrator/migrations"
)

// A migration is rerun whenever an install is interrupted part way through the
// schema, so it has to be repeatable over the tables it already built.
//
// gorm AutoMigrate takes one of two branches per model. A missing table is
// created from the struct tags and the database is never read. An existing
// table is read back out of the catalog and compared against the model, and
// corrective DDL is emitted for each difference the comparison believes it
// found. CockroachDB answers those catalog reads differently from PostgreSQL,
// so the second branch infers differences that are not there: it lists a unique
// index back as a unique constraint, and it reports one integer column as int8,
// bigint and precision 64 at once.
//
// The migration therefore creates the tables it finds missing and enters the
// second branch for none of them. These tests cover both halves of that: what
// the second branch does when it is entered, and that skipping it still leaves
// a rerun able to finish a half-built schema without unguarding a column.

// nameGuarded is a model whose name column is guarded by a partial unique
// index and by no unique constraint, which is the tag shape a catalog
// comparison disagrees with. It is declared here rather than borrowed from the
// api types because the property under test is what that shape costs, and every
// api type is free to change its tags.
type nameGuarded struct {
	gorm.Model
	Name *string `gorm:"not null;uniqueIndex:,where:deleted_at IS NULL"`
}

// lateArrival is a model the first migration leaves unbuilt, standing for the
// tables an interrupted install never reached. What is under test is whether a
// second migration reaches it past a table already there, so it carries the
// same tags as the model beside it rather than a shape of its own.
type lateArrival struct {
	gorm.Model
	Name *string `gorm:"not null;uniqueIndex:,where:deleted_at IS NULL"`
}

// TestRepeatedAutoMigrateIsRejected covers what a rerun costs on the branch the
// migration avoids: the second pass is refused for the drop of a constraint
// that was never created. It is the evidence for creating missing tables
// directly, and it fails once the database and gorm stop disagreeing, after
// which creating them directly stops being needed.
func TestRepeatedAutoMigrateIsRejected(t *testing.T) {
	db := freshDatabase(t, "automigrate_repeat")

	require.NoError(t, db.AutoMigrate(&nameGuarded{}),
		"the first pass builds the table")

	err := db.AutoMigrate(&nameGuarded{})
	require.Error(t, err, "the second pass is rejected")
	// the message names the table twice, so only its tail is stable to match on
	assert.Contains(t, err.Error(), "does not exist",
		"the rejection is the drop of a constraint that was never created")
}

// TestCreatingMissingTablesCompletesAPartialSchema covers the two halves of a
// repeatable migration on the tag shape that breaks a catalog comparison: a
// second run over a schema the first left half built creates the tables it
// never reached, and the index guarding a column the first run did build still
// refuses a duplicate. The second half says the repeatability was not bought
// by leaving the column unguarded.
func TestCreatingMissingTablesCompletesAPartialSchema(t *testing.T) {
	db := freshDatabase(t, "create_missing_tables")

	// one model and not both, the state an install interrupted part way
	// through the schema leaves behind
	createMissingTables(t, db, &nameGuarded{})

	createMissingTables(t, db, &nameGuarded{}, &lateArrival{})
	assert.True(t, db.Migrator().HasTable(&lateArrival{}),
		"the table the first run never reached is built")

	// the step the migration does not take is a constraint drop, so the column
	// that drop would have unguarded has to still be guarded
	name := "one-per-name"
	require.NoError(t, db.Create(&nameGuarded{Name: &name}).Error,
		"the first row is accepted")
	assert.Error(t, db.Create(&nameGuarded{Name: &name}).Error,
		"a second row under the same name is still refused")
}

// TestInitialMigrationIsIdempotent runs the deployed migration twice over one
// database and requires both runs to succeed. The two tests above prove the
// property on two models declared here; this one proves it on every model the
// control plane persists, so a field whose tag first makes a second run fail is
// caught on the commit that adds the tag rather than on the install that reruns
// the migration.
//
// It runs the migration function itself rather than a migration assembled here,
// so the model list, the ordering, and the statements that follow the schema
// are the deployed ones.
func TestInitialMigrationIsIdempotent(t *testing.T) {
	db := freshDatabase(t, "initial_migration_idempotent")

	// the migration looks its handle up under the bare string "gormdb", and a
	// context key matches on type as well as value, so a defined string type
	// here would read back as absent
	//nolint:staticcheck // SA1029: the key has to match what the migration reads
	ctx := context.WithValue(context.Background(), "gormdb", db)

	// the sql handle is unused: the migration works through the gorm handle in
	// the context
	require.NoError(t, migrations.Up000001(ctx, nil),
		"the first migration builds the schema")
	require.NoError(t, migrations.Up000001(ctx, nil),
		"the second migration finds the schema already built and adds nothing")
}

// createMissingTables creates each model that has no table yet, which is the
// step the deployed migration takes in place of a full automigrate. It is
// spelled out here so a test can run it over models the migration's own list
// does not carry.
func createMissingTables(t *testing.T, db *gorm.DB, models ...interface{}) {
	t.Helper()

	for _, model := range models {
		if db.Migrator().HasTable(model) {
			continue
		}
		if err := db.Migrator().CreateTable(model); err != nil {
			t.Fatalf("create table for %T: %v", model, err)
		}
	}
}
