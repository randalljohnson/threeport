package v0

import (
	"bytes"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/iancoleman/strcase"
	"gorm.io/gorm"

	api_lib "github.com/threeport/threeport/pkg/api/lib/v0"
	auth "github.com/threeport/threeport/pkg/auth/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// BlockedDeleteError reports a delete rejected by one or more attached
// object references. Error() renders the message with id-only paths; the
// API server upgrades to name-resolved paths when it can. AttachedRefs
// is always non-empty.
type BlockedDeleteError struct {
	AttachedRefs []AttachedObjectReference
}

func (e *BlockedDeleteError) Error() string {
	return FormatBlockedDelete(e, nil)
}

// FormatBlockedDelete renders the 409 message body. namesByType holds
// id->name for each object type that the caller resolved; pass nil for
// id-only output.
func FormatBlockedDelete(e *BlockedDeleteError, namesByType map[string]map[uint]string) string {
	baseType := *e.AttachedRefs[0].ObjectType
	baseID := *e.AttachedRefs[0].ObjectID
	baseLabel := FormatObjectPath(baseType, baseID, namesByType[baseType])

	var buf bytes.Buffer
	writer := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	for _, ref := range e.AttachedRefs {
		fmt.Fprintf(writer, "  - %s\n", FormatObjectPath(
			*ref.AttachedObjectType, *ref.AttachedObjectID, namesByType[*ref.AttachedObjectType],
		))
	}
	writer.Flush()

	return fmt.Sprintf(
		"%s cannot be deleted while %d object(s) still reference it:\n\n%sRemove dependents first.",
		baseLabel, len(e.AttachedRefs), buf.String(),
	)
}

// FormatObjectPath renders <api-namespace>/<kebab-kind>/<name> for module
// types and <kebab-kind>/<name> for core, falling back to id when no name
// is available.
func FormatObjectPath(rawType string, id uint, names map[uint]string) string {
	namespace := ""
	versionedName := rawType
	if slashIdx := strings.Index(rawType, "/"); slashIdx >= 0 {
		namespace = rawType[:slashIdx]
		versionedName = rawType[slashIdx+1:]
	}
	typeName := versionedName
	if dotIdx := strings.LastIndex(versionedName, "."); dotIdx >= 0 {
		typeName = versionedName[dotIdx+1:]
	}
	kind := strcase.ToKebab(typeName)

	tail := kind
	if namespace != "" {
		tail = fmt.Sprintf("%s/%s", namespace, kind)
	}

	if name, ok := names[id]; ok && name != "" {
		return fmt.Sprintf("%s/%s", tail, name)
	}
	return fmt.Sprintf("%s/%d", tail, id)
}

// CheckBlockingAttachedObjectReferences returns a BlockedDeleteError if obj
// has any incoming references that block its deletion. Handlers call this
// synchronously before scheduling a delete so the caller sees the 409
// immediately instead of the controller looping on the BeforeDelete hook.
func CheckBlockingAttachedObjectReferences(tx *gorm.DB, obj interface{}) error {
	objType := util.ObjectTypeName(obj)
	objID := util.ObjectID(obj)
	if objID == nil {
		return nil
	}

	callerOU := api_lib.Caller(tx.Statement.Context).OrganizationalUnit
	blockingRefs, err := findBlockingAttachedObjectReferences(tx, objType, objID, callerOU)
	if err != nil {
		return err
	}
	if len(blockingRefs) > 0 {
		return &BlockedDeleteError{AttachedRefs: blockingRefs}
	}
	return nil
}

// findBlockingAttachedObjectReferences returns the attached object
// references that block deletion of the given object. "requires" always
// blocks; "owns" and "marries" block unless the caller is a control-plane
// component.
func findBlockingAttachedObjectReferences(
	db *gorm.DB,
	objectType string,
	objectID *uint,
	callerOU string,
) ([]AttachedObjectReference, error) {
	var attachedObjectReferences []AttachedObjectReference
	if err := db.
		Where(
			"object_type = ? AND object_id = ? AND relationship IN ?",
			objectType, objectID, []Relationship{RelationshipRequires, RelationshipOwns, RelationshipMarries},
		).
		Find(&attachedObjectReferences).Error; err != nil {
		return nil, fmt.Errorf("failed to list blocking attached object references: %w", err)
	}

	if callerOU != auth.OUControlPlane {
		return attachedObjectReferences, nil
	}

	var blockingRefs []AttachedObjectReference
	for _, ref := range attachedObjectReferences {
		if ref.Relationship != nil && *ref.Relationship == RelationshipRequires {
			blockingRefs = append(blockingRefs, ref)
		}
	}
	return blockingRefs, nil
}
