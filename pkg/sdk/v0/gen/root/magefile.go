package root

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	. "github.com/dave/jennifer/jen"
	"github.com/iancoleman/strcase"

	cli "github.com/threeport/threeport/pkg/cli/v0"
	sdk "github.com/threeport/threeport/pkg/sdk/v0"
	"github.com/threeport/threeport/pkg/sdk/v0/gen"
	"github.com/threeport/threeport/pkg/sdk/v0/util"
)

// componentSpec carries the bits the magefile generator needs to emit a
// per-component build target: the binary name on disk (used both for the
// `bin/<arch>/<name>` output path and the `BINARY=<name>` build-arg), the
// package dir the Go compiler builds, the container image name, the
// name of the generated package-only function that the AllImages* tasks
// call to skip redundant compile work, and the sdk-config api name.
// ApiName is empty for the always-included components (rest-api,
// database-migrator). Controllers (and the agent) are listed in the
// allComponents slice with their ApiName set to the sdk-config api name (empty for always-included).
type componentSpec struct {
	BinaryName      string
	PackageDir      string
	ImageName       string
	PackageFuncName string
	ApiName         string
}

// GenMagefile generates the source code for mage which is a Make-like tool
// using Go.
// Ref: https://github.com/magefile/mage
func GenMagefile(gen *gen.Generator, sdkConfig *sdk.SdkConfig) error {
	f := NewFile("main")
	f.HeaderComment(sdk.HeaderCommentGenNoEdit)

	// set installer package for threeport and modules
	var installerPkg string
	if gen.Module {
		installerPkg = fmt.Sprintf("%s/pkg/installer/v0", gen.ModulePath)
	} else {
		installerPkg = fmt.Sprintf("%s/pkg/threeport-installer/v0", gen.ModulePath)
	}

	// set release image namespace constant
	var releaseImageRepoConst string
	if gen.Module {
		releaseImageRepoConst = "ReleaseImageNamespace"
	} else {
		releaseImageRepoConst = "ThreeportImageNamespace"
	}

	f.ImportAlias("github.com/threeport/threeport/pkg/util/v0", "util")
	f.ImportAlias(installerPkg, "installer")

	// collect specs for every per-component image function so AllImages
	// can pre-build the binaries up front in one go build per arch.
	var allComponents []componentSpec

	// set function names for each component
	buildApiFuncName := "ApiBin"
	buildDbMigratorFuncName := "DbMigratorBin"
	buildAgentFuncName := "AgentBin"
	buildFuncNames := []string{buildApiFuncName, buildDbMigratorFuncName}

	buildApiDevFuncName := "ApiBinDev"
	buildDbMigratorDevFuncName := "DbMigratorBinDev"
	buildAgentDevFuncName := "AgentBinDev"
	buildDevFuncNames := []string{buildApiDevFuncName, buildDbMigratorDevFuncName}

	buildApiReleaseFuncName := "ApiBinRelease"
	buildDbMigratorReleaseFuncName := "DbMigratorBinRelease"
	buildAgentReleaseFuncName := "AgentBinRelease"
	buildReleaseFuncNames := []string{buildApiReleaseFuncName, buildDbMigratorReleaseFuncName}

	buildApiImageFuncName := "ApiImage"
	buildDbMigratorImageFuncName := "DbMigratorImage"
	buildAgentImageFuncName := "AgentImage"

	buildApiDevImageFuncName := "ApiImageDev"
	buildDbMigratorDevImageFuncName := "DbMigratorImageDev"
	buildAgentDevImageFuncName := "AgentImageDev"

	buildApiReleaseImageFuncName := "ApiImageRelease"
	buildDbMigratorReleaseImageFuncName := "DbMigratorImageRelease"
	buildAgentReleaseImageFuncName := "AgentImageRelease"

	f.Const().Id("releaseArch").Op("=").Lit("amd64")
	f.Line()

	namespaces := []string{"Build", "Test", "Install", "Dev"}
	for _, ns := range namespaces {
		f.Comment(fmt.Sprintf(
			"%s provides a type for methods that implement %s targets.", ns, strcase.ToLowerCamel(ns),
		))
		f.Type().Id(ns).Qual("github.com/magefile/mage/mg", "Namespace")
		f.Line()
	}

	// binary build function for API
	emitBinFunc(f, buildApiFuncName, "REST API", "rest-api", "cmd/rest-api")
	emitBinDevFunc(f, buildApiDevFuncName, buildApiFuncName, "REST API", "rest-api")
	emitBinReleaseFunc(f, buildApiReleaseFuncName, buildApiFuncName, "REST API", "rest-api")

	apiImageName := "threeport-rest-api"
	if gen.Module {
		apiImageName = fmt.Sprintf(
			"threeport-%s-rest-api",
			strcase.ToKebab(sdkConfig.ModuleName),
		)
	}
	apiPackageFuncName := "restApiImagePackage"
	allComponents = append(allComponents, componentSpec{
		BinaryName:      "rest-api",
		PackageDir:      "cmd/rest-api",
		ImageName:       apiImageName,
		PackageFuncName: apiPackageFuncName,
	})
	emitImagePackageFunc(f, apiPackageFuncName, "REST API", "rest-api", apiImageName)
	emitImageFunc(f, buildApiImageFuncName, "REST API", "rest-api", "cmd/rest-api", apiPackageFuncName)
	emitImageDevFunc(f, buildApiDevImageFuncName, buildApiImageFuncName, "REST API", "rest-api", installerPkg, gen.ModulePath)
	emitImageReleaseFunc(f, buildApiReleaseImageFuncName, buildApiImageFuncName, "REST API", "rest-api", installerPkg, releaseImageRepoConst, gen.ModulePath)

	// binary build function for database migrator
	emitBinFunc(f, buildDbMigratorFuncName, "database migrator", "database-migrator", "cmd/database-migrator")
	emitBinDevFunc(f, buildDbMigratorDevFuncName, buildDbMigratorFuncName, "database migrator", "database-migrator")
	emitBinReleaseFunc(f, buildDbMigratorReleaseFuncName, buildDbMigratorFuncName, "database migrator", "database-migrator")

	dbMigratorImageName := "threeport-database-migrator"
	if gen.Module {
		dbMigratorImageName = fmt.Sprintf(
			"threeport-%s-database-migrator",
			strcase.ToKebab(sdkConfig.ModuleName),
		)
	}
	dbMigratorPackageFuncName := "dbMigratorImagePackage"
	allComponents = append(allComponents, componentSpec{
		BinaryName:      "database-migrator",
		PackageDir:      "cmd/database-migrator",
		ImageName:       dbMigratorImageName,
		PackageFuncName: dbMigratorPackageFuncName,
	})
	emitImagePackageFunc(f, dbMigratorPackageFuncName, "database migrator", "database-migrator", dbMigratorImageName)
	emitImageFunc(f, buildDbMigratorImageFuncName, "database migrator", "database-migrator", "cmd/database-migrator", dbMigratorPackageFuncName)
	emitImageDevFunc(f, buildDbMigratorDevImageFuncName, buildDbMigratorImageFuncName, "database migrator", "database-migrator", installerPkg, gen.ModulePath)
	emitImageReleaseFunc(f, buildDbMigratorReleaseImageFuncName, buildDbMigratorImageFuncName, "database migrator", "database-migrator", installerPkg, releaseImageRepoConst, gen.ModulePath)

	if !gen.Module {
		// add function names to "build all" functions
		buildFuncNames = append(buildFuncNames, buildAgentFuncName)
		buildDevFuncNames = append(buildDevFuncNames, buildAgentDevFuncName)
		buildReleaseFuncNames = append(buildReleaseFuncNames, buildAgentReleaseFuncName)

		emitBinFunc(f, buildAgentFuncName, "agent", "agent", "cmd/agent")
		emitBinDevFunc(f, buildAgentDevFuncName, buildAgentFuncName, "agent", "agent")
		emitBinReleaseFunc(f, buildAgentReleaseFuncName, buildAgentFuncName, "agent", "agent")

		agentPackageFuncName := "agentImagePackage"
		allComponents = append(allComponents, componentSpec{
			BinaryName:      "agent",
			PackageDir:      "cmd/agent",
			ImageName:       "threeport-agent",
			PackageFuncName: agentPackageFuncName,
			// ApiName is empty so the agent is always built alongside
			// rest-api and database-migrator. tptctl up installs the
			// agent unconditionally, so the image needs to be present.
		})
		emitImagePackageFunc(f, agentPackageFuncName, "agent", "agent", "threeport-agent")
		emitImageFunc(f, buildAgentImageFuncName, "agent", "agent", "cmd/agent", agentPackageFuncName)
		emitImageDevFunc(f, buildAgentDevImageFuncName, buildAgentImageFuncName, "agent", "agent", installerPkg, gen.ModulePath)
		emitImageReleaseFunc(f, buildAgentReleaseImageFuncName, buildAgentImageFuncName, "agent", "agent", installerPkg, releaseImageRepoConst, gen.ModulePath)
	}

	// binary build functions for controllers
	for _, objGroup := range gen.ApiObjectGroups {
		if len(objGroup.ReconciledObjects) > 0 {
			// set func names
			buildFuncName := fmt.Sprintf("%sControllerBin", objGroup.ControllerDomain)
			buildFuncNames = append(buildFuncNames, buildFuncName)

			buildDevFuncName := fmt.Sprintf("%sControllerBinDev", objGroup.ControllerDomain)
			buildDevFuncNames = append(buildDevFuncNames, buildDevFuncName)

			buildReleaseFuncName := fmt.Sprintf("%sControllerBinRelease", objGroup.ControllerDomain)
			buildReleaseFuncNames = append(buildReleaseFuncNames, buildReleaseFuncName)

			buildImageFuncName := fmt.Sprintf("%sControllerImage", objGroup.ControllerDomain)
			buildDevImageFuncName := fmt.Sprintf("%sControllerImageDev", objGroup.ControllerDomain)
			buildReleaseImageFuncName := fmt.Sprintf("%sControllerImageRelease", objGroup.ControllerDomain)

			// set image name
			imageName := fmt.Sprintf("threeport-%s", objGroup.ControllerName)
			if gen.Module {
				imageName = fmt.Sprintf("threeport-%s-%s", strcase.ToKebab(sdkConfig.ModuleName), objGroup.ControllerName)
			}

			packageDir := fmt.Sprintf("cmd/%s", objGroup.ControllerName)
			emitBinFunc(f, buildFuncName, objGroup.ControllerName, objGroup.ControllerName, packageDir)
			emitBinDevFunc(f, buildDevFuncName, buildFuncName, objGroup.ControllerName, objGroup.ControllerName)
			emitBinReleaseFunc(f, buildReleaseFuncName, buildFuncName, objGroup.ControllerName, objGroup.ControllerName)

			packageFuncName := fmt.Sprintf("%sControllerImagePackage", strcase.ToLowerCamel(objGroup.ControllerDomain))
			// derive the sdk-config api name from the controller name:
			// strip the "-controller" suffix and convert dashes back to
			// underscores, inverting the mapping in generator.go that
			// produced ControllerName.
			apiName := strings.ReplaceAll(
				strings.TrimSuffix(objGroup.ControllerName, "-controller"),
				"-",
				"_",
			)
			allComponents = append(allComponents, componentSpec{
				BinaryName:      objGroup.ControllerName,
				PackageDir:      packageDir,
				ImageName:       imageName,
				PackageFuncName: packageFuncName,
				ApiName:         apiName,
			})
			emitImagePackageFunc(f, packageFuncName, objGroup.ControllerName, objGroup.ControllerName, imageName)
			emitImageFunc(f, buildImageFuncName, objGroup.ControllerName, objGroup.ControllerName, packageDir, packageFuncName)
			emitImageDevFunc(f, buildDevImageFuncName, buildImageFuncName, objGroup.ControllerName, objGroup.ControllerName, installerPkg, gen.ModulePath)
			emitImageReleaseFunc(f, buildReleaseImageFuncName, buildImageFuncName, objGroup.ControllerName, objGroup.ControllerName, installerPkg, releaseImageRepoConst, gen.ModulePath)
		}
	}
	f.Line()

	// build all binaries
	buildAllFuncName := "AllBins"
	f.Comment(fmt.Sprintf("%s builds the binaries for all components.", buildAllFuncName))
	f.Func().Params(Id("Build")).Id(buildAllFuncName).Params(Id("arch").String()).Error().BlockFunc(func(g *Group) {
		g.Id("build").Op(":=").Id("Build").Values()
		for _, funcName := range buildFuncNames {
			g.If(Err().Op(":=").Id("build").Dot(funcName).Call(Id("arch")).Op(";").Err().Op("!=").Nil()).Block(
				Return().Qual("fmt", "Errorf").Call(
					Lit("failed to build binary: %w"),
					Err(),
				),
			)
			g.Line()
		}

		g.Return().Nil()
	})

	// build all dev binaries
	buildAllDevFuncName := "AllBinsDev"
	f.Comment(fmt.Sprintf("%s builds the development binaries for all components.", buildAllDevFuncName))
	f.Func().Params(Id("Build")).Id(buildAllDevFuncName).Params().Error().BlockFunc(func(g *Group) {
		g.Id("build").Op(":=").Id("Build").Values()
		for _, funcName := range buildDevFuncNames {
			g.If(Err().Op(":=").Id("build").Dot(funcName).Call().Op(";").Err().Op("!=").Nil()).Block(
				Return().Qual("fmt", "Errorf").Call(
					Lit("failed to build binary: %w"),
					Err(),
				),
			)
			g.Line()
		}

		g.Return().Nil()
	})

	// build all release binaries
	buildAllReleaseFuncName := "AllBinsRelease"
	f.Comment(fmt.Sprintf("%s builds the release binaries for all components.", buildAllReleaseFuncName))
	f.Func().Params(Id("Build")).Id(buildAllReleaseFuncName).Params().Error().BlockFunc(func(g *Group) {
		g.Id("build").Op(":=").Id("Build").Values()
		for _, funcName := range buildReleaseFuncNames {
			g.If(Err().Op(":=").Id("build").Dot(funcName).Call().Op(";").Err().Op("!=").Nil()).Block(
				Return().Qual("fmt", "Errorf").Call(
					Lit("failed to build binary: %w"),
					Err(),
				),
			)
			g.Line()
		}

		g.Return().Nil()
	})

	// build and push all images
	buildAllImagesFuncName := "AllImages"
	f.Comment(fmt.Sprintf("%s builds and pushes images for all components. Pre-compiles", buildAllImagesFuncName))
	f.Comment("binaries for every requested arch in parallel, then packages each")
	f.Comment("component image in parallel. Set PARALLEL >= 1 to cap packaging")
	f.Comment("concurrency (e.g. `PARALLEL=4 mage build:allImages ghcr.io/foo v1 amd64,arm64`).")
	f.Func().Params(Id("Build")).Id(buildAllImagesFuncName).Params(
		Line().Id("imageRepo").String(),
		Line().Id("imageTag").String(),
		Line().Id("arch").String(),
		Line(),
	).Error().BlockFunc(func(g *Group) {
		emitPrebuildBlock(g, allComponents)

		g.Id("build").Op(":=").Id("Build").Values()
		emitWrapHelper(g, Id("imageRepo"), Id("imageTag"))
		g.Id("tasks").Op(":=").Index().Func().Params().Error().ValuesFunc(func(v *Group) {
			for _, c := range allComponents {
				v.Line().Id("wrap").Call(Id("build").Dot(c.PackageFuncName))
			}
			v.Line()
		})

		g.Return().Qual("github.com/threeport/threeport/pkg/util/v0", "RunParallel").Call(
			Id("parallelFromEnv").Call(),
			Id("tasks"),
		)
	})

	// build and push all dev images
	buildAllDevImagesFuncName := "AllImagesDev"
	f.Comment(fmt.Sprintf("%s builds and pushes development images for all components.", buildAllDevImagesFuncName))
	f.Comment("Set PARALLEL >= 1 to control worker concurrency (e.g. `PARALLEL=4 mage build:allImagesDev`).")
	f.Func().Params(Id("Build")).Id(buildAllDevImagesFuncName).Params().Error().BlockFunc(func(g *Group) {
		g.List(Id("_"), Id("arch"), Id("err")).Op(":=").Id("getBuildVals").Call()
		g.If(Id("err").Op("!=").Nil()).Block(
			Return(Qual("fmt", "Errorf").Call(Lit("failed to get local CPU architecture: %w"), Id("err"))),
		)
		g.Line()

		emitPrebuildBlock(g, allComponents)

		g.Id("build").Op(":=").Id("Build").Values()
		emitWrapHelper(g,
			Qual(installerPkg, "DevImageNamespace"),
			Qual(fmt.Sprintf("%s/internal/version", gen.ModulePath), "GetVersion").Call(),
		)
		g.Id("tasks").Op(":=").Index().Func().Params().Error().ValuesFunc(func(v *Group) {
			for _, c := range allComponents {
				v.Line().Id("wrap").Call(Id("build").Dot(c.PackageFuncName))
			}
			v.Line()
		})
		g.Return().Qual("github.com/threeport/threeport/pkg/util/v0", "RunParallel").Call(
			Id("parallelFromEnv").Call(),
			Id("tasks"),
		)
	})

	// build and push dev images narrowed by sdk-config api name
	buildImagesByApisDevFuncName := "ImagesByApisDev"
	f.Comment(fmt.Sprintf(
		"%s builds and pushes development images for the rest-api,",
		buildImagesByApisDevFuncName,
	))
	f.Comment("database-migrator, and the controllers of the listed sdk-config")
	f.Comment("apis. The agent is not included by default; pass \"agent\" in the")
	f.Comment("apis list (or use AllImagesDev) if it is needed. Set PARALLEL >= 1")
	f.Comment("to control worker concurrency. Example:")
	f.Comment("`PARALLEL=4 mage build:imagesByApisDev kubernetes_workload,gateway,secret`.")
	f.Func().Params(Id("Build")).Id(buildImagesByApisDevFuncName).Params(
		Id("apis").String(),
	).Error().BlockFunc(func(g *Group) {
		g.List(Id("workingDir"), Id("arch"), Id("err")).Op(":=").Id("getBuildVals").Call()
		g.If(Id("err").Op("!=").Nil()).Block(
			Return(Qual("fmt", "Errorf").Call(Lit("failed to get build values: %w"), Id("err"))),
		)
		g.Line()

		// partition components into always-included (no ApiName) and
		// api-scoped controllers so the runtime lookup only covers
		// the controller set.
		var alwaysIncluded []componentSpec
		var controllerComponents []componentSpec
		for _, c := range allComponents {
			if c.ApiName == "" {
				alwaysIncluded = append(alwaysIncluded, c)
			} else {
				controllerComponents = append(controllerComponents, c)
			}
		}

		// emit a static map keyed by api name → controller entry.
		// Each entry pairs the package dir (for the upfront BuildBinaries
		// call) with the per-component package method value (for the
		// per-image task).
		g.Type().Id("controllerEntry").Struct(
			Id("packageDir").String(),
			Id("packageFunc").Func().Params(String(), String(), String(), String()).Error(),
		)
		g.Line()

		g.Id("build").Op(":=").Id("Build").Values()
		g.Line()

		g.Id("controllersByApi").Op(":=").Map(String()).Id("controllerEntry").ValuesFunc(func(v *Group) {
			for _, c := range controllerComponents {
				v.Line().Lit(c.ApiName).Op(":").Values(Dict{
					Id("packageDir"):  Lit(c.PackageDir),
					Id("packageFunc"): Id("build").Dot(c.PackageFuncName),
				})
			}
			v.Line()
		})
		g.Line()

		// known api list for the unknown-api error message.
		g.Id("validApiNames").Op(":=").Func().Params().Index().String().Block(
			Id("names").Op(":=").Make(Index().String(), Lit(0), Len(Id("controllersByApi"))),
			For(List(Id("name"), Id("_")).Op(":=").Range().Id("controllersByApi")).Block(
				Id("names").Op("=").Append(Id("names"), Id("name")),
			),
			Qual("sort", "Strings").Call(Id("names")),
			Return(Id("names")),
		).Call()
		g.Line()

		// parse the comma-separated api list, trimming whitespace
		// and dropping empty fragments.
		g.Id("requestedApis").Op(":=").Index().String().Values()
		g.For(List(Id("_"), Id("part")).Op(":=").Range().Qual("strings", "Split").Call(Id("apis"), Lit(","))).Block(
			Id("trimmed").Op(":=").Qual("strings", "TrimSpace").Call(Id("part")),
			If(Id("trimmed").Op("!=").Lit("")).Block(
				Id("requestedApis").Op("=").Append(Id("requestedApis"), Id("trimmed")),
			),
		)
		g.Line()

		// resolve each requested api to its controller entry,
		// erroring out on the first unknown name.
		g.Id("selectedControllers").Op(":=").Make(Index().Id("controllerEntry"), Lit(0), Len(Id("requestedApis")))
		g.For(List(Id("_"), Id("apiName")).Op(":=").Range().Id("requestedApis")).Block(
			List(Id("entry"), Id("ok")).Op(":=").Id("controllersByApi").Index(Id("apiName")),
			If(Op("!").Id("ok")).Block(
				Return(Qual("fmt", "Errorf").Call(
					Lit("unknown api %q: valid choices are %s"),
					Id("apiName"),
					Qual("strings", "Join").Call(Id("validApiNames"), Lit(", ")),
				)),
			),
			Id("selectedControllers").Op("=").Append(Id("selectedControllers"), Id("entry")),
		)
		g.Line()

		// pre-compile every binary for every requested arch in one go
		// build per arch (arches run in parallel) so dependency
		// compilation is shared across components within an arch. Each
		// per-image task below then only packages the pre-built binary.
		g.Id("arches").Op(":=").Index().String().Values()
		g.For(List(Id("_"), Id("a")).Op(":=").Range().Qual("strings", "Split").Call(Id("arch"), Lit(","))).Block(
			Id("a").Op("=").Qual("strings", "TrimSpace").Call(Id("a")),
			If(Id("a").Op("!=").Lit("")).Block(
				Id("arches").Op("=").Append(Id("arches"), Id("a")),
			),
		)
		g.Line()

		// build the packageDirs list: always-included dirs first, then
		// one entry per selected controller.
		g.Id("packageDirs").Op(":=").Index().String().ValuesFunc(func(v *Group) {
			for _, c := range alwaysIncluded {
				v.Line().Lit(c.PackageDir)
			}
			v.Line()
		})
		g.For(List(Id("_"), Id("entry")).Op(":=").Range().Id("selectedControllers")).Block(
			Id("packageDirs").Op("=").Append(Id("packageDirs"), Id("entry").Dot("packageDir")),
		)
		g.Line()

		g.If(Err().Op(":=").Qual(
			"github.com/threeport/threeport/pkg/util/v0",
			"BuildBinaries",
		).Call(
			Line().Id("workingDir"),
			Line().Id("arches"),
			Line().Id("packageDirs"),
			Line().False(),
			Line().False(),
			Line(),
		).Op(";").Err().Op("!=").Nil()).Block(
			Return(Qual("fmt", "Errorf").Call(Lit("failed to pre-build binaries: %w"), Err())),
		)
		g.Line()

		emitWrapHelper(g,
			Qual(installerPkg, "DevImageNamespace"),
			Qual(fmt.Sprintf("%s/internal/version", gen.ModulePath), "GetVersion").Call(),
		)
		g.Line()

		// always-included tasks first, then one per selected controller.
		g.Id("tasks").Op(":=").Index().Func().Params().Error().ValuesFunc(func(v *Group) {
			for _, c := range alwaysIncluded {
				v.Line().Id("wrap").Call(Id("build").Dot(c.PackageFuncName))
			}
			v.Line()
		})
		g.For(List(Id("_"), Id("entry")).Op(":=").Range().Id("selectedControllers")).Block(
			Id("tasks").Op("=").Append(Id("tasks"), Id("wrap").Call(Id("entry").Dot("packageFunc"))),
		)
		g.Line()

		g.Return().Qual("github.com/threeport/threeport/pkg/util/v0", "RunParallel").Call(
			Id("parallelFromEnv").Call(),
			Id("tasks"),
		)
	})

	// build and push multi-arch images narrowed by sdk-config api name
	buildImagesByApisFuncName := "ImagesByApis"
	f.Comment(fmt.Sprintf(
		"%s builds and pushes multi-arch container images for the rest-api,",
		buildImagesByApisFuncName,
	))
	f.Comment("database-migrator, and the controllers of the listed sdk-config apis")
	f.Comment("to the given registry under the given tag. The rest-api, database-migrator,")
	f.Comment("by default; pass \"agent\" in the apis list (or use AllImages) if it")
	f.Comment("is needed. Set PARALLEL >= 1 to control worker concurrency. Example:")
	f.Comment("`PARALLEL=2 mage build:imagesByApis machine_workload,machine_runtime ghcr.io/myorg dev-pinned-abc1234 amd64,arm64`.")
	f.Func().Params(Id("Build")).Id(buildImagesByApisFuncName).Params(
		Line().Id("apis").String(),
		Line().Id("imageRepo").String(),
		Line().Id("imageTag").String(),
		Line().Id("arch").String(),
		Line(),
	).Error().BlockFunc(func(g *Group) {
		g.List(Id("workingDir"), Id("_"), Id("err")).Op(":=").Id("getBuildVals").Call()
		g.If(Id("err").Op("!=").Nil()).Block(
			Return(Qual("fmt", "Errorf").Call(Lit("failed to get build values: %w"), Id("err"))),
		)
		g.Line()

		// partition components into always-included (no ApiName) and
		// api-scoped controllers so the runtime lookup only covers
		// the controller set.
		var alwaysIncluded []componentSpec
		var controllerComponents []componentSpec
		for _, c := range allComponents {
			if c.ApiName == "" {
				alwaysIncluded = append(alwaysIncluded, c)
			} else {
				controllerComponents = append(controllerComponents, c)
			}
		}

		// emit a static map keyed by api name → controller entry.
		// Each entry pairs the package dir (for the upfront BuildBinaries
		// call) with the per-component package method value (for the
		// per-image task).
		g.Type().Id("controllerEntry").Struct(
			Id("packageDir").String(),
			Id("packageFunc").Func().Params(String(), String(), String(), String()).Error(),
		)
		g.Line()

		g.Id("build").Op(":=").Id("Build").Values()
		g.Line()

		g.Id("controllersByApi").Op(":=").Map(String()).Id("controllerEntry").ValuesFunc(func(v *Group) {
			for _, c := range controllerComponents {
				v.Line().Lit(c.ApiName).Op(":").Values(Dict{
					Id("packageDir"):  Lit(c.PackageDir),
					Id("packageFunc"): Id("build").Dot(c.PackageFuncName),
				})
			}
			v.Line()
		})
		g.Line()

		// known api list for the unknown-api error message.
		g.Id("validApiNames").Op(":=").Func().Params().Index().String().Block(
			Id("names").Op(":=").Make(Index().String(), Lit(0), Len(Id("controllersByApi"))),
			For(List(Id("name"), Id("_")).Op(":=").Range().Id("controllersByApi")).Block(
				Id("names").Op("=").Append(Id("names"), Id("name")),
			),
			Qual("sort", "Strings").Call(Id("names")),
			Return(Id("names")),
		).Call()
		g.Line()

		// parse the comma-separated api list, trimming whitespace
		// and dropping empty fragments.
		g.Id("requestedApis").Op(":=").Index().String().Values()
		g.For(List(Id("_"), Id("part")).Op(":=").Range().Qual("strings", "Split").Call(Id("apis"), Lit(","))).Block(
			Id("trimmed").Op(":=").Qual("strings", "TrimSpace").Call(Id("part")),
			If(Id("trimmed").Op("!=").Lit("")).Block(
				Id("requestedApis").Op("=").Append(Id("requestedApis"), Id("trimmed")),
			),
		)
		g.Line()

		// resolve each requested api to its controller entry,
		// erroring out on the first unknown name.
		g.Id("selectedControllers").Op(":=").Make(Index().Id("controllerEntry"), Lit(0), Len(Id("requestedApis")))
		g.For(List(Id("_"), Id("apiName")).Op(":=").Range().Id("requestedApis")).Block(
			List(Id("entry"), Id("ok")).Op(":=").Id("controllersByApi").Index(Id("apiName")),
			If(Op("!").Id("ok")).Block(
				Return(Qual("fmt", "Errorf").Call(
					Lit("unknown api %q: valid choices are %s"),
					Id("apiName"),
					Qual("strings", "Join").Call(Id("validApiNames"), Lit(", ")),
				)),
			),
			Id("selectedControllers").Op("=").Append(Id("selectedControllers"), Id("entry")),
		)
		g.Line()

		// pre-compile every binary for every requested arch in one go
		// build per arch (arches run in parallel) so dependency
		// compilation is shared across components within an arch. Each
		// per-image task below then only packages the pre-built binary.
		g.Id("arches").Op(":=").Index().String().Values()
		g.For(List(Id("_"), Id("a")).Op(":=").Range().Qual("strings", "Split").Call(Id("arch"), Lit(","))).Block(
			Id("a").Op("=").Qual("strings", "TrimSpace").Call(Id("a")),
			If(Id("a").Op("!=").Lit("")).Block(
				Id("arches").Op("=").Append(Id("arches"), Id("a")),
			),
		)
		g.Line()

		// build the packageDirs list: always-included dirs first, then
		// one entry per selected controller.
		g.Id("packageDirs").Op(":=").Index().String().ValuesFunc(func(v *Group) {
			for _, c := range alwaysIncluded {
				v.Line().Lit(c.PackageDir)
			}
			v.Line()
		})
		g.For(List(Id("_"), Id("entry")).Op(":=").Range().Id("selectedControllers")).Block(
			Id("packageDirs").Op("=").Append(Id("packageDirs"), Id("entry").Dot("packageDir")),
		)
		g.Line()

		g.If(Err().Op(":=").Qual(
			"github.com/threeport/threeport/pkg/util/v0",
			"BuildBinaries",
		).Call(
			Line().Id("workingDir"),
			Line().Id("arches"),
			Line().Id("packageDirs"),
			Line().False(),
			Line().False(),
			Line(),
		).Op(";").Err().Op("!=").Nil()).Block(
			Return(Qual("fmt", "Errorf").Call(Lit("failed to pre-build binaries: %w"), Err())),
		)
		g.Line()

		emitWrapHelper(g, Id("imageRepo"), Id("imageTag"))
		g.Line()

		// always-included tasks first, then one per selected controller.
		g.Id("tasks").Op(":=").Index().Func().Params().Error().ValuesFunc(func(v *Group) {
			for _, c := range alwaysIncluded {
				v.Line().Id("wrap").Call(Id("build").Dot(c.PackageFuncName))
			}
			v.Line()
		})
		g.For(List(Id("_"), Id("entry")).Op(":=").Range().Id("selectedControllers")).Block(
			Id("tasks").Op("=").Append(Id("tasks"), Id("wrap").Call(Id("entry").Dot("packageFunc"))),
		)
		g.Line()

		g.Return().Qual("github.com/threeport/threeport/pkg/util/v0", "RunParallel").Call(
			Id("parallelFromEnv").Call(),
			Id("tasks"),
		)
	})

	// build and push all release images
	buildAllReleaseImagesFuncName := "AllImagesRelease"
	f.Comment(fmt.Sprintf("%s builds and pushes release images for all components.", buildAllReleaseImagesFuncName))
	f.Comment("Set PARALLEL >= 1 to control worker concurrency (e.g. `PARALLEL=4 mage build:allImagesRelease`).")
	f.Func().Params(Id("Build")).Id(buildAllReleaseImagesFuncName).Params().Error().BlockFunc(func(g *Group) {
		g.Id("arch").Op(":=").Id("releaseArch")
		g.Line()

		emitPrebuildBlock(g, allComponents)

		g.Id("build").Op(":=").Id("Build").Values()
		emitWrapHelper(g,
			Qual(installerPkg, releaseImageRepoConst),
			Qual(fmt.Sprintf("%s/internal/version", gen.ModulePath), "GetVersion").Call(),
		)
		g.Id("tasks").Op(":=").Index().Func().Params().Error().ValuesFunc(func(v *Group) {
			for _, c := range allComponents {
				v.Line().Id("wrap").Call(Id("build").Dot(c.PackageFuncName))
			}
			v.Line()
		})
		g.Return().Qual("github.com/threeport/threeport/pkg/util/v0", "RunParallel").Call(
			Id("parallelFromEnv").Call(),
			Id("tasks"),
		)
	})

	// helper: parse the PARALLEL env var, default to 1
	f.Comment("parallelFromEnv returns the PARALLEL env var as an int, defaulting to 1.")
	f.Func().Id("parallelFromEnv").Params().Int().BlockFunc(func(g *Group) {
		g.Id("v").Op(":=").Qual("os", "Getenv").Call(Lit("PARALLEL"))
		g.If(Id("v").Op("==").Lit("")).Block(
			Return(Lit(1)),
		)
		g.List(Id("n"), Err()).Op(":=").Qual("strconv", "Atoi").Call(Id("v"))
		g.If(Err().Op("!=").Nil().Op("||").Id("n").Op("<").Lit(1)).Block(
			Return(Lit(1)),
		)
		g.Return(Id("n"))
	})

	// dev image loads to kind clusters
	f.Comment("LoadImage builds and loads an image to the provided kind cluster.")
	f.Func().Params(Id("Dev")).Id("LoadImage").Params(
		Id("kindClusterName").String(),
		Id("component").String(),
	).Error().BlockFunc(func(g *Group) {
		g.List(Id("workingDir"), Id("arch"), Id("err")).Op(":=").Id("getBuildVals").Call()
		g.If(Id("err").Op("!=").Nil()).Block(
			Return(Qual("fmt", "Errorf").Call(Lit("failed to get build values: %w"), Id("err"))),
		)
		g.Line()

		g.If(Err().Op(":=").Qual(
			"github.com/threeport/threeport/pkg/util/v0",
			"BuildBinaries",
		).Call(
			Line().Id("workingDir"),
			Line().Index().String().Values(Id("arch")),
			Line().Index().String().Values(Qual("fmt", "Sprintf").Call(Lit("cmd/%s"), Id("component"))),
			Line().False(),
			Line().False(),
			Line(),
		).Op(";").Err().Op("!=").Nil()).Block(
			Return(Qual("fmt", "Errorf").Call(Lit("failed to build binary: %w"), Id("err"))),
		)
		g.Line()

		if gen.Module {
			g.Id("imageName").Op(":=").Qual("fmt", "Sprintf").Call(
				Lit("threeport-%s-%s"),
				Lit(strcase.ToKebab(sdkConfig.ModuleName)),
				Id("component"),
			)
		} else {
			g.Id("imageName").Op(":=").Qual("fmt", "Sprintf").Call(Lit("threeport-%s"), Id("component"))
		}
		g.Line()

		g.If(Err().Op(":=").Qual(
			"github.com/threeport/threeport/pkg/util/v0",
			"BuildImage",
		).Call(
			Line().Id("workingDir"),
			Line().Lit("Dockerfile"),
			Line().Lit("release"),
			Line().Id("arch"),
			Line().Id("component"),
			Line().Lit("bin"),
			Line().Nil(),
			Line().Qual(
				installerPkg,
				"DevImageNamespace",
			),
			Line().Id("imageName"),
			Line().Qual(
				fmt.Sprintf("%s/internal/version", gen.ModulePath),
				"GetVersion",
			).Call(),
			Line().False(),
			Line().True(),
			Line().Id("kindClusterName"),
			Line(),
		).Op(";").Err().Op("!=").Nil()).Block(
			Return(Qual("fmt", "Errorf").Call(Lit("failed to build and load image: %w"), Id("err"))),
		)
		g.Line()

		g.Return(Nil())
	})
	f.Line()

	// extension plugin build
	if gen.Module {
		f.Comment("Plugin compiles the extension's tptctl plugin.")
		f.Func().Params(Id("Build")).Id("Plugin").Params().Error().Block(
			Id("buildCmd").Op(":=").Qual("os/exec", "Command").Call(
				Line().Lit("go"),
				Line().Lit("build"),
				Line().Lit("-o"),
				Line().Lit(fmt.Sprintf(
					"bin/%s",
					strcase.ToKebab(sdkConfig.ModuleName),
				)),
				Line().Lit(fmt.Sprintf(
					"cmd/%s/main_gen.go",
					strcase.ToSnake(sdkConfig.ModuleName),
				)),
				Line(),
			),
			Line(),

			Id("output").Op(",").Id("err").Op(":=").Id("buildCmd").Dot("CombinedOutput").Call(),
			If(Id("err").Op("!=").Nil()).Block(
				Return(Qual("fmt", "Errorf").Call(
					Lit("build failed for tptctl plugin with output '%s': %w"),
					Id("output"),
					Id("err"),
				)),
			),
			Line(),

			Qual("fmt", "Println").Call(Lit(fmt.Sprintf(
				"tptctl plugin built and available at bin/%s",
				strcase.ToKebab(sdkConfig.ModuleName),
			))),
			Line(),

			Return(Nil()),
		)
		f.Line()
	}

	// API docs generation
	f.Comment("GenerateSwaggerDocs generates the API server swagger documentation served by the API.")
	f.Func().Params(Id("Dev")).Id("GenerateSwaggerDocs").Params().Error().Block(
		Id("docsDestination").Op(":=").Lit("pkg/api-server/v0/docs"),
		Id("swagCmd").Op(":=").Qual("os/exec", "Command").Call(
			Line().Lit("swag"),
			Line().Lit("init"),
			Line().Lit("--dir"),
			Line().Lit("cmd/rest-api,pkg/api,pkg/api-server/v0"),
			Line().Lit("--parseDependency"),
			Line().Lit("--generalInfo"),
			Line().Lit("main_gen.go"),
			Line().Lit("--output"),
			Line().Id("docsDestination"),
			Line(),
		),
		Line(),

		List(Id("output"), Err()).Op(":=").Id("swagCmd").Dot("CombinedOutput").Call(),
		If(Err().Op("!=").Nil()).Block(
			Return(Qual("fmt", "Errorf").Call(Lit("API docs generation failed with output '%s': %w"), Id("output"), Err())),
		),
		Line(),

		Qual("fmt", "Printf").Call(Lit("API docs generated in %s\n"), Id("docsDestination")),
		Line(),

		Return(Nil()),
	)
	f.Line()

	// local registry creation
	f.Comment("LocalRegistryUp starts a docker container to serve as a local container registry.")
	f.Func().Params(Id("Dev")).Id("LocalRegistryUp").Params().Error().Block(
		If(Err().Op(":=").Qual(
			"github.com/threeport/threeport/pkg/threeport-installer/v0/tptdev",
			"CreateLocalRegistry",
		).Call()).Op(";").Err().Op("!=").Nil().Block(
			Return(Qual("fmt", "Errorf").Call(Lit("failed to create local container registry: %w"), Err())),
		),
		Line(),

		Return().Nil(),
	)

	// local registry deletion
	f.Comment("LocalRegistryDown stops and removes the local container registry.")
	f.Func().Params(Id("Dev")).Id("LocalRegistryDown").Params().Error().Block(
		If(Err().Op(":=").Qual(
			"github.com/threeport/threeport/pkg/threeport-installer/v0/tptdev",
			"DeleteLocalRegistry",
		).Call()).Op(";").Err().Op("!=").Nil().Block(
			Return(Qual("fmt", "Errorf").Call(Lit("failed to remove local container registry: %w"), Err())),
		),
		Line(),

		Return().Nil(),
	)

	// build vals utility function
	f.Comment("getBuildVals returns the working directory and arch for builds.")
	f.Func().Id("getBuildVals").Params().Params(
		String(),
		String(),
		Error(),
	).Block(
		List(Id("workingDir"), Err()).Op(":=").Qual("os", "Getwd").Call(),
		If(Err().Op("!=").Nil()).Block(
			Return(Lit(""), Lit(""), Qual("fmt", "Errorf").Call(Lit("failed to get working directory: %w"), Err())),
		),
		Line(),

		Id("arch").Op(":=").Qual("runtime", "GOARCH"),
		Line(),

		Return(Id("workingDir"), Id("arch"), Nil()),
	)

	// write code to file if not excluded by SDK config
	genFilepath := filepath.Join("magefiles", "magefile_gen.go")
	if slices.Contains(sdkConfig.ExcludeFiles, genFilepath) {
		cli.Info(fmt.Sprintf("source code generation skipped for %s", genFilepath))
	} else {
		_, err := util.WriteCodeToFile(f, genFilepath, true)
		if err != nil {
			return fmt.Errorf("failed to write generated code to file %s: %w", genFilepath, err)
		}
		cli.Info(fmt.Sprintf("source code for magefile written to %s", genFilepath))
	}

	return nil
}

