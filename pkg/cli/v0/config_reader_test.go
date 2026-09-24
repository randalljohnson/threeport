package v0

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withStdin swaps os.Stdin for the duration of fn and restores it on return.
func withStdin(t *testing.T, content []byte, fn func()) {
	t.Helper()
	// prepare a pipe as replacement stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	// write and close so ReadAll returns EOF cleanly
	if content != nil {
		if _, err := w.Write(content); err != nil {
			t.Fatalf("failed to write to pipe: %v", err)
		}
	}
	w.Close()

	// swap stdin
	origStdin := os.Stdin
	os.Stdin = r
	defer func() {
		os.Stdin = origStdin
		r.Close()
	}()

	fn()
}

// TestReadConfigContentRejectsBothFlags asserts that providing both
// configPath and useStdin is rejected before any I/O runs.
func TestReadConfigContentRejectsBothFlags(t *testing.T) {
	// call with both flags set
	got, err := ReadConfigContent("/some/path", true)

	// assert error mentions the conflict and no content returned
	if err == nil {
		t.Fatal("expected error when both --config and --stdin are set")
	}
	if got != nil {
		t.Errorf("expected nil content, got %q", got)
	}
	if !strings.Contains(err.Error(), "cannot use both") {
		t.Errorf("expected error mentioning the flag conflict, got: %v", err)
	}
}

// TestReadConfigContentRequiresPathWhenNoStdin asserts that omitting the
// config path without useStdin returns a required-field error.
func TestReadConfigContentRequiresPathWhenNoStdin(t *testing.T) {
	// call with no path and no stdin flag
	got, err := ReadConfigContent("", false)

	// assert required-path error surfaces
	if err == nil {
		t.Fatal("expected error when neither --config nor --stdin is set")
	}
	if got != nil {
		t.Errorf("expected nil content, got %q", got)
	}
	if !strings.Contains(err.Error(), "config path is required") {
		t.Errorf("expected required-path error, got: %v", err)
	}
}

// TestReadConfigContentReadsFile asserts that a valid file path returns the
// file's bytes verbatim.
func TestReadConfigContentReadsFile(t *testing.T) {
	// stage a temp file with known content
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	want := []byte("key: value\n")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	// call under test
	got, err := ReadConfigContent(path, false)

	// assert bytes match and no error
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("content mismatch: want %q, got %q", want, got)
	}
}

// TestReadConfigContentMissingFile asserts that a nonexistent path surfaces
// the underlying os.ReadFile error.
func TestReadConfigContentMissingFile(t *testing.T) {
	// point at a path that does not exist
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.yaml")

	// call under test
	got, err := ReadConfigContent(path, false)

	// assert error surfaced and no content returned
	if err == nil {
		t.Fatal("expected error reading a missing file")
	}
	if got != nil {
		t.Errorf("expected nil content, got %q", got)
	}
}

// TestReadConfigContentReadsStdin asserts that useStdin true reads all
// bytes from os.Stdin.
func TestReadConfigContentReadsStdin(t *testing.T) {
	want := []byte("stdin-payload")

	// replace stdin with a pipe carrying want
	withStdin(t, want, func() {
		got, err := ReadConfigContent("", true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(got) != string(want) {
			t.Errorf("content mismatch: want %q, got %q", want, got)
		}
	})
}

// TestReadConfigContentStdinEmpty asserts that closed/empty stdin returns
// the no-input-received error.
func TestReadConfigContentStdinEmpty(t *testing.T) {
	// replace stdin with an immediately-closed pipe
	withStdin(t, nil, func() {
		got, err := ReadConfigContent("", true)
		if err == nil {
			t.Fatal("expected error when stdin is empty")
		}
		if got != nil {
			t.Errorf("expected nil content, got %q", got)
		}
		if !strings.Contains(err.Error(), "no input received") {
			t.Errorf("expected no-input error, got: %v", err)
		}
	})
}

// TestReadConfigContentFlagConflictBeforeStdinRead asserts that when both
// flags are set, the function returns immediately without consuming stdin.
func TestReadConfigContentFlagConflictBeforeStdinRead(t *testing.T) {
	// stdin holds data that should NOT be consumed
	withStdin(t, []byte("should-not-be-read"), func() {
		got, err := ReadConfigContent("/tmp/whatever", true)
		if err == nil {
			t.Fatal("expected flag-conflict error")
		}
		if got != nil {
			t.Errorf("expected nil content, got %q", got)
		}
		if !strings.Contains(err.Error(), "cannot use both") {
			t.Errorf("expected flag-conflict error, got: %v", err)
		}
	})
}
