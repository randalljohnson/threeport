package v0

import (
	"fmt"
	"reflect"
	"time"

	"gorm.io/gorm"

	notifications "github.com/threeport/threeport/pkg/notifications/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// ReconciledThreeportApiObject is the interface each reconciled object in the
// Threeport API must satisfy for compatibility with the controlllers.
type ReconciledThreeportApiObject interface {
	NotificationPayload(
		operation notifications.NotificationOperation,
		requeue bool,
		creationTime int64,
	) (*[]byte, error)
	DecodeNotifObject(object interface{}) error
	GetId() uint
	GetType() string
	GetVersion() string
	ScheduledForDeletion() *time.Time
}

// NewCleanSession returns a session on the existing transaction that
// does not inherit clauses (WHERE filters, model targets, etc.) from
// the parent statement. Use this from GORM hooks when issuing a query
// that is unrelated to the operation that triggered the hook - without
// it the surrounding statement's clauses would silently apply to the
// new query.
func NewCleanSession(tx *gorm.DB) *gorm.DB {
	return tx.Session(&gorm.Session{NewDB: true})
}

// LoadFreshFromDB returns a newly-allocated instance of obj's concrete
// type populated from the database by ID. The original obj is not
// mutated. Use this from GORM hooks that need to read post-write field
// values while keeping the original as a pre-write snapshot.
func LoadFreshFromDB(tx *gorm.DB, obj interface{}, id uint) (interface{}, error) {
	// allocate a new instance of obj's concrete type via reflection;
	// the caller's obj stays untouched while loaded values land here
	updatedObj := reflect.New(reflect.TypeOf(obj).Elem()).Interface()

	// read the row by primary key into updatedObj
	if err := NewCleanSession(tx).First(updatedObj, id).Error; err != nil {
		return nil, fmt.Errorf(
			"failed to reload %s/%d from database: %w",
			util.ObjectTypeName(obj), id, err,
		)
	}
	return updatedObj, nil
}
