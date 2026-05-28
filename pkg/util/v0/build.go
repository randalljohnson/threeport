package v0

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// BuildBinaries compiles every binary for every arch with one go build
// invocation per arch. Arches run in parallel, and within each arch all
// binaries are passed to a single go build call so dependency compilation
// is shared across components. Each package dir under ./cmd/<name>
// produces bin/<arch>/<name>. CGO is disabled for cross-compile. A
// single-element packageDirs slice is also valid and produces just that
// binary; per-image targets use this form for standalone use, and pick up
// a Go cache hit when AllImages pre-built the same package earlier.
// noCache=true passes -a to force a full rebuild ignoring Go's local
// build cache. debug=true adds -gcflags="all=-N -l" so the binaries
// are debugger-friendly (no optimization, no inlining).
func BuildBinaries(
	threeportPath string,
	arches []string,
	packageDirs []string,
	noCache bool,
	debug bool,
) error {
	tasks := make([]func() error, 0, len(arches))
	for _, a := range arches {
		arch := strings.TrimSpace(a)
		if arch == "" {
			continue
		}
		tasks = append(tasks, func() error {
			return buildArchBinaries(threeportPath, arch, packageDirs, noCache, debug)
		})
	}
	return RunParallel(len(tasks), tasks)
}

// buildArchBinaries runs one go build for the given arch that compiles
// every package dir into bin/<arch>/<name>. Shared dependency compilation
// within the invocation means a cold build is much faster than running
// one go build per binary.
func buildArchBinaries(threeportPath, arch string, packageDirs []string, noCache, debug bool) error {
	outDir := filepath.Join(threeportPath, "bin", arch)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("failed to create output directory %s: %w", outDir, err)
	}

	args := []string{"build", "-buildvcs=false"}
	if noCache {
		args = append(args, "-a")
	}
	if debug {
		args = append(args, `-gcflags=all=-N -l`)
	}
	args = append(args, "-o", filepath.Join("bin", arch)+string(os.PathSeparator))
	// prefix each package dir with ./ so go build treats them as local
	// import paths rather than stdlib lookups.
	for _, dir := range packageDirs {
		if !strings.HasPrefix(dir, "./") && !strings.HasPrefix(dir, "/") {
			dir = "./" + dir
		}
		args = append(args, dir)
	}

	fmt.Printf("go %s\n", strings.Join(args, " "))

	cmd := exec.Command("go", args...)
	cmd.Env = append(os.Environ(),
		"CGO_ENABLED=0",
		"GOOS=linux",
		"GOARCH="+arch,
	)
	cmd.Dir = threeportPath

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to build %s binaries with output '%s': %w", arch, string(output), err)
	}

	return nil
}

// prefixWriter wraps an io.Writer and prefixes each line with the given
// string (e.g. "[rest-api]"). Buffers partial lines so the prefix always
// lands at line start. Line-level atomic on stdout/stderr so parallel
// builds don't tear individual lines.
type prefixWriter struct {
	prefix string
	out    io.Writer
	buf    bytes.Buffer
}

// Write splits incoming bytes into lines and writes each with the prefix.
// Skips lines that are only whitespace so buildx's section-separator blanks
// don't produce naked-prefix noise in the output.
func (p *prefixWriter) Write(data []byte) (int, error) {
	p.buf.Write(data)
	for {
		line, err := p.buf.ReadBytes('\n')
		if err != nil {
			p.buf.Reset()
			p.buf.Write(line)
			return len(data), nil
		}
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if _, err := fmt.Fprintf(p.out, "%s %s", p.prefix, line); err != nil {
			return len(data), err
		}
	}
}

// multiArchBuilderName is the buildx builder created on demand for
// multi-architecture builds. The default `docker` driver does not support
// multi-platform output, so multi-arch builds route through a dedicated
// docker-container builder. Single-arch builds keep using whatever builder
// is currently active.
const multiArchBuilderName = "threeport-multi"

// ensureMultiArchBuilder makes sure a docker-container builder named
// multiArchBuilderName exists. It runs `docker buildx inspect` first; if the
// builder is missing, it creates one with `docker buildx create`. Returns
// nil if the builder is already present or was just created successfully.
func ensureMultiArchBuilder() error {
	inspect := exec.Command("docker", "buildx", "inspect", multiArchBuilderName)
	if err := inspect.Run(); err == nil {
		return nil
	}
	fmt.Printf("creating docker-container buildx builder %q for multi-arch builds...\n", multiArchBuilderName)
	create := exec.Command(
		"docker", "buildx", "create",
		"--name", multiArchBuilderName,
		"--driver", "docker-container",
		"--bootstrap",
	)
	create.Stdout = os.Stdout
	create.Stderr = os.Stderr
	if err := create.Run(); err != nil {
		return fmt.Errorf("docker buildx create failed: %w", err)
	}
	return nil
}

