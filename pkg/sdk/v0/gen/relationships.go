package gen

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

// ValidateRelationshipCycles returns an error when the foreign-key graph in
// RelationshipDependencies contains a cycle. gorm AutoMigrate creates each
// table and its foreign-key constraints as it walks the migration list, so a
// cycle has no table order in which every constraint points at a table that
// already exists. Without this check the migration sort falls back to
// alphabetical order and the failure only surfaces when the control plane
// migrates its database, far from the model that caused it.
//
// A type whose foreign key points at itself is not a cycle: gorm creates the
// table before adding the constraint, so a self-reference migrates cleanly.
func (g *Generator) ValidateRelationshipCycles() error {
	// walk root types and edges in sorted order so a graph carrying more than
	// one cycle always reports the same one
	typeNames := make([]string, 0, len(g.RelationshipDependencies))
	for typeName := range g.RelationshipDependencies {
		typeNames = append(typeNames, typeName)
	}
	sort.Strings(typeNames)

	// visiting holds the types on the current depth-first path, so an edge back
	// into it closes a cycle. visited holds types already cleared, so a type
	// several paths reach is explored once.
	visiting := make(map[string]bool, len(typeNames))
	visited := make(map[string]bool, len(typeNames))
	var path []string

	var walk func(typeName string) []string
	walk = func(typeName string) []string {
		if visiting[typeName] {
			// trim the path back to where this type first appeared so the
			// report carries the cycle alone, not the walk that reached it
			start := 0
			for i, name := range path {
				if name == typeName {
					start = i
					break
				}
			}
			return append(append([]string{}, path[start:]...), typeName)
		}
		if visited[typeName] {
			return nil
		}

		visiting[typeName] = true
		path = append(path, typeName)

		referenced := append([]string{}, g.RelationshipDependencies[typeName]...)
		sort.Strings(referenced)
		for _, next := range referenced {
			if next == typeName {
				continue
			}
			if cycle := walk(next); cycle != nil {
				return cycle
			}
		}

		path = path[:len(path)-1]
		visiting[typeName] = false
		visited[typeName] = true
		return nil
	}

	for _, typeName := range typeNames {
		if cycle := walk(typeName); cycle != nil {
			return fmt.Errorf(
				"circular foreign-key dependency between API types: %s",
				strings.Join(cycle, " -> "),
			)
		}
	}

	return nil
}

// SortDatabaseInitNamesByDependency returns names reordered so that every
// referenced type precedes the types whose foreign-key columns point at it.
// gorm AutoMigrate creates each model's foreign-key constraints
// as it walks the slice, so a referenced table must appear before any model
// that references it or the migration fails with a missing-relation error.
// Ties are broken alphabetically so the generated order is stable across runs.
// Names not present in the input are ignored.
func (g *Generator) SortDatabaseInitNamesByDependency(names []string) []string {
	// collapse repeats before anything else. The emit loop below runs until it
	// has emitted as many names as it was given, and it emits each name once, so
	// a repeated entry leaves the loop one short, drops into the cycle fallback
	// with nothing remaining, and returns a list missing a table. The caller
	// writes this result straight into the generated AutoMigrate call, so that
	// table would simply never be created.
	seen := make(map[string]bool, len(names))
	unique := make([]string, 0, len(names))
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true
		unique = append(unique, name)
	}
	names = unique

	// build the set of names being migrated so cross-module references
	// (types migrated elsewhere) do not introduce edges into this list
	inList := make(map[string]bool, len(names))
	for _, name := range names {
		inList[name] = true
	}

	// dependsOn[name] holds the in-list types that name's foreign keys
	// reference; each such type must be migrated before name
	dependsOn := make(map[string]map[string]bool, len(names))
	for _, name := range names {
		dependsOn[name] = make(map[string]bool)
		for _, referenced := range g.RelationshipDependencies[name] {
			if inList[referenced] && referenced != name {
				dependsOn[name][referenced] = true
			}
		}
	}

	// Kahn's algorithm: repeatedly emit the alphabetically-first name whose
	// dependencies have all been emitted, so referenced tables land ahead of
	// the tables that reference them
	emitted := make(map[string]bool, len(names))
	sorted := make([]string, 0, len(names))
	for len(sorted) < len(names) {
		var ready []string
		for _, name := range names {
			if emitted[name] {
				continue
			}
			allDepsEmitted := true
			for dep := range dependsOn[name] {
				if !emitted[dep] {
					allDepsEmitted = false
					break
				}
			}
			if allDepsEmitted {
				ready = append(ready, name)
			}
		}
		if len(ready) == 0 {
			// unreachable during generation: ValidateRelationshipCycles rejects
			// a cyclic graph before any code is emitted. Kept as defence so a
			// caller reaching here still gets every name back in a deterministic
			// order rather than a list silently missing tables.
			var remaining []string
			for _, name := range names {
				if !emitted[name] {
					remaining = append(remaining, name)
				}
			}
			sort.Strings(remaining)
			sorted = append(sorted, remaining...)
			break
		}
		sort.Strings(ready)
		next := ready[0]
		sorted = append(sorted, next)
		emitted[next] = true
	}

	return sorted
}