// emitBinFunc writes a `func (Build) <BinFunc>(arch string) error` that
// compiles the component's binary via util.BuildBinaries with a
// single-element packageDirs slice.
func emitBinFunc(f *File, funcName, displayName, binaryName, packageDir string) {
	f.Comment(fmt.Sprintf("%s builds the %s binary.", funcName, displayName))
	f.Func().Params(Id("Build")).Id(funcName).Params(Id("arch").String()).Error().Block(
		List(Id("workingDir"), Id("_"), Err()).Op(":=").Id("getBuildVals").Call(),
		If(Err().Op("!=").Nil()).Block(
			Return().Qual("fmt", "Errorf").Call(Lit("failed to get working directory: %w"), Err()),
		),
		Line(),

		If(Err().Op(":=").Qual(
			"github.com/threeport/threeport/pkg/util/v0",
			"BuildBinaries",
		).Call(
			Line().Id("workingDir"),
			Line().Index().String().Values(Id("arch")),
			Line().Index().String().Values(Lit(packageDir)),
			Line().False(),
			Line().False(),
			Line(),
		).Op(";").Err().Op("!=").Nil()).Block(
			Return().Qual("fmt", "Errorf").Call(
				Lit(fmt.Sprintf("failed to build %s binary: %%w", binaryName)),
				Err(),
			),
		),
		Line(),

		Qual("fmt", "Printf").Call(Lit(fmt.Sprintf(
			"binary built and available at bin/%%s/%s\n", binaryName,
		)), Id("arch")),
		Line(),

		Return().Nil(),
	)
	f.Line()
}