// BuildImage packages a pre-built binary into a container image via docker
// buildx, optionally pushing to a registry or loading into a kind cluster.
// Binary inputs are expected at <threeportPath>/<binDir>/<arch>/<binary>
// for each arch listed in `arch`; the build context is set to the binDir
// root so the Dockerfile's `COPY ${TARGETARCH}/${BINARY}` resolves per
// platform during multi-arch builds. `arch` is a comma-separated list of
// architectures (e.g. "amd64" or "amd64,arm64"); each is prefixed with
// "linux/" to form the buildx --platform value. Multi-arch builds require
// pushImage because buildx cannot --load multiple platforms into a single
// docker daemon. pushImage and loadImage are mutually exclusive. When
// multiple platforms are requested, BuildImage routes the build through a
// dedicated docker-container buildx builder, creating it on first use if
// absent. extraBuildArgs are passed through to buildx for per-target args
// like TERRAFORM_VERSION or PULUMI_VERSION.
func BuildImage(
	threeportPath string,
	dockerfilePath string,
	target string,
	arch string,
	binary string,
	binDir string,
	extraBuildArgs map[string]string,
	imageRepo string,
	imageName string,
	imageTag string,
	pushImage bool,
	loadImage bool,
	loadClusterName string,
) error {
	platforms := []string{}
	for _, a := range strings.Split(arch, ",") {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		platforms = append(platforms, "linux/"+a)
	}
	if len(platforms) == 0 {
		return errors.New("--arch is required")
	}
	if len(platforms) > 1 && !pushImage {
		return fmt.Errorf(
			"multi-arch builds require --push (--load only works for a single platform); got --arch=%s",
			arch,
		)
	}
	if pushImage && loadImage {
		return errors.New("--push and --load are mutually exclusive")
	}

	image := fmt.Sprintf("%s/%s:%s", imageRepo, imageName, imageTag)

	args := []string{"buildx", "build"}
	if len(platforms) > 1 {
		if err := ensureMultiArchBuilder(); err != nil {
			return fmt.Errorf("failed to prepare multi-arch builder: %w", err)
		}
		args = append(args, "--builder", multiArchBuilderName)
	}
	if pushImage {
		args = append(args, "--push")
	} else {
		args = append(args, "--load")
	}
	args = append(args, fmt.Sprintf("--platform=%s", strings.Join(platforms, ",")))
	if target != "" {
		args = append(args, "--target", target)
	}
	args = append(args, "--build-arg", fmt.Sprintf("BINARY=%s", binary))
	// sort extra build-arg keys for deterministic command output
	keys := make([]string, 0, len(extraBuildArgs))
	for k := range extraBuildArgs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "--build-arg", fmt.Sprintf("%s=%s", k, extraBuildArgs[k]))
	}
	// short, prefix-trimmed component name used for both stdout prefixing
	// (see below) and {component} substitution in BUILDX_CACHE_FROM/TO
	// (see immediately following).
	shortName := strings.TrimPrefix(imageName, "threeport-")

	// honor BUILDX_CACHE_FROM/TO for CI cache reuse; opt-in via env so
	// local builds don't pay the registry round trip. {component}
	// substitutes per image, giving each its own cache scope.
	if v := os.Getenv("BUILDX_CACHE_FROM"); v != "" {
		args = append(args, "--cache-from", strings.ReplaceAll(v, "{component}", shortName))
	}
	if v := os.Getenv("BUILDX_CACHE_TO"); v != "" {
		args = append(args, "--cache-to", strings.ReplaceAll(v, "{component}", shortName))
	}
	// plain progress keeps output line-oriented so concurrent builds
	// interleave cleanly when prefixed per component
	args = append(args, "--progress=plain")
	// build context is the per-arch bin root; Dockerfile is referenced
	// from the repo root via -f.
	contextDir := filepath.Join(threeportPath, binDir)
	dockerfile := filepath.Join(threeportPath, dockerfilePath)
	args = append(args, "-t", image, "-f", dockerfile, contextDir)

	// prefix each output line with the short image name so parallel builds
	// are distinguishable in the combined stdout/stderr stream. The
	// "threeport-" prefix is dropped since every component shares it.
	// Arch isn't in the prefix because buildx's own per-step labels
	// (e.g. "[linux/arm64 builder 6/6]") already identify the platform
	// for each line.
	prefix := fmt.Sprintf("[%s]", shortName)
	dockerBuildCmd := exec.Command("docker", args...)
	dockerBuildCmd.Stdout = &prefixWriter{prefix: prefix, out: os.Stdout}
	var stderrBuf bytes.Buffer
	dockerBuildCmd.Stderr = io.MultiWriter(
		&prefixWriter{prefix: prefix, out: os.Stderr},
		&stderrBuf,
	)
	if err := dockerBuildCmd.Run(); err != nil {
		stderr := stderrBuf.String()
		// surface a hint for the most common multi-arch setup gap
		if len(platforms) > 1 && strings.Contains(stderr, "Multi-platform build is not supported for the docker driver") {
			return fmt.Errorf(
				"image build failed for %s: the active buildx builder uses the `docker` driver, which can't do multi-arch builds. Create and switch to a docker-container builder with:\n  docker buildx create --name threeport-multi --driver docker-container --bootstrap --use\nThen retry. Underlying error: %w",
				image, err,
			)
		}
		return fmt.Errorf("image build failed for %s: %w", image, err)
	}

	if pushImage {
		fmt.Printf("%s built and pushed\n", prefix)
	} else {
		fmt.Printf("%s built\n", prefix)
	}

	if loadImage {
		kindLoadCmd := exec.Command(
			"kind",
			"load",
			"docker-image",
			image,
			"--name",
			loadClusterName,
		)
		output, err := kindLoadCmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf(
				"failed to load image %s to kind cluster with output '%s': %w",
				image,
				string(output),
				err,
			)
		}
		fmt.Printf("%s image loaded to kind cluster\n", image)
	}

	return nil
}
