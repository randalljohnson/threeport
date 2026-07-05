package terraform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	logr "github.com/go-logr/logr"

	v0 "github.com/threeport/threeport/pkg/api/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// failingCredentialsProvider is an aws.CredentialsProvider whose Retrieve
// always errors: it lets tests exercise the wrap-error branch that runs when
// the AWS SDK cannot supply credentials.
type failingCredentialsProvider struct{}

// Retrieve reports the configured failure so the caller's wrap branch fires.
func (failingCredentialsProvider) Retrieve(ctx context.Context) (aws.Credentials, error) {
	return aws.Credentials{}, errors.New("boom")
}

// newTestLogger returns a discard-backed logger that satisfies the *logr.Logger
// signature used by the terraform reconciliation entry points.
func newTestLogger() *logr.Logger {
	l := logr.Discard()
	return &l
}

// TestGetTerraformInstanceOperationsBuildsSingleNamedOperation asserts the
// helper stack contains exactly one operation, named "terraformInstance",
// with non-nil Create and Delete callbacks bound to the receiver.
func TestGetTerraformInstanceOperationsBuildsSingleNamedOperation(t *testing.T) {
	// build a minimal config; the operation stack does not read any fields at build time
	c := &TerraformInstanceConfig{}

	// invoke the helper under test
	ops := c.getTerraformInstanceOperations()

	// verify the stack holds exactly one operation
	if ops == nil {
		t.Fatalf("expected non-nil operations, got nil")
	}
	if len(ops.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(ops.Operations))
	}

	// verify the single operation carries the expected name and both callbacks
	op := ops.Operations[0]
	if op.Name != "terraformInstance" {
		t.Errorf("operation name = %q, want %q", op.Name, "terraformInstance")
	}
	if op.Create == nil {
		t.Errorf("expected non-nil Create callback")
	}
	if op.Delete == nil {
		t.Errorf("expected non-nil Delete callback")
	}
}

