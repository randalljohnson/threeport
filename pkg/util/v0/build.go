package v0

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// BuildBinary builds the go binary for a threeport control plane component.
func BuildBinary(
	threeportPath string,
	arch string,
	binName string,
	mainPath string,
	noCache bool,
) error {
	// construct build arguments
	buildArgs := []string{"build"}

	// append build flags
	buildArgs = append(buildArgs, "-gcflags=\\\"all=-N -l\\\"") // escape quotes and escape char for shell

	// append no cache flag if specified
	if noCache {
		buildArgs = append(buildArgs, "-a")
	}

	// append output flag
	buildArgs = append(buildArgs, "-o")

	// append binary name
	buildArgs = append(buildArgs, "bin/"+binName)

	// append main.go filepath
	buildArgs = append(buildArgs, mainPath)

	fmt.Printf("go %s \n", strings.Join(buildArgs, " "))

	// construct build command
	cmd := exec.Command("go", buildArgs...)
	cmd.Env = os.Environ()
	goEnv := []string{
		"CGO_ENABLED=0",
		"GOOS=linux",
		"GOARCH=" + arch,
	}
	cmd.Env = append(cmd.Env, goEnv...)
	cmd.Dir = threeportPath

	// start build command
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to build %s with output '%s': %w", binName, string(output), err)
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

// BuildImage builds a container image via docker buildx, optionally pushing
// to a registry or loading into a kind cluster. Arch is a comma-separated
// list of architectures (e.g. "amd64" or "amd64,arm64"); each is prefixed
// with "linux/" to form the buildx --platform value. Multi-arch builds
// require pushImage because buildx cannot --load multiple platforms into a
// single docker daemon. pushImage and loadImage are mutually exclusive.
// When multiple platforms are requested, BuildImage routes the build
// through a dedicated docker-container buildx builder, creating it on
// first use if absent.
func BuildImage(
	threeportPath string,
	dockerfilePath string,
	target string,
	arch string,
	buildArgs map[string]string,
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
	// sort build-arg keys for deterministic command output
	keys := make([]string, 0, len(buildArgs))
	for k := range buildArgs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "--build-arg", fmt.Sprintf("%s=%s", k, buildArgs[k]))
	}
	// honor BUILDX_CACHE_FROM / BUILDX_CACHE_TO for CI cache reuse; opt-in
	// via env so local builds (with hot in-builder BuildKit cache) don't pay
	// the registry-or-gha round trip.
	if v := os.Getenv("BUILDX_CACHE_FROM"); v != "" {
		args = append(args, "--cache-from", v)
	}
	if v := os.Getenv("BUILDX_CACHE_TO"); v != "" {
		args = append(args, "--cache-to", v)
	}
	// plain progress keeps output line-oriented so concurrent builds
	// interleave cleanly when prefixed per component
	args = append(args, "--progress=plain")
	args = append(args, "-t", image, "-f", dockerfilePath, threeportPath)

	// prefix each output line with the short image name so parallel builds
	// are distinguishable in the combined stdout/stderr stream. The
	// "threeport-" prefix is dropped since every component shares it.
	// Arch isn't in the prefix because buildx's own per-step labels
	// (e.g. "[linux/arm64 builder 6/6]") already identify the platform
	// for each line.
	shortName := strings.TrimPrefix(imageName, "threeport-")
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
		fmt.Printf("%s ✓ built and pushed\n", prefix)
	} else {
		fmt.Printf("%s ✓ built\n", prefix)
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
