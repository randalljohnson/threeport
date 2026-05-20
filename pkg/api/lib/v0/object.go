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

// LoadFreshFromDB returns a newly-allocated instance of obj's concrete
// type populated from the database by ID. The original obj is not
// mutated. Use this from GORM hooks that need to read post-write field
// values while keeping the original as a pre-write snapshot.
func LoadFreshFromDB(tx *gorm.DB, obj interface{}, id uint) (interface{}, error) {
	// allocate a new instance of obj's concrete type via reflection;
	// the caller's obj stays untouched while loaded values land here
	updatedObj := reflect.New(reflect.TypeOf(obj).Elem()).Interface()

	// open a new session; the inbound tx may carry filter clauses from
	// the surrounding hook's statement that would taint an unrelated read
	cleanSession := tx.Session(&gorm.Session{NewDB: true})

	// read the row by primary key into updatedObj
	if err := cleanSession.First(updatedObj, id).Error; err != nil {
		return nil, fmt.Errorf(
			"failed to reload %s/%d from database: %w",
			util.ObjectTypeName(obj), id, err,
		)
	}
	return updatedObj, nil
}