// TestDeleteTerraformInstanceReturnsNilWhenNoVarsOrState covers the delete
// path taken when both the vars and state documents are nil: the helper
// only needs to ensure the config directory exists and then returns cleanly.
func TestDeleteTerraformInstanceReturnsNilWhenNoVarsOrState(t *testing.T) {
	// use a temp dir the helper can stat successfully
	tfDir := t.TempDir()
	c := &TerraformInstanceConfig{
		terraformInstance: &v0.TerraformInstance{},
		log:               newTestLogger(),
		tfDirName:         tfDir,
	}

	// invoke delete; with no vars, no state, and an existing dir, the helper
	// short-circuits after the Stat call
	if err := c.deleteTerraformInstance(); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

// TestDeleteTerraformInstanceCreatesMissingDir asserts the delete helper
// materialises the config directory when it does not yet exist, then returns
// cleanly because there is no state document to consume.
func TestDeleteTerraformInstanceCreatesMissingDir(t *testing.T) {
	// point tfDirName at a not-yet-existing subdirectory under a temp root
	root := t.TempDir()
	tfDir := filepath.Join(root, "new-config")
	c := &TerraformInstanceConfig{
		terraformInstance: &v0.TerraformInstance{},
		log:               newTestLogger(),
		tfDirName:         tfDir,
	}

	// invoke delete; the helper should Mkdir the missing directory
	if err := c.deleteTerraformInstance(); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	// verify the directory now exists on disk
	if _, err := os.Stat(tfDir); err != nil {
		t.Errorf("expected mkdir to create %q, stat err %v", tfDir, err)
	}
}

// TestDeleteTerraformInstanceWrapsVarsWriteError covers the branch where a
// vars document is present but the target directory is invalid: the helper
// must return the "failed to write terraform vars to file" wrap prefix.
func TestDeleteTerraformInstanceWrapsVarsWriteError(t *testing.T) {
	// invalid path forces WriteFile to fail before any other work runs
	c := &TerraformInstanceConfig{
		terraformInstance: &v0.TerraformInstance{
			VarsDocument: util.Ptr("region = \"us-west-2\""),
		},
		log:       newTestLogger(),
		tfDirName: "/proc/does-not-exist",
	}

	// invoke delete and expect the wrap-prefixed error
	err := c.deleteTerraformInstance()
	if err == nil {
		t.Fatalf("expected error from vars WriteFile failure")
	}
	if !strings.Contains(err.Error(), "failed to write terraform vars to file") {
		t.Errorf("error %q missing expected prefix", err.Error())
	}
}

// TestDeleteTerraformInstanceWrapsMkdirError covers the branch where the
// config directory is missing and cannot be created: the helper must return
// the "failed to create directory for terraform config" wrap prefix.
func TestDeleteTerraformInstanceWrapsMkdirError(t *testing.T) {
	// path under /proc is unwritable so Mkdir fails
	c := &TerraformInstanceConfig{
		terraformInstance: &v0.TerraformInstance{},
		log:               newTestLogger(),
		tfDirName:         "/proc/does-not-exist",
	}

	// invoke delete and expect the wrap-prefixed error
	err := c.deleteTerraformInstance()
	if err == nil {
		t.Fatalf("expected error from Mkdir failure")
	}
	if !strings.Contains(err.Error(), "failed to create directory for terraform config") {
		t.Errorf("error %q missing expected prefix", err.Error())
	}
}

// TestDeleteTerraformInstanceWrapsCredentialsError covers the branch where a
// state document is present: the helper writes state to disk, retrieves AWS
// credentials, and surfaces the "failed to retrieve AWS credentials" wrap
// prefix when the credentials provider errors.
func TestDeleteTerraformInstanceWrapsCredentialsError(t *testing.T) {
	// existing dir so the state write succeeds before creds are retrieved
	tfDir := t.TempDir()
	c := &TerraformInstanceConfig{
		terraformInstance: &v0.TerraformInstance{
			StateDocument: util.Ptr("{\"version\":4}"),
		},
		log:       newTestLogger(),
		tfDirName: tfDir,
		awsConfig: &aws.Config{Credentials: failingCredentialsProvider{}},
	}

	// invoke delete and expect the credentials wrap prefix
	err := c.deleteTerraformInstance()
	if err == nil {
		t.Fatalf("expected error from failing credentials provider")
	}
	if !strings.Contains(err.Error(), "failed to retrieve AWS credentials") {
		t.Errorf("error %q missing expected prefix", err.Error())
	}
}

// TestDeleteTerraformInstanceWrapsStateWriteError covers the branch where a
// state document is present but the target file cannot be written: the
// helper must return the "failed to write terraform state to file" wrap
// prefix.
func TestDeleteTerraformInstanceWrapsStateWriteError(t *testing.T) {
	// name the config directory the same as an existing file so Stat succeeds
	// (non-IsNotExist), the mkdir is skipped, and the state WriteFile then
	// fails because the "directory" is a file
	root := t.TempDir()
	tfDir := filepath.Join(root, "not-a-dir")
	if err := os.WriteFile(tfDir, []byte("blocker"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	c := &TerraformInstanceConfig{
		terraformInstance: &v0.TerraformInstance{
			StateDocument: util.Ptr("{\"version\":4}"),
		},
		log:       newTestLogger(),
		tfDirName: tfDir,
	}

	// invoke delete and expect the state-write wrap prefix
	err := c.deleteTerraformInstance()
	if err == nil {
		t.Fatalf("expected error from state WriteFile failure")
	}
	if !strings.Contains(err.Error(), "failed to write terraform state to file") {
		t.Errorf("error %q missing expected prefix", err.Error())
	}
}

// TestCreateTerraformInstanceWrapsVarsWriteError covers the create-path
// branch where a vars document is set but the target directory is invalid:
// the helper must return the "failed to write terraform vars to file" wrap
// prefix.
func TestCreateTerraformInstanceWrapsVarsWriteError(t *testing.T) {
	// invalid path forces the vars WriteFile to fail first
	c := &TerraformInstanceConfig{
		terraformInstance: &v0.TerraformInstance{
			VarsDocument: util.Ptr("region = \"us-west-2\""),
		},
		log:       newTestLogger(),
		tfDirName: "/proc/does-not-exist",
	}

	// invoke create and expect the vars-write wrap prefix
	err := c.createTerraformInstance()
	if err == nil {
		t.Fatalf("expected error from vars WriteFile failure")
	}
	if !strings.Contains(err.Error(), "failed to write terraform vars to file") {
		t.Errorf("error %q missing expected prefix", err.Error())
	}
}

// TestCreateTerraformInstanceWrapsCredentialsError covers the create-path
// branch where the vars write is skipped (nil VarsDocument) but the AWS
// credentials provider errors: the helper must return the "failed to
// retrieve AWS credentials" wrap prefix.
func TestCreateTerraformInstanceWrapsCredentialsError(t *testing.T) {
	// existing dir so no write is attempted; failing creds provider errors on Retrieve
	tfDir := t.TempDir()
	c := &TerraformInstanceConfig{
		terraformInstance: &v0.TerraformInstance{},
		log:               newTestLogger(),
		tfDirName:         tfDir,
		awsConfig:         &aws.Config{Credentials: failingCredentialsProvider{}},
	}

	// invoke create and expect the credentials wrap prefix
	err := c.createTerraformInstance()
	if err == nil {
		t.Fatalf("expected error from failing credentials provider")
	}
	if !strings.Contains(err.Error(), "failed to retrieve AWS credentials") {
		t.Errorf("error %q missing expected prefix", err.Error())
	}
}
