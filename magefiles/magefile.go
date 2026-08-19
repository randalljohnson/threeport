package main

import (
	"fmt"
	"go/build"
	"os"
	"os/exec"
	"path/filepath"
	"slices"

	version "github.com/threeport/threeport/internal/version"
	cli "github.com/threeport/threeport/pkg/cli/v0"
	installer "github.com/threeport/threeport/pkg/threeport-installer/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// Cockroach runs the tests that need a real CockroachDB, which it starts in a
// container itself.
//
// The unit tests run on sqlite and the api tests run gorm in dry-run mode, so
// anything decided by how CockroachDB answers is out of reach of both: dry run
// builds a statement without reading a result back, and sqlite rejects the
// grammar. The prerequisite here is a container rather than a control plane,
// which is why this sits outside both test:unit and test:integration.
//
// The suite skips itself when docker is missing.
func (Test) Cockroach() error {
	cmd := "go"
	args := []string{
		"test",
		"-v",
		"-count=1",
		"./test/cockroach",
	}
	if err := util.RunCommandStreamOutput(cmd, args...); err != nil {
		return fmt.Errorf("failed to run cockroach tests: %w", err)
	}

	return nil
}

// E2e calls ginkgo to run the e2e tests suite.  Takes 2 args: 1. imageRepo -
// either 'local' or the URL for an external image repo.  2. clean - if true
// will remove the control plane and infra after completion.
func (Test) E2e(
	imageRepo string,
	clean bool,
) error {
	// determine path to root of Threeport repo
	threeportPath, err := os.Getwd() // mage must be run from repo root
	if err != nil {
		return fmt.Errorf("failed to get path to Threeport repo: %w", err)
	}

	cmd := "ginkgo"
	args := []string{
		"test/e2e",
		"--",
		"-provider=kind",
		fmt.Sprintf("-image-repo=%s", imageRepo),
		fmt.Sprintf("-threeport-path=%s", threeportPath),
		fmt.Sprintf("-clean=%t", clean),
	}
	if err := util.RunCommandStreamOutput(cmd, args...); err != nil {
		return fmt.Errorf("failed to run e2e tests: %w", err)
	}

	return nil
}

// E2eLocal is a wrapper for e2e that uses kind, a local image repo in a docker
// container and cleans up at completion.
func (Test) E2eLocal() error {
	test := Test{}
	return test.E2e("local", true)
}

// E2eClean removes the kind cluster and local container registry for e2e
// testing.
func (Test) E2eClean() error {
	cmd := "kind"
	args := []string{
		"delete",
		"cluster",
		"-n",
		"threeport-e2e-test",
	}
	if err := util.RunCommandStreamOutput(cmd, args...); err != nil {
		return fmt.Errorf("failed to remove e2e test cluster: %w", err)
	}

	dev := Dev{}
	if err := dev.LocalRegistryDown(); err != nil {
		return err
	}

	return nil
}

// installDir returns the directory `go install` writes binaries to:
// $GOBIN if set, otherwise $GOPATH/bin. build.Default.GOPATH falls back
// to ~/go when $GOPATH is unset, so the result is always non-empty.
func installDir() string {
	if gobin := os.Getenv("GOBIN"); gobin != "" {
		return gobin
	}
	return filepath.Join(build.Default.GOPATH, "bin")
}

// Sdk builds the threeport-sdk binary.
func (Build) Sdk() error {
	buildSdkCmd := exec.Command(
		"go",
		"build",
		"-o",
		"bin/threeport-sdk",
		"cmd/sdk/main.go",
	)
	output, err := buildSdkCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build failed for threeport-sdk binary with output: '%s': %w", output, err)
	}

	fmt.Println("threeport-sdk binary built and available at bin/threeport-sdk")

	return nil
}

// Sdk builds the threeport-sdk binary and installs in $GOBIN (or $GOPATH/bin).
func (Install) Sdk() error {
	build := Build{}
	if err := build.Sdk(); err != nil {
		return fmt.Errorf("failed to build threeport-sdk: %w", err)
	}

	outputPath := filepath.Join(installDir(), "threeport-sdk")

	installSdkCmd := exec.Command(
		"cp",
		"./bin/threeport-sdk",
		outputPath,
	)
	output, err := installSdkCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("install failed for threeport-sdk binary with output: '%s': %w", output, err)
	}

	fmt.Printf("threeport-sdk binary installed and available at %s\n", outputPath)

	return nil
}

// Integration runs integration tests against an existing Threeport control
// plane.
func (Test) Integration() error {
	cmd := "go"
	args := []string{
		"test",
		"-v",
		"./test/integration",
		"-count=1",
	}
	if err := util.RunCommandStreamOutput(cmd, args...); err != nil {
		return fmt.Errorf("failed to run integration tests: %w", err)
	}

	return nil
}

