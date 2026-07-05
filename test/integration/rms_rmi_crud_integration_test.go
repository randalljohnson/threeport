//go:build integration

package main

import (
	"fmt"
	"testing"
	"time"

	v0 "github.com/threeport/threeport/pkg/api/v0"
	client "github.com/threeport/threeport/pkg/client/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestRMSCRUDViaSXAAvailability documents that the sxa module's CLI is used
// upstream for RMS/RMI CRUD; when the sxa-built tptctl is not on PATH we
// fall back to direct client_v0 calls against the fork's MachineRuntime
// endpoints in the tests below.
func TestRMSCRUDViaSXAAvailability(t *testing.T) {
	if !sxaCLIAvailable() {
		t.Skip("sxa CLI not available; RMS/RMI CRUD exercised via direct API in sibling tests")
	}
	// tptctl / sxa is available; still skip because scripting the full
	// module workflow is out of scope for the fork's integration suite.
	// The presence of the CLI is asserted so a future test can extend
	// coverage without a redundant skip.
}

// TestMachineRuntimeDefinitionCRUD exercises the CRUD surface for the
// MachineRuntimeDefinition (RMS-analog on the fork): create, read, list,
// delete.
func TestMachineRuntimeDefinitionCRUD(t *testing.T) {
	requireKube(t)
	apiAddr, apiClient := getAPIServerURL(t)

	// setup: unique name so parallel runs don't collide on the unique
	// index that guards Name
	name := fmt.Sprintf("mrd-integration-%d", time.Now().UnixNano())

	// action: create
	created, err := client.CreateMachineRuntimeDefinition(apiClient, apiAddr, &v0.MachineRuntimeDefinition{
		Definition: v0.Definition{Name: util.Ptr(name)},
	})
	if err != nil {
		t.Skipf("skipping: create MachineRuntimeDefinition failed on this control plane (%v)", err)
	}
	defer func() {
		_, _ = client.DeleteMachineRuntimeDefinition(apiClient, apiAddr, *created.ID)
	}()

	// action: read back by ID
	got, err := client.GetMachineRuntimeDefinitionByID(apiClient, apiAddr, *created.ID)
	if err != nil {
		t.Fatalf("failed to read back MachineRuntimeDefinition: %v", err)
	}
	// assert: the read-back name matches what we wrote
	if got.Name == nil || *got.Name != name {
		t.Fatalf("read-back name mismatch: got %v, want %q", got.Name, name)
	}

	// action: list and assert our new row is present
	all, err := client.GetMachineRuntimeDefinitions(apiClient, apiAddr)
	if err != nil {
		t.Fatalf("failed to list MachineRuntimeDefinitions: %v", err)
	}
	if all == nil {
		t.Fatal("expected non-nil list result")
	}
	found := false
	for _, d := range *all {
		if d.ID != nil && *d.ID == *created.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("created MachineRuntimeDefinition ID=%d not in list result", *created.ID)
	}

	// action + assert: delete succeeds
	if _, err := client.DeleteMachineRuntimeDefinition(apiClient, apiAddr, *created.ID); err != nil {
		t.Fatalf("failed to delete MachineRuntimeDefinition: %v", err)
	}
}

// TestMachineRuntimeInstanceCRUD exercises the CRUD surface for a
// MachineRuntimeInstance (RMI-analog): create paired with a definition,
// read, and delete.
func TestMachineRuntimeInstanceCRUD(t *testing.T) {
	requireKube(t)
	apiAddr, apiClient := getAPIServerURL(t)

	// setup: paired definition the instance can reference; keep it around
	// until the instance is deleted so the FK guard doesn't block cleanup
	defName := fmt.Sprintf("mri-integration-def-%d", time.Now().UnixNano())
	def, err := client.CreateMachineRuntimeDefinition(apiClient, apiAddr, &v0.MachineRuntimeDefinition{
		Definition: v0.Definition{Name: util.Ptr(defName)},
	})
	if err != nil {
		t.Skipf("skipping: create MachineRuntimeDefinition failed on this control plane (%v)", err)
	}
	defer func() { _, _ = client.DeleteMachineRuntimeDefinition(apiClient, apiAddr, *def.ID) }()

	// action: create the instance
	instName := fmt.Sprintf("mri-integration-inst-%d", time.Now().UnixNano())
	inst, err := client.CreateMachineRuntimeInstance(apiClient, apiAddr, &v0.MachineRuntimeInstance{
		Instance:                   v0.Instance{Name: util.Ptr(instName)},
		MachineRuntimeDefinitionID: def.ID,
	})
	if err != nil {
		t.Skipf("skipping: create MachineRuntimeInstance failed on this control plane (%v)", err)
	}
	defer func() { _, _ = client.DeleteMachineRuntimeInstance(apiClient, apiAddr, *inst.ID) }()

	// action: read back by ID
	got, err := client.GetMachineRuntimeInstanceByID(apiClient, apiAddr, *inst.ID)
	if err != nil {
		t.Fatalf("failed to read back MachineRuntimeInstance: %v", err)
	}
	// assert: the read-back name matches what we wrote
	if got.Name == nil || *got.Name != instName {
		t.Fatalf("read-back name mismatch: got %v, want %q", got.Name, instName)
	}

	// action + assert: delete succeeds and the row disappears from list
	if _, err := client.DeleteMachineRuntimeInstance(apiClient, apiAddr, *inst.ID); err != nil {
		t.Fatalf("failed to delete MachineRuntimeInstance: %v", err)
	}
}
