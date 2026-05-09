package api

import (
	"fmt"
	"sort"
	"strings"

	. "github.com/dave/jennifer/jen"
	"github.com/iancoleman/strcase"

	sdk "github.com/threeport/threeport/pkg/sdk/v0"
)

// encryptedFieldEntry holds the parsed shape of a single encrypt-tagged
// field, used during emission.
type encryptedFieldEntry struct {
	fieldName  string
	columnName string
}

// encryptedFieldEntries returns the encrypt-tagged fields of typeName in
// deterministic (sorted) order for stable emission.
func encryptedFieldEntries(
	typeName string,
	typeToTags map[string]map[string]map[string]string,
) []encryptedFieldEntry {
	tagsForType, ok := typeToTags[typeName]
	if !ok {
		return nil
	}
	var entries []encryptedFieldEntry
	for fieldName, tagMap := range tagsForType {
		if tagMap[sdk.EncryptTag] != sdk.EncryptTrue {
			continue
		}
		entries = append(entries, encryptedFieldEntry{
			fieldName:  fieldName,
			columnName: strcase.ToSnake(fieldName),
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].fieldName < entries[j].fieldName
	})
	return entries
}

// emitEncryptedFieldsMethod adds the EncryptedFields() method for typeName
// to f.
func emitEncryptedFieldsMethod(f *File, typeName string, fields []encryptedFieldEntry) {
	receiver := strings.ToLower(string(typeName[0]))

	f.Comment(fmt.Sprintf(
		"EncryptedFields returns the encrypt-tagged fields on %s.",
		typeName,
	))
	f.Func().Params(
		Id(receiver).Op("*").Id(typeName),
	).Id("EncryptedFields").Params().Index().Id("encryptedField").BlockFunc(func(g *Group) {
		g.Return().Index().Id("encryptedField").ValuesFunc(func(vg *Group) {
			for _, entry := range fields {
				vg.Values(Dict{
					Id("fieldName"):  Lit(entry.fieldName),
					Id("columnName"): Lit(entry.columnName),
					Id("value"):      Id(receiver).Dot(entry.fieldName),
				})
			}
		})
	})
	f.Line()
}
