package v0

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// TestWriteCIEnvAlwaysPrintsGoreleaserParallelism covers the fork
// ci:env path, which passes an empty moduleVersion.
func TestWriteCIEnvAlwaysPrintsGoreleaserParallelism(t *testing.T) {
	// capture stdout around WriteCIEnv
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	writeErr := WriteCIEnv("")
	w.Close()
	os.Stdout = old

	// restore stdout then read the captured env lines
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if writeErr != nil {
		t.Fatalf("WriteCIEnv(\"\") = %v", writeErr)
	}

	// require GOFLAGS and GORELEASER_PARALLELISM on the empty-module path
	out := buf.String()
	if !strings.Contains(out, "GOFLAGS=-p=") {
		t.Errorf("WriteCIEnv stdout missing GOFLAGS, got %q", out)
	}
	if !strings.Contains(out, "GORELEASER_PARALLELISM=") {
		t.Errorf("WriteCIEnv stdout missing GORELEASER_PARALLELISM, got %q", out)
	}
}

// TestWriteCIEnvEmitsThreeportPinFromGoMod covers module CI, which reads
// the versioned threeport replace and must print image namespace and tag
// for tptctl up.
func TestWriteCIEnvEmitsThreeportPinFromGoMod(t *testing.T) {
	dir := t.TempDir()
	gomod := "module example.com/mod\n\ngo 1.27\n\nreplace github.com/threeport/threeport => github.com/randalljohnson/threeport v0.7.0-dev.28\n"
	if err := os.WriteFile(dir+"/go.mod", []byte(gomod), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	t.Chdir(dir)

	// capture stdout around WriteCIEnv
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	writeErr := WriteCIEnv("")
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if writeErr != nil {
		t.Fatalf("WriteCIEnv(\"\") = %v", writeErr)
	}

	// require repo, tag, and ghcr owner namespace from the replace
	out := buf.String()
	for _, want := range []string{
		"THREEPORT_REPO=randalljohnson/threeport",
		"THREEPORT_IMAGE_TAG=v0.7.0-dev.28",
		"THREEPORT_IMAGE_NAMESPACE=ghcr.io/randalljohnson",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("WriteCIEnv stdout missing %q, got %q", want, out)
		}
	}
}