// emitBinDevFunc writes the no-arg `<BinFunc>Dev` wrapper.
func emitBinDevFunc(f *File, funcName, baseFuncName, displayName, binaryName string) {
	f.Comment(fmt.Sprintf("%s builds the %s binary for the architcture of the machine", funcName, displayName))
	f.Comment("where it is built.")
	f.Func().Params(Id("Build")).Id(funcName).Params().Error().Block(
		List(Id("_"), Id("arch"), Err()).Op(":=").Id("getBuildVals").Call(),
		If(Err().Op("!=").Nil()).Block(
			Return().Qual("fmt", "Errorf").Call(Lit("failed to get local CPU architecture: %w"), Err()),
		),
		Line(),
		Id("build").Op(":=").Id("Build").Values(),
		If(Err().Op(":=").Id("build").Dot(baseFuncName).Call(Id("arch")).Op(";").Err().Op("!=").Nil()).Block(
			Return().Qual("fmt", "Errorf").Call(Lit(fmt.Sprintf(
				"failed to build dev %s binary: %%w", binaryName,
			)), Err()),
		),
		Line(),
		Return().Nil(),
	)
	f.Line()
}

// emitBinReleaseFunc writes the no-arg `<BinFunc>Release` wrapper.
func emitBinReleaseFunc(f *File, funcName, baseFuncName, displayName, binaryName string) {
	f.Comment(fmt.Sprintf("%s builds the %s binary for release architecture.", funcName, displayName))
	f.Func().Params(Id("Build")).Id(funcName).Params().Error().Block(
		Id("build").Op(":=").Id("Build").Values(),
		If(Err().Op(":=").Id("build").Dot(baseFuncName).Call(Id("releaseArch")).Op(";").Err().Op("!=").Nil()).Block(
			Return().Qual("fmt", "Errorf").Call(Lit(fmt.Sprintf(
				"failed to build release %s binary: %%w", binaryName,
			)), Err()),
		),
		Line(),
		Return().Nil(),
	)
	f.Line()
}

