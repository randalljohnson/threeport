package api

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	. "github.com/dave/jennifer/jen"
	"github.com/iancoleman/strcase"

	cli "github.com/threeport/threeport/pkg/cli/v0"
	sdk "github.com/threeport/threeport/pkg/sdk/v0"
	"github.com/threeport/threeport/pkg/sdk/v0/gen"
	"github.com/threeport/threeport/pkg/sdk/v0/util"
)

// GenRelationshipFKMethods emits a per-type
// `RelationshipForeignKeys() []relationshipForeignKey` method that returns
// a pre-computed slice for each API object's relationship-tagged fields.
// The runtime GORM hooks call this method instead of walking struct tags
// via reflection on every write.
//
// The relationshipForeignKey struct is unexported, so this only emits for
// core threeport. Module support requires exporting the type and is left
// as a follow-up.
func GenRelationshipFKMethods(generator *gen.Generator, sdkConfig *sdk.SdkConfig) error {
	if generator.Module {
		return nil
	}

	// flatten StructTags from every ApiObjectGroup so we can look tags up by
	// TypeName rather than by group
	typeToTags := make(map[string]map[string]map[string]string)
	for _, group := range generator.ApiObjectGroups {
		for typeName, tagMap := range group.StructTags {
			typeToTags[typeName] = tagMap
		}
	}

	for _, objCollection := range generator.VersionedApiObjectCollections {
		for _, objGroup := range objCollection.VersionedApiObjectGroups {
			if err := emitRelationshipFKsGen(generator, objCollection.Version, objGroup, typeToTags); err != nil {
				return fmt.Errorf(
					"failed to emit %s_relationship_foreign_keys_gen.go: %w",
					objGroup.Name, err,
				)
			}
		}
	}
	return nil
}

// emitRelationshipFKsGen writes <group>_relationship_fks_gen.go containing
// one method per API object. Skips writing if no object in the group has
// any relationship-tagged fields.
func emitRelationshipFKsGen(
	generator *gen.Generator,
	version string,
	objGroup gen.VersionedApiObjectGroup,
	typeToTags map[string]map[string]map[string]string,
) error {
	f := NewFile(version)
	f.HeaderComment(sdk.HeaderCommentGenNoEdit)

	emittedAny := false
	for _, apiObj := range objGroup.ApiObjects {
		fields := relationshipFields(apiObj.TypeName, typeToTags)
		if len(fields) == 0 {
			continue
		}
		emittedAny = true
		emitRelationshipFKMethod(f, apiObj.TypeName, fields, generator.Module)
	}
	if !emittedAny {
		return nil
	}

	outputPath := filepath.Join(
		"pkg", "api", version,
		fmt.Sprintf("%s_relationship_foreign_keys_gen.go", strcase.ToSnake(objGroup.Name)),
	)
	if _, err := util.WriteCodeToFile(f, outputPath, true); err != nil {
		return err
	}
	cli.Info(fmt.Sprintf("source code for relationship FK methods written to %s", outputPath))
	return nil
}

// relationshipFKField holds the parsed shape of a single relationship-tagged
// field, used during emission.
type relationshipFKField struct {
	fieldName    string
	targetType   string
	relationship string // sdk.RelationshipRequires or sdk.RelationshipOwns
}

// relationshipFields returns the relationship-tagged fields of typeName in
// declaration order (sorted for deterministic emission).
func relationshipFields(
	typeName string,
	typeToTags map[string]map[string]map[string]string,
) []relationshipFKField {
	tagsForType, ok := typeToTags[typeName]
	if !ok {
		return nil
	}
	var fields []relationshipFKField
	for fieldName, tagMap := range tagsForType {
		rel, ok := tagMap[sdk.RelationshipTag]
		if !ok || rel == "" {
			continue
		}
		parts := strings.Split(rel, ";")
		kind := parts[0]
		targetType := strings.TrimSuffix(fieldName, "ID")
		for _, p := range parts[1:] {
			k, v, ok := strings.Cut(p, ":")
			if ok && k == sdk.RelationshipTypeKey {
				targetType = v
			}
		}
		fields = append(fields, relationshipFKField{
			fieldName:    fieldName,
			targetType:   targetType,
			relationship: kind,
		})
	}
	sort.Slice(fields, func(i, j int) bool {
		return fields[i].fieldName < fields[j].fieldName
	})
	return fields
}

// emitRelationshipFKMethod adds the RelationshipForeignKeys() method for
// typeName to f.
func emitRelationshipFKMethod(f *File, typeName string, fields []relationshipFKField, module bool) {
	receiver := strings.ToLower(string(typeName[0]))

	kindLit := func(kind string) *Statement {
		switch kind {
		case sdk.RelationshipOwns:
			return Qual("github.com/threeport/threeport/pkg/sdk/v0", "RelationshipOwns")
		default:
			return Qual("github.com/threeport/threeport/pkg/sdk/v0", "RelationshipRequires")
		}
	}

	f.Comment(fmt.Sprintf(
		"RelationshipForeignKeys returns the relationship-tagged foreign keys on %s.",
		typeName,
	))
	// targetTypeRef emits util.ObjectTypeName(<type>{}) so the type name is
	// compile-checked instead of being a free-floating string literal
	targetTypeRef := func(targetType string) *Statement {
		return Qual(
			"github.com/threeport/threeport/pkg/util/v0",
			"ObjectTypeName",
		).Call(Id(targetType).Values())
	}

	f.Func().Params(
		Id(receiver).Op("*").Id(typeName),
	).Id("RelationshipForeignKeys").Params().Index().Id("relationshipForeignKey").BlockFunc(func(g *Group) {
		g.Return().Index().Id("relationshipForeignKey").ValuesFunc(func(vg *Group) {
			for _, fk := range fields {
				vg.Values(Dict{
					Id("fieldName"):    Lit(fk.fieldName),
					Id("targetType"):   targetTypeRef(fk.targetType),
					Id("relationship"): kindLit(fk.relationship),
					Id("value"):        Id(receiver).Dot(fk.fieldName),
				})
			}
		})
	})
	f.Line()
}
