package api

import (
	"fmt"
	"path/filepath"
	"slices"

	. "github.com/dave/jennifer/jen"

	cli "github.com/threeport/threeport/pkg/cli/v0"
	sdk "github.com/threeport/threeport/pkg/sdk/v0"
	"github.com/threeport/threeport/pkg/sdk/v0/gen"
	"github.com/threeport/threeport/pkg/sdk/v0/util"
)

// GenTypesByName emits an init() that registers each API type's factory
// function with the runtime types-by-name registry, so AOR hooks can
// construct instances from reflect.Type.String()-style names.
func GenTypesByName(gen *gen.Generator, sdkConfig *sdk.SdkConfig) error {
	for _, version := range gen.GlobalVersionConfig.Versions {
		f := NewFilePath(fmt.Sprintf("%s/pkg/api/%s", gen.ModulePath, version.VersionName))
		f.HeaderComment(sdk.HeaderCommentGenNoEdit)

		f.ImportAlias("github.com/threeport/threeport/pkg/api/v0", "api")

		f.Func().Id("init").Params().BlockFunc(func(g *Group) {
			for _, name := range version.DatabaseInitNames {
				typeKey := fmt.Sprintf("%s.%s", version.VersionName, name)
				g.Qual("github.com/threeport/threeport/pkg/api/v0", "RegisterTypeFactory").Call(
					Lit(typeKey),
					Func().Params().Interface().Block(
						Return(Op("&").Id(name).Values()),
					),
				)
			}
		})

		genFilepath := filepath.Join(
			"pkg",
			"api",
			version.VersionName,
			"types_by_name_gen.go",
		)
		if slices.Contains(sdkConfig.ExcludeFiles, genFilepath) {
			cli.Info(fmt.Sprintf("source code generation skipped for %s", genFilepath))
		} else {
			_, err := util.WriteCodeToFile(f, genFilepath, true)
			if err != nil {
				return fmt.Errorf("failed to write generated code to file %s: %w", genFilepath, err)
			}
			cli.Info(fmt.Sprintf("source code for types-by-name registry written to %s", genFilepath))
		}
	}
	return nil
}