// emitImageFunc writes a `func (Build) <ImageFunc>(repo, tag, arch) error`
// that compiles the binary for every requested arch via BuildBinaries,
// then delegates packaging to the per-component package function. When
// called from AllImages the BuildBinaries call is a Go cache hit
// (AllImages pre-compiled the same package earlier); when called
// standalone it does the actual compile.
func emitImageFunc(f *File, funcName, displayName, binaryName, packageDir, packageFuncName string) {
	f.Comment(fmt.Sprintf("%s builds and pushes a %s container image.", funcName, displayName))
	f.Func().Params(Id("Build")).Id(funcName).Params(
		Line().Id("imageRepo").String(),
		Line().Id("imageTag").String(),
		Line().Id("arch").String(),
		Line(),
	).Parens(Error()).Block(
		List(Id("workingDir"), Id("_"), Err()).Op(":=").Id("getBuildVals").Call(),
		If(Err().Op("!=").Nil()).Block(
			Return(Qual("fmt", "Errorf").Call(Lit("failed to get working directory: %w"), Err())),
		),
		Line(),

		Id("arches").Op(":=").Index().String().Values(),
		For(List(Id("_"), Id("a")).Op(":=").Range().Qual("strings", "Split").Call(Id("arch"), Lit(","))).Block(
			Id("a").Op("=").Qual("strings", "TrimSpace").Call(Id("a")),
			If(Id("a").Op("!=").Lit("")).Block(
				Id("arches").Op("=").Append(Id("arches"), Id("a")),
			),
		),
		If(Err().Op(":=").Qual(
			"github.com/threeport/threeport/pkg/util/v0",
			"BuildBinaries",
		).Call(
			Line().Id("workingDir"),
			Line().Id("arches"),
			Line().Index().String().Values(Lit(packageDir)),
			Line().False(),
			Line().False(),
			Line(),
		).Op(";").Err().Op("!=").Nil()).Block(
			Return(Qual("fmt", "Errorf").Call(
				Lit(fmt.Sprintf("failed to build %s binary: %%w", binaryName)),
				Err(),
			)),
		),
		Line(),

		Return(Id("Build").Values().Dot(packageFuncName).Call(Id("workingDir"), Id("imageRepo"), Id("imageTag"), Id("arch"))),
	)
	f.Line()
}

