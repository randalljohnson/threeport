package controller

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/threeport/threeport/pkg/sdk/v0"
	"github.com/threeport/threeport/pkg/sdk/v0/gen"
)

// reconcilerFixtureGenerator returns a generator carrying the smallest input
// the reconciler generator reads: a module path for the client and API import
// paths, and one object group holding a single reconciled object.
func reconcilerFixtureGenerator() *gen.Generator {
	return &gen.Generator{
		ModulePath: "example.com/widget-module",
		ApiObjectGroups: []gen.ApiObjectGroup{
			{
				ControllerShortName:   "widget",
				ControllerPackageName: "widget",
				ReconciledObjects: []gen.ReconciledObject{
					{Name: "WidgetInstance", Versions: []string{"v0"}},
				},
			},
		},
	}
}

// generateReconciler runs the reconciler generator with the working directory
// pointed at a scratch tree, then returns what it wrote. The generator writes
// to a path relative to the working directory, so the chdir keeps the run out
// of the repository.
func generateReconciler(t *testing.T) string {
	t.Helper()

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to read working directory: %v", err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("failed to change to scratch directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Fatalf("failed to restore working directory: %v", err)
		}
	})

	if err := GenReconcilers(reconcilerFixtureGenerator(), &sdk.SdkConfig{}); err != nil {
		t.Fatalf("GenReconcilers returned an error: %v", err)
	}

	generated, err := os.ReadFile(filepath.Join(
		"internal",
		"widget",
		"widget_instance_reconciler_gen.go",
	))
	if err != nil {
		t.Fatalf("failed to read generated reconciler: %v", err)
	}

	return string(generated)
}

// Each operation's guard emits a skip message naming that operation, so the
// message is the only place in the generated source that says which branch the
// guard landed in.
const (
	skipCreateOnDeletion = `"widget instance scheduled for deletion - skipping create"`
	skipUpdateOnDeletion = `"widget instance scheduled for deletion - skipping update"`
	skipDeleteOnDeletion = `"widget instance scheduled for deletion - skipping delete"`
)

// TestGenReconcilersGuardsCreateAgainstScheduledDeletion asserts the create
// branch skips its handler once deletion is scheduled. Creating infrastructure
// for an object that is on its way out leaves resources behind that nothing
// owns.
func TestGenReconcilersGuardsCreateAgainstScheduledDeletion(t *testing.T) {
	generated := generateReconciler(t)

	if !strings.Contains(generated, skipCreateOnDeletion) {
		t.Errorf("create branch does not skip when deletion is scheduled")
	}
}

// TestGenReconcilersGuardsUpdateAgainstScheduledDeletion asserts the update
// branch skips its handler once deletion is scheduled.
//
// Without the guard, an object whose update handler fails keeps its update
// notification in redelivery. The notification is NAK'd into durable
// JetStream state rather than held in memory, so the retry outlives a
// controller restart, and the delete operation never gets a turn on the single
// reconciler worker. Deletion then blocks for as long as the update keeps
// failing, which is forever when the failure is a bad script or a missing
// package version.
func TestGenReconcilersGuardsUpdateAgainstScheduledDeletion(t *testing.T) {
	generated := generateReconciler(t)

	if !strings.Contains(generated, skipUpdateOnDeletion) {
		t.Errorf("update branch does not skip when deletion is scheduled")
	}
}

// TestGenReconcilersLeavesDeleteUnguarded asserts the delete branch carries no
// scheduled-for-deletion guard.
//
// Deletion is scheduled before the delete operation runs, so a guard on this
// branch would skip the only handler that tears the object down and confirms
// the delete. The object would survive every retry and never leave the
// database. The asymmetry across the three branches is deliberate.
func TestGenReconcilersLeavesDeleteUnguarded(t *testing.T) {
	generated := generateReconciler(t)

	if strings.Contains(generated, skipDeleteOnDeletion) {
		t.Errorf("delete branch skips the handler that performs the deletion")
	}
}

// TestGenReconcilersNamesTheOperationInVersionMismatch asserts each branch
// reports the operation it was running when it met an unrecognized object
// version. The error reaches an operator as the whole explanation of a stalled
// object, so naming the wrong operation sends the reader to the wrong handler.
func TestGenReconcilersNamesTheOperationInVersionMismatch(t *testing.T) {
	generated := generateReconciler(t)

	for _, want := range []string{
		`"unrecognized version of widget instance encountered for create operation"`,
		`"unrecognized version of widget instance encountered for update operation"`,
		`"unrecognized version of widget instance encountered for delete operation"`,
	} {
		if !strings.Contains(generated, want) {
			t.Errorf("generated reconciler does not report %s", want)
		}
	}
}
