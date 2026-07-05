//go:build integration

package main

import (
	"errors"
	"fmt"
	"testing"
	"time"

	v0 "github.com/threeport/threeport/pkg/api/v0"
	client_lib "github.com/threeport/threeport/pkg/client/lib/v0"
	client "github.com/threeport/threeport/pkg/client/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// reconcileFixtureYAML deploys a small workload the reconcile-loop test can
// watch through its state transitions.
const reconcileFixtureYAML = `---
apiVersion: v1
kind: Namespace
metadata:
  name: reconcile-integration
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: reconcile-integration
  namespace: reconcile-integration
spec:
  replicas: 1
  selector:
    matchLabels:
      app: reconcile-integration
  template:
    metadata:
      labels:
        app: reconcile-integration
    spec:
      containers:
        - name: app
          image: registry.k8s.io/pause:3.9
`

// TestControllerReconcilesWorkloadDefinition creates a workload definition
// and asserts the workload-controller flips Reconciled=true within the poll
// window.
func TestControllerReconcilesWorkloadDefinition(t *testing.T) {
	requireKube(t)
	apiAddr, apiClient := getAPIServerURL(t)

	// setup: unique name so parallel runs don't collide on the definition
	// unique-name constraint
	name := fmt.Sprintf("reconcile-def-%d", time.Now().UnixNano())
	def, err := client.CreateKubernetesWorkloadDefinition(apiClient, apiAddr, &v0.KubernetesWorkloadDefinition{
		Definition:   v0.Definition{Name: util.Ptr(name)},
		YAMLDocument: util.Ptr(reconcileFixtureYAML),
	})
	if err != nil {
		t.Fatalf("failed to create workload definition: %v", err)
	}
	defer func() { _, _ = client.DeleteKubernetesWorkloadDefinition(apiClient, apiAddr, *def.ID) }()

	// assert: Reconciled flips to true once the workload controller
	// finishes its first pass
	waitForResource(t, 3*time.Minute, 2*time.Second, "workload definition Reconciled=true", func() error {
		latest, err := client.GetKubernetesWorkloadDefinitionByID(apiClient, apiAddr, *def.ID)
		if err != nil {
			return err
		}
		if latest.Reconciled == nil || !*latest.Reconciled {
			return errors.New("not yet reconciled")
		}
		return nil
	})
}

// TestControllerReconcilesWorkloadInstance covers the instance leg of the
// reconcile loop: create a workload instance and watch Reconciled=true.
func TestControllerReconcilesWorkloadInstance(t *testing.T) {
	requireKube(t)
	apiAddr, apiClient := getAPIServerURL(t)

	// setup: definition, plus the control-plane runtime instance the
	// workload instance can target
	defName := fmt.Sprintf("reconcile-inst-def-%d", time.Now().UnixNano())
	def, err := client.CreateKubernetesWorkloadDefinition(apiClient, apiAddr, &v0.KubernetesWorkloadDefinition{
		Definition:   v0.Definition{Name: util.Ptr(defName)},
		YAMLDocument: util.Ptr(reconcileFixtureYAML),
	})
	if err != nil {
		t.Fatalf("failed to create workload definition: %v", err)
	}
	defer func() { _, _ = client.DeleteKubernetesWorkloadDefinition(apiClient, apiAddr, *def.ID) }()

	kri, err := client.GetThreeportControlPlaneKubernetesRuntimeInstance(apiClient, apiAddr)
	if err != nil {
		t.Fatalf("failed to look up control-plane runtime instance: %v", err)
	}

	instName := defName
	inst, err := client.CreateKubernetesWorkloadInstance(apiClient, apiAddr, &v0.KubernetesWorkloadInstance{
		Instance:                       v0.Instance{Name: util.Ptr(instName)},
		KubernetesRuntimeInstanceID:    kri.ID,
		KubernetesWorkloadDefinitionID: def.ID,
	})
	if err != nil {
		t.Fatalf("failed to create workload instance: %v", err)
	}
	defer func() { _, _ = client.DeleteKubernetesWorkloadInstance(apiClient, apiAddr, *inst.ID) }()

	// assert: Reconciled flips true within the poll window
	waitForResource(t, 5*time.Minute, 2*time.Second, "workload instance Reconciled=true", func() error {
		latest, err := client.GetKubernetesWorkloadInstanceByID(apiClient, apiAddr, *inst.ID)
		if err != nil {
			return err
		}
		if latest.Reconciled == nil || !*latest.Reconciled {
			return errors.New("not yet reconciled")
		}
		return nil
	})
}

// TestControllerHandlesDeleteReconciliation covers the tear-down half of the
// reconcile loop: delete an instance and watch it disappear.
func TestControllerHandlesDeleteReconciliation(t *testing.T) {
	requireKube(t)
	apiAddr, apiClient := getAPIServerURL(t)

	// setup: create a definition + instance, then trigger the delete
	defName := fmt.Sprintf("reconcile-del-def-%d", time.Now().UnixNano())
	def, err := client.CreateKubernetesWorkloadDefinition(apiClient, apiAddr, &v0.KubernetesWorkloadDefinition{
		Definition:   v0.Definition{Name: util.Ptr(defName)},
		YAMLDocument: util.Ptr(reconcileFixtureYAML),
	})
	if err != nil {
		t.Fatalf("failed to create workload definition: %v", err)
	}
	defer func() { _, _ = client.DeleteKubernetesWorkloadDefinition(apiClient, apiAddr, *def.ID) }()

	kri, err := client.GetThreeportControlPlaneKubernetesRuntimeInstance(apiClient, apiAddr)
	if err != nil {
		t.Fatalf("failed to look up control-plane runtime instance: %v", err)
	}
	inst, err := client.CreateKubernetesWorkloadInstance(apiClient, apiAddr, &v0.KubernetesWorkloadInstance{
		Instance:                       v0.Instance{Name: util.Ptr(defName)},
		KubernetesRuntimeInstanceID:    kri.ID,
		KubernetesWorkloadDefinitionID: def.ID,
	})
	if err != nil {
		t.Fatalf("failed to create workload instance: %v", err)
	}

	// action: issue the delete and let the controller run
	if _, err := client.DeleteKubernetesWorkloadInstance(apiClient, apiAddr, *inst.ID); err != nil {
		t.Fatalf("failed to issue delete: %v", err)
	}

	// assert: the row is gone from the API within the poll window
	waitForResource(t, 3*time.Minute, 2*time.Second, "workload instance deletion reconciled", func() error {
		_, err := client.GetKubernetesWorkloadInstanceByID(apiClient, apiAddr, *inst.ID)
		if err == nil {
			return errors.New("still present")
		}
		if errors.Is(err, client_lib.ErrObjectNotFound) {
			return nil
		}
		return err
	})
}
