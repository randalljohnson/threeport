package v0

import (
	"gorm.io/gorm"
)

// GORM update hooks fire under two call shapes, and the hook receiver
// means a different thing in each:
//
//	PATCH  db.Model(&committed).Updates(&incoming)
//	       the receiver is the loaded committed row; the caller's values
//	       are in tx.Statement.Dest.
//	PUT    db.Save(&incoming)
//	       Model == Dest, so the receiver IS the caller's values; there
//	       is no separate committed row in memory.
//
// GORM merges the incoming values onto the receiver between the before-
// and after-update callbacks, so by an after-update hook the receiver
// already reflects the committed result under both shapes.
//
// To stay correct regardless of shape, hooks read through two helpers
// rather than the receiver directly: IncomingValues for the values being
// written, and LoadObjFromDB for a fresh read of the committed row.

// IncomingValues returns the values being written by the current update:
// tx.Statement.Dest when it differs from the receiver (a PATCH, where the
// receiver is the loaded committed row), otherwise the receiver itself (a
// PUT or a create, where the receiver already holds the new values). Use
// in before-hooks that operate on the caller-supplied values regardless
// of call shape.
func IncomingValues(tx *gorm.DB, receiver interface{}) interface{} {
	if dest := tx.Statement.Dest; dest != nil && dest != receiver {
		return dest
	}
	return receiver
}