// parseRelationshipDependencies scans every model source file in dir and
// returns each struct's foreign-key dependencies keyed by struct name, where a
// dependency means the struct's table carries a foreign-key column referencing
// the named type and so must be migrated after it. The edges mirror where gorm
// places foreign-key columns rather than the relationship tags: a has-many
// slice field puts the key on the child, and a belongs-to association field
// puts the key on the owning struct. A bare key field with no association field
// is a plain column that gorm does not constrain, so it contributes no edge,
// and neither does a many2many field, whose keys live in a join table.
// Generated, validation, and test files are skipped so only hand-authored model
// definitions contribute.
func parseRelationshipDependencies(dir string) (map[string][]string, error) {
	structs, err := parseModelStructs(dir)
	if err != nil {
		return nil, err
	}

	// the set of model type names in this directory bounds association
	// detection: only fields whose element or singular type names another
	// local model produce a gorm foreign key inside this migration list
	modelNames := make(map[string]bool, len(structs))
	for name := range structs {
		modelNames[name] = true
	}

	dependencies := map[string][]string{}
	for typeName, structType := range structs {
		// collect the struct's key field names so a singular association can
		// be confirmed to carry a matching XID column before it counts as a
		// belongs-to that places the foreign key on this struct
		keyFields := make(map[string]bool)
		for _, field := range structType.Fields.List {
			for _, name := range field.Names {
				if strings.HasSuffix(name.Name, "ID") {
					keyFields[name.Name] = true
				}
			}
		}

		for _, field := range structType.Fields.List {
			// embedded fields carry no name and never declare an association
			if len(field.Names) == 0 {
				continue
			}

			// a many2many association keys a join table, so neither side's own
			// table gains a foreign key and the pair orders nothing. Counting
			// it would read as each side depending on the other, which is a
			// cycle no ordering can satisfy.
			if joinTableAssociation(field) {
				continue
			}

			// a has-many slice puts the foreign key on the child, so the child
			// depends on this parent
			if child, ok := sliceElementModel(field.Type, modelNames); ok {
				dependencies[child] = append(dependencies[child], typeName)
				continue
			}

			// a belongs-to singular association puts the foreign key on this
			// struct, so this struct depends on the referenced type; require a
			// matching XID key field so a plain unconstrained column does not
			// register as an association
			if referenced, ok := singularModel(field.Type, modelNames); ok {
				if keyFields[referenced+"ID"] {
					dependencies[typeName] = append(dependencies[typeName], referenced)
				}
			}
		}
	}

	return dependencies, nil
}

// parseModelStructs returns every hand-authored struct type declared in dir
// keyed by type name. Generated, validation, and test files are skipped so the
// set matches the model definitions that drive migration ordering.
func parseModelStructs(dir string) (map[string]*ast.StructType, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		// a missing version directory is not fatal; nothing to scan
		if os.IsNotExist(err) {
			return map[string]*ast.StructType{}, nil
		}
		return nil, fmt.Errorf("failed to read model directory %s: %w", dir, err)
	}

	structs := map[string]*ast.StructType{}
	for _, entry := range entries {
		fileName := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(fileName, ".go") {
			continue
		}
		// skip non-model files: generated code, validators, and tests carry
		// no hand-authored model definitions
		if strings.HasSuffix(fileName, "_gen.go") ||
			strings.HasSuffix(fileName, "_test.go") ||
			strings.HasSuffix(fileName, "_validate.go") {
			continue
		}

		filePath := filepath.Join(dir, fileName)
		fset := token.NewFileSet()
		parsedFile, err := parser.ParseFile(fset, filePath, nil, parser.AllErrors)
		if err != nil {
			return nil, fmt.Errorf("failed to parse model file %s: %w", filePath, err)
		}

		for _, decl := range parsedFile.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range genDecl.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				structType, ok := typeSpec.Type.(*ast.StructType)
				if !ok {
					continue
				}
				structs[typeSpec.Name.Name] = structType
			}
		}
	}

	return structs, nil
}

// joinTableAssociation reports whether a field declares a gorm many2many
// association. gorm keeps that relationship in a join table holding both
// foreign keys, so the two model tables can be created in either order.
func joinTableAssociation(field *ast.Field) bool {
	if field.Tag == nil {
		return false
	}
	gormTag := reflect.StructTag(strings.Trim(field.Tag.Value, "`")).Get("gorm")
	// a gorm tag is a list of semicolon-separated key:value settings; match on
	// the key alone so a table name mentioning the word does not count
	for _, setting := range strings.Split(gormTag, ";") {
		key, _, _ := strings.Cut(setting, ":")
		if strings.EqualFold(strings.TrimSpace(key), "many2many") {
			return true
		}
	}
	return false
}

// sliceElementModel reports the local model type named by a has-many slice
// field. It matches []*Model and []Model element types and ignores slices of
// non-model or cross-package element types, which place no foreign key inside
// this migration list.
func sliceElementModel(expr ast.Expr, modelNames map[string]bool) (string, bool) {
	arrayType, ok := expr.(*ast.ArrayType)
	if !ok || arrayType.Len != nil {
		return "", false
	}
	if name, ok := identModel(arrayType.Elt, modelNames); ok {
		return name, true
	}
	return "", false
}

// singularModel reports the local model type named by a singular association
// field. It matches *Model and Model field types and ignores embedded or
// cross-package types, which carry a package qualifier rather than a bare
// identifier.
func singularModel(expr ast.Expr, modelNames map[string]bool) (string, bool) {
	return identModel(expr, modelNames)
}

// identModel reports the model type named by a bare identifier or pointer to a
// bare identifier when that name belongs to the local model set.
func identModel(expr ast.Expr, modelNames map[string]bool) (string, bool) {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return "", false
	}
	if modelNames[ident.Name] {
		return ident.Name, true
	}
	return "", false
}
