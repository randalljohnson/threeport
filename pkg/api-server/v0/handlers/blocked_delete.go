package handlers

import (
	"fmt"

	echo "github.com/labstack/echo/v4"
	gorm "gorm.io/gorm"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	api_v0 "github.com/threeport/threeport/pkg/api/v0"
)

// RespondBlockedDelete writes a 409 with blockers rendered as
// <namespace>/<kebab-kind>/<name>, falling back to id when no name resolves.
func RespondBlockedDelete(c echo.Context, db *gorm.DB, blocked *api_v0.BlockedDeleteError) error {
	baseType := *blocked.AttachedRefs[0].ObjectType
	baseID := *blocked.AttachedRefs[0].ObjectID
	idsByType := map[string]map[uint]struct{}{
		baseType: {baseID: struct{}{}},
	}
	for _, ref := range blocked.AttachedRefs {
		if idsByType[*ref.AttachedObjectType] == nil {
			idsByType[*ref.AttachedObjectType] = map[uint]struct{}{}
		}
		idsByType[*ref.AttachedObjectType][*ref.AttachedObjectID] = struct{}{}
	}

	namesByType := make(map[string]map[uint]string, len(idsByType))
	for objectType, idSet := range idsByType {
		ids := make([]uint, 0, len(idSet))
		for id := range idSet {
			ids = append(ids, id)
		}
		namesByType[objectType] = resolveNamesByType(db, objectType, ids)
	}

	msg := api_v0.FormatBlockedDelete(blocked, namesByType)
	return apiserver_lib.ResponseStatus409(c, nil, fmt.Errorf("%s", msg), baseType)
}

// resolveNamesByType returns an empty map when name resolution fails so
// one failed lookup doesn't drop every blocker from the response.
func resolveNamesByType(db *gorm.DB, objectType string, ids []uint) map[uint]string {
	names, err := GetObjectNames(db, objectType, ids, false)
	if err != nil {
		return map[uint]string{}
	}
	return names
}