// Tptdev builds tptdev binary.
func (Build) Tptdev() error {
	buildTptdevCmd := exec.Command(
		"go",
		"build",
		"-o",
		"bin/tptdev",
		"cmd/tptdev/main.go",
	)
	output, err := buildTptdevCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build failed for tptdev with output: '%s': %w", output, err)
	}

	fmt.Println("tptdev built and available at bin/tptdev")

	return nil
}

// Tptdev builds the tptdev binary and installs in $GOBIN (or $GOPATH/bin).
func (Install) Tptdev() error {
	build := Build{}
	if err := build.Tptdev(); err != nil {
		return fmt.Errorf("failed to build tptdev: %w", err)
	}

	outputPath := filepath.Join(installDir(), "tptdev")

	installTptdevCmd := exec.Command(
		"cp",
		"./bin/tptdev",
		outputPath,
	)
	output, err := installTptdevCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("install failed for tptdev with output: '%s': %w", output, err)
	}

	fmt.Printf("tptdev installed and available at %s\n", outputPath)

	return nil
}

// Tptctl builds tptctl binary.
func (Build) Tptctl() error {
	buildTptctlCmd := exec.Command(
		"go",
		"build",
		"-o",
		"bin/tptctl",
		"cmd/tptctl/main.go",
	)
	output, err := buildTptctlCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build failed for tptctl binary with output: '%s': %w", output, err)
	}

	fmt.Println("tptctl binary built and available at bin/tptctl")

	return nil
}

// Tptctl builds the tptctl binary and installs in $GOBIN (or $GOPATH/bin).
func (Install) Tptctl() error {
	build := Build{}
	if err := build.Tptctl(); err != nil {
		return fmt.Errorf("failed to build tptctl: %w", err)
	}

	outputPath := filepath.Join(installDir(), "tptctl")

	installTptctlCmd := exec.Command(
		"cp",
		"./bin/tptctl",
		outputPath,
	)
	output, err := installTptctlCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("install failed for tptctl binary with output: '%s': %w", output, err)
	}

	fmt.Printf("tptctl binary installed and available at %s\n", outputPath)

	return nil
}

// Generate runs runs threeport-sdk code generation and generates API
// swagger docs.
func (Dev) Generate() error {
	dev := Dev{}
	err := dev.GenerateCode()
	if err != nil {
		return fmt.Errorf("code generation failed: %w", err)
	}

	err = dev.GenerateDocs()
	if err != nil {
		return fmt.Errorf("docs generation failed: %w", err)
	}

	fmt.Println("code generated successfully")

	return nil
}

// GenerateCode generates code with threeport-sdk.
func (Dev) GenerateCode() error {
	generateCode := exec.Command(
		"threeport-sdk",
		"gen",
		"-c",
		"sdk-config.yaml",
	)
	output, err := generateCode.CombinedOutput()
	if err != nil {
		return fmt.Errorf("code generation failed with output: '%s': %w", output, err)
	}

	fmt.Println("code generated successfully")

	return nil
}

// GenerateDocs generates the swagger docs served by the API.
func (Dev) GenerateDocs() error {
	docsDestination := "pkg/api-server/v0/docs"
	generateSwaggerDocs := exec.Command(
		"swag",
		"init",
		"--dir",
		"cmd/rest-api,pkg/api-server/v0,pkg/api-server/v0",
		"--parseDependency",
		"--propertyStrategy",
		"pascalcase",
		"--generalInfo",
		"main_gen.go",
		"--output",
		docsDestination,
	)

	output, err := generateSwaggerDocs.CombinedOutput()
	if err != nil {
		return fmt.Errorf("swagger docs generation failed with output: '%s': %w", output, err)
	}

	fmt.Printf("API swagger docs generated successfully in %s\n", docsDestination)

	return nil
}

// moduleTestPath is where a Threeport module gets generated for testing.  It
// is two directories deep so the generated go.mod can reach the repository
// root with a relative replace directive.
const moduleTestPath = "test/module"

// moduleTestGoPath is the module path for the generated go.mod.  The SDK reads
// this path to decide whether to generate module code, so it only has to
// differ from this project's own path.
const moduleTestGoPath = "github.com/threeport/threeport-module-test"

// moduleTestBinary is the generated module's command-line binary.  The SDK
// names it after ModuleName in the SDK config.
const moduleTestBinary = "test"

// moduleTestInputs are the files tracked under moduleTestPath.  A run deletes
// everything else first, which forces the SDK to re-emit the scaffolding it
// otherwise writes only once.
var moduleTestInputs = []string{"sdk-config.yaml", "README.md"}

