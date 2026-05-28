package v0

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestBuildBinaries tests BuildBinaries with a single package dir.
func TestBuildBinaries(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Fatalf("go not installed: %v", err)
	}

	rootDir := t.TempDir()

	err := os.WriteFile(filepath.Join(rootDir, "go.mod"), []byte(`module example.com/test

go 1.22
`), 0644)
	if err != nil {
		t.Fatalf("failed to write go.mod: %v", err)
	}

	err = os.MkdirAll(filepath.Join(rootDir, "cmd", "tptctl"), 0755)
	if err != nil {
		t.Fatalf("failed to create directories: %v", err)
	}

	err = os.WriteFile(filepath.Join(rootDir, "cmd", "tptctl", "main.go"), []byte(`package main

import "fmt"

func main() {
	fmt.Println("hello")
}
`), 0644)
	if err != nil {
		t.Fatalf("failed to write main.go: %v", err)
	}

	err = BuildBinaries(rootDir, []string{"amd64"}, []string{"cmd/tptctl"})
	if err != nil {
		t.Errorf(`BuildBinaries(rootDir, ["amd64"], ["cmd/tptctl"]) failed: %v`, err)
	}

	if _, err := os.Stat(filepath.Join(rootDir, "bin", "amd64", "tptctl")); err != nil {
		t.Errorf("expected binary at bin/amd64/tptctl: %v", err)
	}
}

// TestBuildBinaries_Failure tests that BuildBinaries surfaces a go build
// error when the package dir doesn't exist.
func TestBuildBinaries_Failure(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Fatalf("go not installed: %v", err)
	}

	rootDir := t.TempDir()

	err := os.WriteFile(filepath.Join(rootDir, "go.mod"), []byte(`module example.com/test

go 1.22
`), 0644)
	if err != nil {
		t.Fatalf("failed to write go.mod: %v", err)
	}

	err = BuildBinaries(rootDir, []string{"amd64"}, []string{"cmd/missing"})
	if err == nil {
		t.Errorf(`BuildBinaries(rootDir, ["amd64"], ["cmd/missing"]) expected error, got nil`)
	}
}