// emitImagePackageFunc writes a private `(Build).<packageFuncName>` method
// that takes a pre-built binary at bin/<arch>/<binaryName> and packages
// it into a container image via util.BuildImage. AllImages* call this
// directly after the upfront BuildBinaries to skip the redundant per-
// component compile that the public <ImageFunc> wrapper does.
func emitImagePackageFunc(f *File, packageFuncName, displayName, binaryName, imageName string) {
	f.Comment(fmt.Sprintf("%s packages a pre-built %s binary into a container image.", packageFuncName, displayName))
	f.Func().Params(Id("Build")).Id(packageFuncName).Params(
		Line().Id("workingDir").String(),
		Line().Id("imageRepo").String(),
		Line().Id("imageTag").String(),
		Line().Id("arch").String(),
		Line(),
	).Parens(Error()).Block(
		If(Err().Op(":=").Qual(
			"github.com/threeport/threeport/pkg/util/v0",
			"BuildImage",
		).Call(
			Line().Id("workingDir"),
			Line().Lit("Dockerfile"),
			Line().Lit("release"),
			Line().Id("arch"),
			Line().Lit(binaryName),
			Line().Lit("bin"),
			Line().Nil(),
			Line().Id("imageRepo"),
			Line().Lit(imageName),
			Line().Id("imageTag"),
			Line().True(),
			Line().False(),
			Line().Lit(""),
			Line(),
		), Err().Op("!=").Nil()).Block(
			Return(Qual("fmt", "Errorf").Call(Lit(fmt.Sprintf(
				"failed to build and push %s image: %%w", binaryName,
			)), Err())),
		),
		Line(),

		Return(Nil()),
	)
	f.Line()
}