// ModuleGen generates a Threeport module and type-checks it, test files
// included.
//
// The SDK emits different code for a module than for this repository, and
// nothing else here type-checks that code.  It calls this repository's exported
// API by name, so a changed signature breaks it.  Without this target the
// break surfaces later, in a module repository, far from the change.
func (Test) ModuleGen() error {
	if err := generateModuleTest(); err != nil {
		return err
	}

	// go vet type-checks the generated test files, which call this
	// repository's exported API and so break on a changed signature, and it
	// accepts the magefiles package, whose main function mage supplies at run
	// time
	if err := util.RunCommandStreamOutputInDir(
		moduleTestPath,
		"go", "vet", "./...",
	); err != nil {
		return fmt.Errorf("failed to type-check the generated module: %w", err)
	}

	// a failed run returns above without this, leaving the generated source
	// to read
	if err := resetModuleTestDir(); err != nil {
		return err
	}

	fmt.Println("module generated and type-checked successfully")

	return nil
}

// ModuleInstall generates a Threeport module, builds its images and installs
// it into the control plane named by the local Threeport config.  It covers
// what compiling cannot: the migrations apply, the module registers with the
// control plane, and its routes answer through the control plane's proxy.
//
// It needs a running control plane and a registry the cluster can pull from,
// so it belongs to the integration tests rather than the unit tests.
func (Test) ModuleInstall() error {
	if err := generateModuleTest(); err != nil {
		return err
	}

	// the install deploys images by tag, so push them first
	if err := util.RunCommandStreamOutputInDir(
		moduleTestPath,
		"mage", "build:allImagesDev",
	); err != nil {
		return fmt.Errorf("failed to build the module images: %w", err)
	}

	// a module reaches a control plane as a tptctl plugin, so put the binary
	// where tptctl looks for one rather than running it in place.  Installing
	// it covers the plugin discovery and dispatch this repository owns
	if err := util.RunCommandStreamOutputInDir(
		moduleTestPath,
		"mage", "install:plugin",
	); err != nil {
		return fmt.Errorf("failed to install the module plugin: %w", err)
	}

	install := Install{}
	if err := install.Tptctl(); err != nil {
		return fmt.Errorf("failed to install tptctl: %w", err)
	}

	// --debug re-pulls the tag on every rollout, so a rebuilt image at the
	// same tag is the one that runs.  The image namespace defaults to the one
	// in the SDK config, which for this module is the local registry
	if err := util.RunCommandStreamOutput(
		filepath.Join(installDir(), "tptctl"),
		moduleTestBinary, "install", "--debug",
	); err != nil {
		return fmt.Errorf("failed to install the module: %w", err)
	}

	if err := removeModuleTestPlugin(); err != nil {
		return err
	}
	if err := resetModuleTestDir(); err != nil {
		return err
	}

	fmt.Println("module generated, installed and registered successfully")

	return nil
}

// removeModuleTestPlugin deletes the test module's plugin from the tptctl
// plugin directory.  A plugin left behind shows up under every later tptctl
// invocation on the machine, which a test has no business doing.
func removeModuleTestPlugin() error {
	pluginDir := os.Getenv("THREEPORT_PLUGIN_DIR")
	if pluginDir == "" {
		dir, err := cli.DefaultPluginDir()
		if err != nil {
			return fmt.Errorf("failed to determine tptctl plugin directory: %w", err)
		}
		pluginDir = dir
	}

	pluginPath := filepath.Join(pluginDir, moduleTestBinary)
	if err := os.Remove(pluginPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove the module test plugin %s: %w", pluginPath, err)
	}

	return nil
}

// generateModuleTest generates a module under moduleTestPath from the tracked
// SDK config.  It does not compile the result, so callers choose what to do
// with it.
func generateModuleTest() error {
	// rebuild threeport-sdk from this checkout first.  It installs to one
	// global path, so the binary on PATH may have come from another worktree
	// and would generate code this repository never asked for.
	install := Install{}
	if err := install.Sdk(); err != nil {
		return fmt.Errorf("failed to install threeport-sdk: %w", err)
	}

	if err := resetModuleTestDir(); err != nil {
		return err
	}

	// the module path selects the SDK's module code path; the replace
	// directive points the generated code at this checkout
	if err := util.RunCommandStreamOutputInDir(
		moduleTestPath,
		"go", "mod", "init", moduleTestGoPath,
	); err != nil {
		return fmt.Errorf("failed to initialize the module test go.mod: %w", err)
	}
	if err := util.RunCommandStreamOutputInDir(
		moduleTestPath,
		"go", "mod", "edit",
		"-require=github.com/threeport/threeport@v0.0.0",
		"-replace=github.com/threeport/threeport=../..",
	); err != nil {
		return fmt.Errorf("failed to point the module test at this checkout: %w", err)
	}

	// scaffold the API object source, then generate the boilerplate from it
	sdkBinary := filepath.Join(installDir(), "threeport-sdk")
	for _, subcommand := range []string{"create", "gen"} {
		if err := util.RunCommandStreamOutputInDir(
			moduleTestPath,
			sdkBinary,
			subcommand,
			"-c",
			"sdk-config.yaml",
		); err != nil {
			return fmt.Errorf("failed to run threeport-sdk %s for the module test: %w", subcommand, err)
		}
	}

	// the generated imports are only known now, so resolve them here
	if err := util.RunCommandStreamOutputInDir(
		moduleTestPath,
		"go", "mod", "tidy",
	); err != nil {
		return fmt.Errorf("failed to resolve the module test dependencies: %w", err)
	}

	return nil
}

