package v0

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestBuildBinary tests the BuildBinary function with a valid input.
func TestBuildBinary(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Fatalf("go not installed: %v", err)
	}

	// Create a real temporary Go project.
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

	// Call BuildBinary with valid inputs.
	err = BuildBinary(rootDir, "amd64", "test-binary", "cmd/tptctl/main.go", false)
	if err != nil {
		t.Errorf(`BuildBinary(rootDir, "amd64", "test-binary", "cmd/tptctl/main.go", false) failed: %v`, err)
	}
}

// TestBuildBinary_Failure tests the BuildBinary function with a failing command.
func TestBuildBinary_Failure(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Fatalf("go not installed: %v", err)
	}

	// Create a real temporary Go project.
	rootDir := t.TempDir()

	err := os.WriteFile(filepath.Join(rootDir, "go.mod"), []byte(`module example.com/test

go 1.22
`), 0644)
	if err != nil {
		t.Fatalf("failed to write go.mod: %v", err)
	}

	// Call BuildBinary with an invalid main.go path and expect an error.
	err = BuildBinary(rootDir, "amd64", "test-binary", "cmd/main.go", false)
	if err == nil {
		t.Errorf(`BuildBinary(rootDir, "amd64", "test-binary", "cmd/main.go", false) expected error, got nil`)
	}
}

