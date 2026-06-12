package v0

import (
	"fmt"
	"reflect"

	"gorm.io/gorm"

	util "github.com/threeport/threeport/pkg/util/v0"
)

// This file holds the helpers for writing GORM update hooks that stay
// correct across the two call shapes GORM supports. The asymmetry
// between the call shapes is subtle and easy to miss, so the next
// block explains the mental model once in depth and the per-function
// docstrings below stay focused on what each helper does.
//
// "Hook receiver" here means the *T receiver of the GORM lifecycle
// hook methods (BeforeCreate, BeforeUpdate, BeforeDelete, AfterCreate,
// AfterUpdate, AfterDelete) that the SDK generates on every API type
// into *_validate_gen.go files. GORM calls them automatically when a
// row is being created, updated, or deleted.
//
// The update hooks fire under two call shapes, and the receiver means
// a different thing in each.
//
// The mental model starts with the generated client signatures (T
// stands in for any API type). Note the DELETE row: its HTTP method
// is DELETE but the GORM call is Updates, the same as PATCH.
//
//	client signature                            | HTTP   | GORM    | semantic       | reason
//	--------------------------------------------|--------|---------|----------------|-----------------------------
//	ReplaceT(apiClient, apiAddr, *T) (*T, error)| PUT    | Save    | full replace   | every column lands
//	UpdateT(apiClient, apiAddr, *T) (*T, error) | PATCH  | Updates | partial update | only sent fields land
//	DeleteT(apiClient, apiAddr, id) (*T, error) | DELETE | Updates | partial update | only deletion-trigger flags
//
// Generated handlers pick the call shape from the HTTP method. The
// UpdateT PATCH handlers use Updates so only fields the client
// actually sent get written. The ReplaceT PUT handlers use Save so
// every column on the bound row lands, including fields the caller
// cleared. DeleteT handlers also use Updates to write only the
// reconciliation flag columns (DeletionScheduled, etc.) without
// touching the rest of the row. GORM fires BeforeUpdate from
// whatever call shape the caller used.
//
// Under the hood the two call shapes differ in what the hook receiver
// holds. The examples use `loaded` (a row pulled from the DB), `patch`
// (the caller's partial payload), and `obj` (a complete inbound row).
//
//   - Model(&loaded).Updates(&patch): the caller has a payload with
//     only the fields it wants to change; GORM applies those over the
//     loaded row and leaves the rest. Hook receiver is the loaded DB
//     row; the caller's new values live in tx.Statement.Dest.
//
//   - Save(&obj): the caller has a complete object and wants every
//     field persisted. Receiver and tx.Statement.Dest are the same
//     inbound object that already holds the new values.
//
// Create has only the Save shape: receiver, Model, and Dest all
// point at the inbound object.
//
// Updates fire from either shape. GORM merges the incoming values
// onto the receiver between the before-update and after-update
// callbacks, so by an after-update hook the receiver already reflects
// the committed result.
//
// To stay correct regardless of shape, hooks read through helpers
// rather than the receiver directly. IsFieldChanged is the high-level
// helper to reach for first; the rest are lower-level building blocks:
//
//   - IsFieldChanged for per-field change detection (works under both
//     PATCH and PUT; handles the DB load internally).
//   - IncomingValues(tx) for the values being written.
//   - LoadObjectFromDB(tx) for a fresh read of the committed row.
//   - IsFullReplace to identify the Save-shape call (PUT) where a nil
//     inbound field means an explicit clear.
//   - IsPartialUpdate to identify the Updates-shape call (PATCH and
//     DELETE) where a nil inbound field means absent-and-unchanged.
//
// Layer choice for the PUT/PATCH naming
//
// The PUT/PATCH distinction can be named at four different layers, and
// the layer matters because threeport's DeleteX handlers also use
// GORM's Updates internally to flip a few reconciliation flags. Any
// name pulled from the HTTP-verb or threeport-client-verb layer would
// therefore report true for DELETE too, which is misleading for hooks
// that want to reason about "is this a partial update".
//
// We chose the semantic layer (IsFullReplace / IsPartialUpdate)
// because it describes the GORM call shape directly without leaking
// HTTP or client vocabulary, and carries no DELETE caveat.
//
//   layer             | true on PUT   | true on PATCH or DELETE | DELETE caveat
//   ------------------|---------------|-------------------------|--------------
//   GORM method       | IsSave        | IsUpdates               | no
//   semantic (chosen) | IsFullReplace | IsPartialUpdate         | no
//   HTTP verb         | IsPut         | IsPatch                 | yes
//   threeport client  | IsReplace     | IsUpdate                | yes

