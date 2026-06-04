package v0

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrefixWriter_CompleteLineEmittedDuringWrite(t *testing.T) {
	var out bytes.Buffer
	w := &prefixWriter{prefix: "[svc]", out: &out}

	n, err := w.Write([]byte("done\n"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len("done\n") {
		t.Errorf("Write returned %d, want %d", n, len("done\n"))
	}
	if got, want := out.String(), "[svc] done\n"; got != want {
		t.Errorf("Write output = %q, want %q", got, want)
	}

	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if got, want := out.String(), "[svc] done\n"; got != want {
		t.Errorf("Flush should be no-op, got %q, want %q", got, want)
	}
}

func TestPrefixWriter_PartialLineFlushed(t *testing.T) {
	var out bytes.Buffer
	w := &prefixWriter{prefix: "[svc]", out: &out}

	if _, err := w.Write([]byte("exporting layers")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("partial line should be buffered, got %q", out.String())
	}

	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if got, want := out.String(), "[svc] exporting layers\n"; got != want {
		t.Errorf("Flush output = %q, want %q", got, want)
	}
}

func TestPrefixWriter_FlushEmptyBufferNoop(t *testing.T) {
	var out bytes.Buffer
	w := &prefixWriter{prefix: "[svc]", out: &out}

	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("Flush on empty buffer should write nothing, got %q", out.String())
	}
}

func TestPrefixWriter_WhitespaceOnlyLineSkipped(t *testing.T) {
	var out bytes.Buffer
	w := &prefixWriter{prefix: "[svc]", out: &out}

	if _, err := w.Write([]byte("   \n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("whitespace-only line should be skipped, got %q", out.String())
	}
}

func TestPrefixWriter_PartialThenCompleteAcrossWrites(t *testing.T) {
	var out bytes.Buffer
	w := &prefixWriter{prefix: "[svc]", out: &out}

	if _, err := w.Write([]byte("expo")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("partial line should be buffered, got %q", out.String())
	}
	if _, err := w.Write([]byte("rting layers\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got, want := out.String(), "[svc] exporting layers\n"; got != want {
		t.Errorf("output across writes = %q, want %q", got, want)
	}
}

func TestPrefixWriter_MultipleCompleteLines(t *testing.T) {
	var out bytes.Buffer
	w := &prefixWriter{prefix: "[svc]", out: &out}

	if _, err := w.Write([]byte("a\nb\nc\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	want := []string{"[svc] a", "[svc] b", "[svc] c"}
	if len(got) != len(want) {
		t.Fatalf("line count = %d, want %d (output %q)", len(got), len(want), out.String())
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestPrefixWriter_CompleteLinesThenPartialFlushed(t *testing.T) {
	var out bytes.Buffer
	w := &prefixWriter{prefix: "[svc]", out: &out}

	if _, err := w.Write([]byte("first\nsecond\nthird-no-newline")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got, want := out.String(), "[svc] first\n[svc] second\n"; got != want {
		t.Errorf("complete-line output = %q, want %q", got, want)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	want := "[svc] first\n[svc] second\n[svc] third-no-newline\n"
	if got := out.String(); got != want {
		t.Errorf("output after Flush = %q, want %q", got, want)
	}
}
