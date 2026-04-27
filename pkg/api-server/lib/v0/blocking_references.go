package v0

import (
	"fmt"

	api_v0 "github.com/threeport/threeport/pkg/api/v0"
	"gorm.io/gorm"
)

// FindBlockingAttachedObjectReferences returns attached object references that
// point at the given object and are marked blocking.
func FindBlockingAttachedObjectReferences(
	db *gorm.DB,
	objectType string,
	objectID *uint,
) ([]api_v0.AttachedObjectReference, error) {
	var attachedObjectReferences []api_v0.AttachedObjectReference
	if err := db.
		Where("object_type = ? AND object_id = ? AND blocking = true", objectType, objectID).
		Find(&attachedObjectReferences).Error; err != nil {
		return nil, fmt.Errorf("failed to list blocking attached object references: %w", err)
	}
	return attachedObjectReferences, nil
}
