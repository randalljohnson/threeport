package migrations

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"gorm.io/gorm"

	v0 "github.com/threeport/threeport/pkg/api/v0"
)

// TestGetGormDbFromContextMissing asserts the helper returns an error when the
// context carries no gormdb value.
func TestGetGormDbFromContextMissing(t *testing.T) {
	// build a bare context with no gormdb value attached
	ctx := context.Background()

	// invoke the helper under test
	got, err := getGormDbFromContext(ctx)

	// verify the error surfaces the missing-value case
	if err == nil {
		t.Fatalf("expected error for missing gormdb, got nil")
	}
	if !strings.Contains(err.Error(), "could not retrieve gormdb") {
		t.Errorf("error = %q, want it to mention retrieval failure", err.Error())
	}

	// verify no db is returned when the lookup fails
	if got != nil {
		t.Errorf("expected nil db on missing key, got %v", got)
	}
}

// TestGetGormDbFromContextWrongType asserts the helper returns an error when
// the gormdb key holds a value of the wrong type.
func TestGetGormDbFromContextWrongType(t *testing.T) {
	// attach a non-gorm value under the expected key
	ctx := context.WithValue(context.Background(), "gormdb", "not-a-gorm-db")

	// invoke the helper under test
	got, err := getGormDbFromContext(ctx)

	// verify the error identifies the type mismatch
	if err == nil {
		t.Fatalf("expected error for wrong-typed gormdb, got nil")
	}
	if !strings.Contains(err.Error(), "type convert") {
		t.Errorf("error = %q, want it to mention type conversion", err.Error())
	}

	// verify no db is returned when the type assertion fails
	if got != nil {
		t.Errorf("expected nil db on wrong type, got %v", got)
	}
}

// TestGetGormDbFromContextSuccess asserts the helper returns the gorm.DB
// stored under the expected key.
func TestGetGormDbFromContextSuccess(t *testing.T) {
	// stash a bare gorm.DB pointer under the expected key
	want := &gorm.DB{}
	ctx := context.WithValue(context.Background(), "gormdb", want)

	// invoke the helper under test
	got, err := getGormDbFromContext(ctx)

	// verify no error surfaces on the happy path
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// verify the same pointer is returned unchanged
	if got != want {
		t.Errorf("returned db = %p, want %p", got, want)
	}
}

// TestUp000001MissingGormDb asserts Up000001 propagates the missing-gormdb
// error from the context helper.
func TestUp000001MissingGormDb(t *testing.T) {
	// prepare a context without a gormdb value
	ctx := context.Background()

	// invoke Up000001; it must fail before touching the database
	err := Up000001(ctx, nil)

	// verify the error came from the context helper, not a nil deref
	if err == nil {
		t.Fatalf("expected error when gormdb is absent, got nil")
	}
	if !strings.Contains(err.Error(), "could not retrieve gormdb") {
		t.Errorf("error = %q, want it to originate in the context helper", err.Error())
	}
}

// TestDown000001MissingGormDb asserts Down000001 propagates the missing-gormdb
// error from the context helper.
func TestDown000001MissingGormDb(t *testing.T) {
	// prepare a context without a gormdb value
	ctx := context.Background()

	// invoke Down000001; it must fail before touching the database
	err := Down000001(ctx, nil)

	// verify the error came from the context helper, not a nil deref
	if err == nil {
		t.Fatalf("expected error when gormdb is absent, got nil")
	}
	if !strings.Contains(err.Error(), "could not retrieve gormdb") {
		t.Errorf("error = %q, want it to originate in the context helper", err.Error())
	}
}

// TestDbInterfaces000001NotEmpty asserts the migration model set is
// populated so AutoMigrate has tables to create.
func TestDbInterfaces000001NotEmpty(t *testing.T) {
	// gather the model set under test
	got := dbInterfaces000001()

	// verify the set is not empty; an empty AutoMigrate is a silent no-op
	if len(got) == 0 {
		t.Fatalf("dbInterfaces000001 returned empty slice")
	}
}

// TestDbInterfaces000001AllPointers asserts every entry is a non-nil pointer
// so gorm.AutoMigrate can reflect on the model.
func TestDbInterfaces000001AllPointers(t *testing.T) {
	// gather the model set under test
	got := dbInterfaces000001()

	// verify each element is a non-nil pointer to a struct
	for i, m := range got {
		if m == nil {
			t.Errorf("entry %d is nil", i)
			continue
		}
		rv := reflect.ValueOf(m)
		if rv.Kind() != reflect.Ptr {
			t.Errorf("entry %d kind = %s, want ptr", i, rv.Kind())
			continue
		}
		if rv.IsNil() {
			t.Errorf("entry %d is a nil pointer", i)
			continue
		}
		if rv.Elem().Kind() != reflect.Struct {
			t.Errorf("entry %d points to %s, want struct", i, rv.Elem().Kind())
		}
	}
}

// TestDbInterfaces000001IncludesEventAndAttachedObjectReference asserts the
// two models targeted by the row-level TTL statements are present, so the
// TTL DDL in Up000001 references tables that AutoMigrate has created.
func TestDbInterfaces000001IncludesEventAndAttachedObjectReference(t *testing.T) {
	// gather the model set under test
	got := dbInterfaces000001()

	// track whether each TTL-target model appears in the set
	var haveEvent, haveAOR bool
	for _, m := range got {
		switch m.(type) {
		case *v0.Event:
			haveEvent = true
		case *v0.AttachedObjectReference:
			haveAOR = true
		}
	}

	// verify the event model is present so v0_events exists for the TTL DDL
	if !haveEvent {
		t.Errorf("dbInterfaces000001 missing *v0.Event")
	}

	// verify the attached-object-reference model is present so its TTL DDL
	// runs against an existing table
	if !haveAOR {
		t.Errorf("dbInterfaces000001 missing *v0.AttachedObjectReference")
	}
}

// TestDbInterfaces000001NoDuplicates asserts each concrete model type appears
// at most once; a duplicate would run AutoMigrate twice on the same table.
func TestDbInterfaces000001NoDuplicates(t *testing.T) {
	// gather the model set under test
	got := dbInterfaces000001()

	// collect each entry's dynamic type and count occurrences
	seen := make(map[reflect.Type]int, len(got))
	for _, m := range got {
		seen[reflect.TypeOf(m)]++
	}

	// verify no type appears more than once
	for typ, n := range seen {
		if n > 1 {
			t.Errorf("type %s appears %d times, want 1", typ, n)
		}
	}
}
