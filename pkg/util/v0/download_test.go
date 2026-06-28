package v0

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

// gzippedTar builds an in-memory gzipped tar containing one regular-file entry
// at name holding content.
func gzippedTar(t *testing.T, name string, content []byte) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	gzWriter := gzip.NewWriter(buf)
	tarWriter := tar.NewWriter(gzWriter)

	header := &tar.Header{
		Name:     name,
		Mode:     0o755,
		Size:     int64(len(content)),
		Typeflag: tar.TypeReg,
	}
	if err := tarWriter.WriteHeader(header); err != nil {
		t.Fatalf("WriteHeader error: %v", err)
	}
	if _, err := tarWriter.Write(content); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("tar Close error: %v", err)
	}
	if err := gzWriter.Close(); err != nil {
		t.Fatalf("gzip Close error: %v", err)
	}
	return buf
}

// TestReleaseAssetInfix_MapsOSAndArch asserts releaseAssetInfix reproduces the
// goreleaser archive naming: amd64 to x86_64, arm64 verbatim, 386 to i386, and
// title-cased OS tokens for linux and darwin.
func TestReleaseAssetInfix_MapsOSAndArch(t *testing.T) {
	// each case pairs a GOOS/GOARCH input with the expected goreleaser infix
	cases := []struct {
		goos   string
		goarch string
		want   string
	}{
		{"linux", "amd64", "Linux_x86_64"},   // amd64 maps to x86_64
		{"linux", "arm64", "Linux_arm64"},    // arm64 passes through verbatim
		{"linux", "386", "Linux_i386"},       // 386 maps to i386
		{"darwin", "amd64", "Darwin_x86_64"}, // darwin title-cases to Darwin
		{"darwin", "arm64", "Darwin_arm64"},  // darwin with arm64 passthrough
	}

	// assert each input produces the goreleaser infix
	for _, c := range cases {
		got := releaseAssetInfix(c.goos, c.goarch)
		if got != c.want {
			t.Errorf("releaseAssetInfix(%q, %q)=%q, want %q", c.goos, c.goarch, got, c.want)
		}
	}
}

// TestExtractBinary_InstallsExecutableFromVersionedDir asserts extractBinary
// strips the versioned top-level directory, writes the binary executable, and
// preserves its contents.
func TestExtractBinary_InstallsExecutableFromVersionedDir(t *testing.T) {
	// build an archive whose binary lives under a versioned top-level directory
	want := []byte("threeport-sdk binary bytes")
	archive := gzippedTar(t, "v1.2.3-dist/threeport-sdk", want)
	destDir := t.TempDir()

	// extract the binary by base name
	if err := extractBinary(archive, "threeport-sdk", destDir); err != nil {
		t.Fatalf("extractBinary error: %v", err)
	}

	// assert the binary lands at destDir/threeport-sdk
	destPath := filepath.Join(destDir, "threeport-sdk")
	info, err := os.Stat(destPath)
	if err != nil {
		t.Fatalf("Stat error: %v", err)
	}

	// assert the installed binary is executable
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode=%o, want 0755", info.Mode().Perm())
	}

	// assert the installed binary matches the archive contents
	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("contents=%q, want %q", got, want)
	}
}

// TestExtractBinary_RejectsArchiveWithoutMatch asserts extractBinary returns an
// error when no tar entry matches the requested binary name.
func TestExtractBinary_RejectsArchiveWithoutMatch(t *testing.T) {
	// build an archive holding a different binary than the one requested
	archive := gzippedTar(t, "v1.2.3-dist/other-binary", []byte("nope"))
	destDir := t.TempDir()

	// extract the missing binary and assert an error is returned
	if err := extractBinary(archive, "threeport-sdk", destDir); err == nil {
		t.Fatalf("expected error for missing binary, got nil")
	}
}
