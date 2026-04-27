package handlers

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"

	strcase "github.com/iancoleman/strcase"
	gorm "gorm.io/gorm"

	api_v0 "github.com/threeport/threeport/pkg/api/v0"
)

// FormatBlockingAttachedObjectReferencesError returns an error listing each
// blocker as <namespace>/<kebab-kind>/<name> for module types and
// <kebab-kind>/<name> for core, falling back to the id when name resolution
// returns nothing.
func FormatBlockingAttachedObjectReferencesError(
	db *gorm.DB,
	refs []api_v0.AttachedObjectReference,
) error {
	if len(refs) == 0 {
		return errors.New("delete blocked but no blocking attached object references provided")
	}

	// resolve the parent's name for the header line
	parentLabel := "object"
	if refs[0].ObjectType != nil && refs[0].ObjectID != nil {
		parentNames, _ := LookupObjectNames(db, *refs[0].ObjectType, []uint{*refs[0].ObjectID}, false)
		parentLabel = formatBlockerPath(*refs[0].ObjectType, *refs[0].ObjectID, parentNames)
	}

	// group blocker ids by type so each type can be looked up in a single call
	idsByType := map[string]map[uint]struct{}{}
	for _, r := range refs {
		if r.AttachedObjectType == nil || r.AttachedObjectID == nil {
			continue
		}
		if idsByType[*r.AttachedObjectType] == nil {
			idsByType[*r.AttachedObjectType] = map[uint]struct{}{}
		}
		idsByType[*r.AttachedObjectType][*r.AttachedObjectID] = struct{}{}
	}

	// dispatch each type to core sql or module http and collect resolved names
	namesByType := make(map[string]map[uint]string, len(idsByType))
	for typ, idSet := range idsByType {
		ids := make([]uint, 0, len(idSet))
		for id := range idSet {
			ids = append(ids, id)
		}
		names, err := LookupObjectNames(db, typ, ids, false)
		if err != nil {
			continue
		}
		namesByType[typ] = names
	}

	// render one bullet per blocker
	var buf bytes.Buffer
	writer := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	for _, r := range refs {
		if r.AttachedObjectType == nil || r.AttachedObjectID == nil {
			continue
		}
		fmt.Fprintf(writer, "  - %s\n", formatBlockerPath(
			*r.AttachedObjectType, *r.AttachedObjectID, namesByType[*r.AttachedObjectType],
		))
	}
	writer.Flush()

	return fmt.Errorf(
		"%s cannot be deleted while %d object(s) still reference it:\n\n%s\nRemove dependents first.",
		parentLabel, len(refs), buf.String(),
	)
}

// formatBlockerPath renders <namespace>/<kebab-kind>/<name> for module types
// and <kebab-kind>/<name> for core. Falls back to id when no name resolved.
func formatBlockerPath(rawType string, id uint, names map[uint]string) string {
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