// emitImageDevFunc writes the no-arg `<ImageFunc>Dev` wrapper that calls
// the per-component image function with the dev image namespace and the
// host arch.
func emitImageDevFunc(f *File, funcName, baseFuncName, displayName, binaryName, installerPkg, modulePath string) {
	f.Comment(fmt.Sprintf("%s builds and pushes a development %s container image.", funcName, displayName))
	f.Func().Params(Id("Build")).Id(funcName).Params().Error().Block(
		List(Id("_"), Id("arch"), Err()).Op(":=").Id("getBuildVals").Call(),
		If(Err().Op("!=").Nil()).Block(
			Return().Qual("fmt", "Errorf").Call(Lit("failed to get local CPU architecture: %w"), Err()),
		),
		Line(),
		Id("build").Op(":=").Id("Build").Values(),
		If(Err().Op(":=").Id("build").Dot(baseFuncName).Call(
			Line().Qual(installerPkg, "DevImageNamespace"),
			Line().Qual(fmt.Sprintf("%s/internal/version", modulePath), "GetVersion").Call(),
			Line().Id("arch"),
			Line(),
		).Op(";").Err().Op("!=").Nil()).Block(
			Return().Qual("fmt", "Errorf").Call(
				Lit(fmt.Sprintf("failed to build and push dev %s image: %%w", binaryName)),
				Err(),
			),
		),
		Line(),

		Return(Nil()),
	)
	f.Line()
}

