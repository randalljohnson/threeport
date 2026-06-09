package installer

import (
	"fmt"
	"path/filepath"
	"sort"

	. "github.com/dave/jennifer/jen"

	cli "github.com/threeport/threeport/pkg/cli/v0"
	sdk "github.com/threeport/threeport/pkg/sdk/v0"
	"github.com/threeport/threeport/pkg/sdk/v0/gen"
	"github.com/threeport/threeport/pkg/sdk/v0/util"
)

// GenApiObjectGroupNames emits a deterministic, sorted list of API object
// group names declared in the SDK config so downstream installer code can
// iterate over the group set without re-parsing the config.
func GenApiObjectGroupNames(generator *gen.Generator, sdkConfig *sdk.SdkConfig) error {
	// collect group names from the SDK config
	var names []string
	for _, group := range sdkConfig.ApiObjectGroups {
		if group == nil || group.Name == nil {
			continue
		}
		names = append(names, *group.Name)
	}
	sort.Strings(names)

	f := NewFile("v0")
	f.HeaderComment(sdk.HeaderCommentGenNoEdit)

	f.Var().Id("ApiObjectGroupNames").Op("=").Index().String().ValuesFunc(func(v *Group) {
		for _, name := range names {
			v.Line().Lit(name)
		}
		v.Line()
	})

	// modules write to the module installer package; threeport-core writes
	// to the threeport-installer package
	var genFilepath string
	if generator.Module {
		genFilepath = filepath.Join(
			"pkg",
			"installer",
			"v0",
			"api_object_groups_gen.go",
		)
	} else {
		genFilepath = filepath.Join(
			"pkg",
			"threeport-installer",
			"v0",
			"api_object_groups_gen.go",
		)
	}

	if _, err := util.WriteCodeToFile(f, genFilepath, true); err != nil {
		return fmt.Errorf("failed to write generated code to file %s: %w", genFilepath, err)
	}
	cli.Info(fmt.Sprintf("source code for API object group names written to %s", genFilepath))

	return nil
}
