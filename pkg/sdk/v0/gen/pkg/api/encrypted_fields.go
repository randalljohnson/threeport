package api

import (
	"fmt"
	"sort"
	"strings"

	. "github.com/dave/jennifer/jen"

	sdk "github.com/threeport/threeport/pkg/sdk/v0"
)

// encryptedFieldNames returns the encrypt-tagged field names of typeName
// in deterministic (sorted) order for stable emission.
func encryptedFieldNames(
	typeName string,
	typeToTags map[string]map[string]map[string]string,
) []string {
	tagsForType, ok := typeToTags[typeName]
	if !ok {
		return nil
	}
	var names []string
	for fieldName, tagMap := range tagsForType {
		if tagMap[sdk.EncryptTag] != sdk.EncryptTrue {
			continue
		}
		names = append(names, fieldName)
	}
	sort.Strings(names)
	return names
}

// emitEncryptedFieldsMethod adds the EncryptedFields() method for typeName
// to f.
func emitEncryptedFieldsMethod(f *File, typeName string, fieldNames []string) {
	receiver := strings.ToLower(string(typeName[0]))

	encryptedFieldType := Qual("github.com/threeport/threeport/pkg/sdk/v0", "EncryptedField")

	f.Comment(fmt.Sprintf(
		"EncryptedFields returns the encrypt-tagged fields on %s.",
		typeName,
	))
	f.Func().Params(
		Id(receiver).Op("*").Id(typeName),
	).Id("EncryptedFields").Params().Index().Add(encryptedFieldType).BlockFunc(func(g *Group) {
		g.Return().Index().Add(encryptedFieldType).ValuesFunc(func(vg *Group) {
			for _, fieldName := range fieldNames {
				vg.Values(Dict{
					Id("Name"):  Lit(fieldName),
					Id("Value"): Id(receiver).Dot(fieldName),
				})
			}
		})
	})
	f.Line()
}