// emitImageReleaseFunc writes the no-arg `<ImageFunc>Release` wrapper that
// calls the per-component image function with the release image namespace
// and arch.
func emitImageReleaseFunc(f *File, funcName, baseFuncName, displayName, binaryName, installerPkg, releaseImageRepoConst, modulePath string) {
	f.Comment(fmt.Sprintf("%s builds and pushes a release %s container image.", funcName, displayName))
	f.Func().Params(Id("Build")).Id(funcName).Params().Error().Block(
		Id("build").Op(":=").Id("Build").Values(),
		If(Err().Op(":=").Id("build").Dot(baseFuncName).Call(
			Line().Qual(installerPkg, releaseImageRepoConst),
			Line().Qual(fmt.Sprintf("%s/internal/version", modulePath), "GetVersion").Call(),
			Line().Id("releaseArch"),
			Line(),
		).Op(";").Err().Op("!=").Nil()).Block(
			Return().Qual("fmt", "Errorf").Call(
				Lit(fmt.Sprintf("failed to build and push release %s image: %%w", binaryName)),
				Err(),
			),
		),
		Line(),

		Return(Nil()),
	)
	f.Line()
}

// emitPrebuildBlock writes the upfront BuildBinaries call shared by
// AllImages and its Dev/Release wrappers. Expects `arch` in the caller's
// scope; declares workingDir locally via getBuildVals.
func emitPrebuildBlock(g *Group, components []componentSpec) {
	g.Comment("pre-compile every binary for every requested arch in one go build")
	g.Comment("per arch (arches run in parallel) so dependency compilation is")
	g.Comment("shared across components within an arch. Each per-image task")
	g.Comment("below then only packages the pre-built binary.")
	g.List(Id("workingDir"), Id("_"), Id("err")).Op(":=").Id("getBuildVals").Call()
	g.If(Id("err").Op("!=").Nil()).Block(
		Return(Qual("fmt", "Errorf").Call(Lit("failed to get working directory: %w"), Id("err"))),
	)
	g.Line()

	g.Id("arches").Op(":=").Index().String().ValuesFunc(func(v *Group) {})
	g.For(List(Id("_"), Id("a")).Op(":=").Range().Qual("strings", "Split").Call(Id("arch"), Lit(","))).Block(
		Id("a").Op("=").Qual("strings", "TrimSpace").Call(Id("a")),
		If(Id("a").Op("!=").Lit("")).Block(
			Id("arches").Op("=").Append(Id("arches"), Id("a")),
		),
	)
	g.Line()

	g.Id("packageDirs").Op(":=").Index().String().ValuesFunc(func(v *Group) {
		for _, c := range components {
			v.Line().Lit(c.PackageDir)
		}
		v.Line()
	})
	g.Line()

	g.If(Err().Op(":=").Qual(
		"github.com/threeport/threeport/pkg/util/v0",
		"BuildBinaries",
	).Call(
		Line().Id("workingDir"),
		Line().Id("arches"),
		Line().Id("packageDirs"),
		Line().False(),
		Line().False(),
		Line(),
	).Op(";").Err().Op("!=").Nil()).Block(
		Return(Qual("fmt", "Errorf").Call(Lit("failed to pre-build binaries: %w"), Err())),
	)
	g.Line()
}

// emitWrapHelper writes a `wrap` closure into the AllImages* function
// bodies. It captures workingDir + repo + tag + arch from the surrounding
// scope and adapts each per-component package method (which has a
// uniform 4-string signature) into the func() error shape RunParallel
// expects, so the task list reads as `wrap(build.fooImagePackage)` per
// component instead of an inline closure per entry.
func emitWrapHelper(g *Group, repo, tag Code) {
	g.Id("wrap").Op(":=").Func().Params(
		Id("fn").Func().Params(String(), String(), String(), String()).Error(),
	).Func().Params().Error().Block(
		Return().Func().Params().Error().Block(
			Return().Id("fn").Call(Id("workingDir"), repo, tag, Id("arch")),
		),
	)
}
