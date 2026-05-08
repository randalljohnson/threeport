package api

import (
	"fmt"
	"sort"
	"strings"

	. "github.com/dave/jennifer/jen"

	sdk "github.com/threeport/threeport/pkg/sdk/v0"
	"github.com/threeport/threeport/pkg/sdk/v0/gen"
)

// relationshipForeignKeyField holds the parsed shape of a single
// relationship-tagged field, used during emission.
type relationshipForeignKeyField struct {
	fieldName    string
	objectType   string
	relationship string // sdk.RelationshipRequires or sdk.RelationshipOwns
}

// relationshipForeignKeyFields returns the relationship-tagged fields of
// typeName in deterministic (sorted) order for stable emission.
func relationshipForeignKeyFields(
	typeName string,
	typeToTags map[string]map[string]map[string]string,
) []relationshipForeignKeyField {
	tagsForType, ok := typeToTags[typeName]
	if !ok {
		return nil
	}
	var fields []relationshipForeignKeyField
	for fieldName, tagMap := range tagsForType {
		rel, ok := tagMap[sdk.RelationshipTag]
		if !ok || rel == "" {
			continue
		}
		kind, modifiers, _ := gen.ParseRelationshipTagValue(rel)
		objectType := strings.TrimSuffix(fieldName, "ID")
		if v, ok := modifiers[sdk.RelationshipTypeKey]; ok {
			objectType = v
		}
		fields = append(fields, relationshipForeignKeyField{
			fieldName:    fieldName,
			objectType:   objectType,
			relationship: kind,
		})
	}
	sort.Slice(fields, func(i, j int) bool {
		return fields[i].fieldName < fields[j].fieldName
	})
	return fields
}

// emitRelationshipForeignKeyMethod adds the RelationshipForeignKeys()
// method for typeName to f.
func emitRelationshipForeignKeyMethod(f *File, typeName string, fields []relationshipForeignKeyField) {
	receiver := strings.ToLower(string(typeName[0]))

	kindLit := func(kind string) *Statement {
		switch kind {
		case sdk.RelationshipOwns:
			return Qual("github.com/threeport/threeport/pkg/sdk/v0", "RelationshipOwns")
		default:
			return Qual("github.com/threeport/threeport/pkg/sdk/v0", "RelationshipRequires")
		}
	}

	// objectTypeRef emits util.ObjectTypeName(<type>{}) so the type name is
	// compile-checked instead of a free-floating string literal
	objectTypeRef := func(objectType string) *Statement {
		return Qual(
			"github.com/threeport/threeport/pkg/util/v0",
			"ObjectTypeName",
		).Call(Id(objectType).Values())
	}

	f.Comment(fmt.Sprintf(
		"RelationshipForeignKeys returns the relationship-tagged foreign keys on %s.",
		typeName,
	))
	f.Func().Params(
		Id(receiver).Op("*").Id(typeName),
	).Id("RelationshipForeignKeys").Params().Index().Id("relationshipForeignKey").BlockFunc(func(g *Group) {
		g.Return().Index().Id("relationshipForeignKey").ValuesFunc(func(vg *Group) {
			for _, foreignKey := range fields {
				vg.Values(Dict{
					Id("fieldName"):    Lit(foreignKey.fieldName),
					Id("objectType"):   objectTypeRef(foreignKey.objectType),
					Id("relationship"): kindLit(foreignKey.relationship),
					Id("objectID"):     Id(receiver).Dot(foreignKey.fieldName),
				})
			}
		})
	})
	f.Line()
}