// IsFieldChanged reports whether the named field is being modified by
// the current GORM update. Reach for this first for any per-field
// change detection in a hook; it works under both PATCH and PUT call
// shapes.
//
// Under PATCH (Updates), uses tx.Statement.Changed which correctly
// compares the inbound patch against the loaded row (no DB read).
// Under PUT (Save), loads the committed row via LoadObjectFromDB and
// compares the named field by reflection. Each call may incur one DB
// read under the PUT path; callers checking several fields in the
// same hook will incur one read per call, which is fine in practice.
//
// Returns an error if the named field doesn't exist on the type, the
// row has no ID, or the DB load fails. Callers in immutability hooks
// should return the error so GORM aborts the transaction rather than
// proceeding with an undetermined change status.
func IsFieldChanged(tx *gorm.DB, fieldName string) (bool, error) {
	// PATCH fast-path: Statement.Model is the loaded row, Statement.Dest
	// is the inbound patch, and they're different objects. GORM's
	// Statement.Changed compares them directly, so no DB read of our own
	// is needed.
	if IsPartialUpdate(tx) {
		// validate the field exists on the schema; tx.Statement.Changed
		// silently returns false for unknown names, which would let a
		// typo'd field slip through an immutability check on PATCH.
		if tx.Statement.Schema != nil && tx.Statement.Schema.LookUpField(fieldName) == nil {
			return false, fmt.Errorf("IsFieldChanged: field %s not found on %T", fieldName, tx.Statement.Dest)
		}
		return tx.Statement.Changed(fieldName), nil
	}

	// PUT path: Model == Dest, so the inbound IS the receiver. Under
	// Save, tx.Statement.Changed always reports false because there is
	// no separate loaded row to diff against; LoadObjectFromDB pulls
	// the committed row so we can compare manually.
	incoming := tx.Statement.Dest
	pre, err := LoadObjectFromDB(tx)
	if err != nil {
		return false, fmt.Errorf("IsFieldChanged: %w", err)
	}

	// Both pre and incoming are *T; .Elem() unwraps to T so FieldByName
	// can read named struct fields via reflection.
	preVal := reflect.ValueOf(pre).Elem().FieldByName(fieldName)
	newVal := reflect.ValueOf(incoming).Elem().FieldByName(fieldName)
	if !preVal.IsValid() || !newVal.IsValid() {
		return false, fmt.Errorf("IsFieldChanged: field %s not found on %T", fieldName, incoming)
	}

	// DeepEqual handles pointer fields, slices, maps, and primitives
	// uniformly. For *uint FK fields this correctly distinguishes nil
	// from non-nil and any non-equal pair of values.
	return !reflect.DeepEqual(preVal.Interface(), newVal.Interface()), nil
}

// IncomingValues returns the inbound values for the current update.
// Under PATCH this is the patch struct; under PUT it is the inbound
// object (same as the hook receiver). The helper exists so callers
// don't have to remember which call shape they're in.
func IncomingValues(tx *gorm.DB) interface{} {
	return tx.Statement.Dest
}

// LoadObjectFromDB returns a newly-allocated instance of the current
// row's type populated from the database by ID via a fresh session
// that does not inherit the current statement's clauses. Reads
// tx.Statement.Model for the type and ID; under PATCH that is the
// loaded row (which always has the ID), and under PUT that is the
// inbound object (which also has the ID because the caller is
// updating an existing row). Returns an error if Model has no ID or
// the DB load fails.
func LoadObjectFromDB(tx *gorm.DB) (interface{}, error) {
	model := tx.Statement.Model
	id := util.ObjectID(model)
	if id == nil {
		return nil, fmt.Errorf("LoadObjectFromDB: %T has no ID", model)
	}
	loaded := reflect.New(reflect.TypeOf(model).Elem()).Interface()
	if err := NewCleanSession(tx).First(loaded, *id).Error; err != nil {
		return nil, fmt.Errorf(
			"failed to load %s/%d from database: %w",
			util.ObjectTypeName(model), *id, err,
		)
	}
	return loaded, nil
}

// IsFullReplace reports whether the current update is a PUT (Save,
// where Model == Dest) rather than a PATCH (Updates, where they
// differ). The distinction matters when a nil inbound field means an
// explicit clear under PUT but absent-and-unchanged under PATCH.
func IsFullReplace(tx *gorm.DB) bool {
	return tx.Statement.Model == tx.Statement.Dest
}

// IsPartialUpdate reports whether the current update is the
// Updates-shape call that PATCH and DELETE handlers both use (where
// Model != Dest).
func IsPartialUpdate(tx *gorm.DB) bool {
	return tx.Statement.Model != tx.Statement.Dest
}
