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
// The mental model starts with the two generated client signatures:
//
//	client signature                             | HTTP method | GORM method
//	---------------------------------------------|-------------|------------
//	UpdateX(apiClient, apiAddr, *X) (*X, error)  | PATCH       | Updates
//	ReplaceX(apiClient, apiAddr, *X) (*X, error) | PUT         | Save
//
// Generated handlers pick the call shape from the HTTP method. The
// UpdateX PATCH handlers use Updates so only fields the client
// actually sent get written. The ReplaceX PUT handlers use Save so
// every column on the bound row lands, including fields the caller
// cleared. DeleteX handlers also use Updates to flip a few
// reconciliation flags without touching the rest of the row. GORM
// fires BeforeUpdate from whatever call shape the caller used.
//
// Examples (all from pkg/api-server/v0/handlers/*_gen.go):
//
//	type                        | function                          | HTTP   | reason
//	----------------------------|-----------------------------------|--------|-----------------------
//	AttachedObjectReference     | UpdateAttachedObjectReference     | PATCH  | only sent fields land
//	KubernetesWorkloadInstance  | ReplaceKubernetesWorkloadInstance | PUT    | every column lands
//	KubernetesRuntimeDefinition | DeleteKubernetesRuntimeDefinition | DELETE | flips a few flags only
//
// Under the hood the two call shapes differ in what the hook receiver
// holds:
//
//   - Model(&loaded).Updates(&patch) — the caller has a payload with
//     only the fields it wants to change; GORM applies those over the
//     loaded row and leaves the rest. Hook receiver is the loaded DB
//     row; the caller's new values live in tx.Statement.Dest.
//
//   - Save(&obj) — the caller has a complete object and wants every
//     field persisted. Receiver and tx.Statement.Dest are the same
//     inbound object that already holds the new values.
//
// Create has only the Save shape: receiver, Model, and Dest all
// point at the inbound object.
//
// GORM merges the incoming values onto the receiver between the
// before-update and after-update callbacks, so by an after-update
// hook the receiver already reflects the committed result under
// both shapes.
//
// To stay correct regardless of shape, hooks read through helpers
// rather than the receiver directly. IsFieldChanged is the high-level
// helper to reach for first; the rest are lower-level building blocks:
//
//   - IsFieldChanged for per-field change detection (works under both
//     PATCH and PUT; handles the DB load internally).
//   - IncomingValues for the values being written.
//   - LoadObjFromDB for a fresh read of the committed row.
//   - IsFullReplace to distinguish PUT (where a nil inbound field means
//     an explicit clear) from PATCH (where it means absent-and-unchanged).

// IncomingValues returns tx.Statement.Dest when it differs from the
// receiver, otherwise the receiver itself. Use in before-hooks that
// need to operate on the caller-supplied values regardless of call
// shape.
func IncomingValues(tx *gorm.DB, receiver interface{}) interface{} {
	if dest := tx.Statement.Dest; dest != nil && dest != receiver {
		return dest
	}
	return receiver
}

// IsFullReplace reports whether the current update is a full replace (a
// PUT via Save, where Model == Dest) rather than a partial patch
// (Updates, where they differ). The distinction matters when a nil
// inbound field can mean two different things: an explicit clear on a
// full replace, or an absent-and-unchanged field on a patch. See the
// top of this file for the full call-shape model.
func IsFullReplace(tx *gorm.DB, receiver interface{}) bool {
	return tx.Statement.Dest == receiver
}

// IsFieldChanged reports whether the named field is being modified by
// the current GORM update. Reach for this first for any per-field
// change detection in a hook — it works under both PATCH and PUT call
// shapes.
//
// Under PATCH (Updates), uses tx.Statement.Changed which correctly
// compares the inbound patch against the loaded row (no DB read).
// Under PUT (Save), loads the committed row via LoadObjFromDB and
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
	if tx.Statement.Model != tx.Statement.Dest {
		return tx.Statement.Changed(fieldName), nil
	}

	// PUT path: Model == Dest, so the inbound IS the receiver. Under
	// Save, tx.Statement.Changed always reports false because there is
	// no separate loaded row to diff against — we have to load the
	// committed row ourselves and compare manually.
	incoming := tx.Statement.Dest

	id := util.ObjectID(incoming)
	if id == nil {
		return false, fmt.Errorf("IsFieldChanged: %T has no ID; only call from update hooks on persisted rows", incoming)
	}

	pre, err := LoadObjFromDB(tx, incoming, *id)
	if err != nil {
		return false, fmt.Errorf("IsFieldChanged: failed to load pre-update row: %w", err)
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