// resetModuleTestDir removes everything the last run generated, leaving the
// tracked inputs in place.
func resetModuleTestDir() error {
	entries, err := os.ReadDir(moduleTestPath)
	if err != nil {
		return fmt.Errorf("failed to read module test directory %s: %w", moduleTestPath, err)
	}

	for _, entry := range entries {
		if slices.Contains(moduleTestInputs, entry.Name()) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(moduleTestPath, entry.Name())); err != nil {
			return fmt.Errorf("failed to remove generated path %s: %w", entry.Name(), err)
		}
	}

	return nil
}

// Commits checks to make sure commit messages follow conventional commits
// format.
func (Test) Commits() error {
	testCommits := exec.Command(
		"test/scripts/commit-check-latest.sh",
	)

	output, err := testCommits.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run commit check: '%s': %w", output, err)
	}

	fmt.Println("commit check ran successfully")

	return nil
}

// Up spins up a control plane using tptctl and a local registry for testing.
func (Test) Up() error {
	testUp := exec.Command(
		"./bin/tptctl",
		"up",
		"-r",
		installer.DevImageNamespace,
		"-t",
		version.GetVersion(),
		"-n",
		"dev-0",
		"--local-registry",
	)

	output, err := testUp.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create test control plane: '%s': %w", output, err)
	}

	fmt.Println("local test control plane created")

	return nil
}

// Up spins up a local development environment.
func (Dev) Up() error {
	devUp := exec.Command(
		"./bin/tptdev",
		"up",
		"--auth-enabled=false",
	)

	output, err := devUp.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create local dev environment: '%s': %w", output, err)
	}

	fmt.Println("local dev environment ran successfully")

	return nil
}

// Down removes the local development environment.
func (Dev) Down() error {
	devDown := exec.Command(
		"./bin/tptdev",
		"down",
	)
	output, err := devDown.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to delete local dev environment: '%s': %w", output, err)
	}

	fmt.Println("local dev environment successfully deleted")

	return nil
}

// ForwardApi forwards local port 1323 to the local dev API.
func (Dev) ForwardApi() error {
	devforwardAPI := exec.Command(
		"kubectl",
		"port-forward",
		"-n",
		"threeport-control-plane",
		"service/threeport-api-server",
		"1323:80",
	)
	output, err := devforwardAPI.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to forward local port 1323 to local dev API: '%s': %w", output, err)
	}

	fmt.Println("local port 1323 forwarded to local dev API successfully")

	return nil
}

// ForwardCrdb forwards local port 26257 to local dev cockroach database.
func (Dev) ForwardCrdb() error {
	devforwardCrdb := exec.Command(
		"kubectl",
		"port-forward",
		"-n",
		"threeport-control-plane",
		"service/crdb",
		"26257",
	)
	output, err := devforwardCrdb.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to forward local port 26257 to local dev cockroach database: '%s': %w", output, err)
	}

	fmt.Println("local port 26257 forwarded to local dev API successfully")

	return nil
}

// ForwardNats forwards local port 33993 to the local dev API nats server.
func (Dev) ForwardNats() error {
	devforwardNats := exec.Command(
		"kubectl",
		"port-forward",
		"-n",
		"threeport-control-plane",
		"service/nats-js",
		"4222:4222",
	)
	output, err := devforwardNats.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to forward local port 33993 to local dev API nats server: '%s': %w", output, err)
	}

	fmt.Println("local port 33993 forwarded to local dev API nats server successfully")

	return nil
}

// ServeDocs serves the Threeport documentation locally.
func (Dev) ServeDocs() error {
	workingDir, _, err := getBuildVals()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	cmd := "mkdocs"
	args := []string{
		"serve",
		"--config-file",
		fmt.Sprintf("%s/docs/mkdocs.yml", workingDir),
	}
	if err := util.RunCommandStreamOutput(cmd, args...); err != nil {
		return fmt.Errorf("failed to serve docs locally: %w", err)
	}

	return nil
}
