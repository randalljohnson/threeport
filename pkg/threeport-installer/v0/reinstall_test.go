package v0

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/threeport/threeport/pkg/api-server/v0/database"
)

// testNamespaceMapper returns a mapper that resolves the Namespace kind,
// which is the only kind the tier lookup maps.
func testNamespaceMapper() meta.RESTMapper {
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}, meta.RESTScopeRoot)

	return mapper
}

// testNamespace returns a namespace object carrying the supplied tier
// label. An empty tier produces a namespace with no labels at all,
// standing in for one installed before the tier was recorded.
func testNamespace(name, tier string) *unstructured.Unstructured {
	namespace := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Namespace",
			"metadata": map[string]interface{}{
				"name": name,
			},
		},
	}
	if tier != "" {
		namespace.SetLabels(map[string]string{LabelTier: tier})
	}

	return namespace
}

// testStatefulSet returns a statefulset standing in for one of the
// stateful components the drop must leave alone.
func testStatefulSet(name, namespace string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "StatefulSet",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
			},
		},
	}
}

// testVolumeClaim returns a volume claim standing in for the one
// holding the database's data directory.
func testVolumeClaim(name, namespace string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "PersistentVolumeClaim",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
			},
		},
	}
}

// testManagedDeployment returns an installer-managed deployment with no
// ready replicas, standing in for one whose pods have already drained.
// The scale-down waits on ready replicas, so a deployment reporting any
// would hold the test for the full drain deadline.
func testManagedDeployment(name, namespace string) *unstructured.Unstructured {
	deployment := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]interface{}{
				"replicas": int64(1),
			},
		},
	}
	deployment.SetLabels(map[string]string{LabelManagedBy: LabelManagedByValue})

	return deployment
}

// testKubeClient returns a fake dynamic client seeded with the supplied
// objects and a scheme that knows every list kind the reinstall reads.
func testKubeClient(objects ...runtime.Object) *dynamicfake.FakeDynamicClient {
	scheme := runtime.NewScheme()
	for _, listKind := range []schema.GroupVersionKind{
		{Version: "v1", Kind: "NamespaceList"},
		{Version: "v1", Kind: "PersistentVolumeClaimList"},
		{Group: "apps", Version: "v1", Kind: "DeploymentList"},
		{Group: "apps", Version: "v1", Kind: "StatefulSetList"},
		{Group: "batch", Version: "v1", Kind: "JobList"},
	} {
		scheme.AddKnownTypeWithName(listKind, &unstructured.UnstructuredList{})
	}

	return dynamicfake.NewSimpleDynamicClient(scheme, objects...)
}

// succeedDropJob makes the fake client answer a drop job's status the
// way a cluster would once its pod has exited cleanly. The reactor
// stamps the status onto the object on its way into the tracker and
// leaves the create itself to the default handler, so the poll that
// follows reads a completed job.
func succeedDropJob(kubeClient *dynamicfake.FakeDynamicClient) *unstructured.Unstructured {
	created := &unstructured.Unstructured{}

	kubeClient.PrependReactor("create", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
		job, ok := action.(k8stesting.CreateAction).GetObject().(*unstructured.Unstructured)
		if !ok {
			return false, nil, nil
		}
		created.Object = job.Object
		if err := unstructured.SetNestedField(job.Object, int64(1), "status", "succeeded"); err != nil {
			return false, nil, err
		}

		return false, nil, nil
	})

	return created
}

// allowDeploymentScaleDown makes the fake client accept the scale-down
// patch and records which deployments it was issued against. The fake
// client cannot apply a strategic merge patch to an unstructured
// object, so the reactor answers with the scaled-down deployment
// itself.
func allowDeploymentScaleDown(kubeClient *dynamicfake.FakeDynamicClient, scaled *[]string) {
	kubeClient.PrependReactor("patch", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		patch := action.(k8stesting.PatchAction)
		*scaled = append(*scaled, patch.GetName())

		deployment := testManagedDeployment(patch.GetName(), patch.GetNamespace())
		if err := unstructured.SetNestedField(deployment.Object, int64(0), "spec", "replicas"); err != nil {
			return true, nil, err
		}

		return true, deployment, nil
	})
}

// TestDropDatabaseRejectsNonDevelopmentControlPlane asserts that the
// database drop refuses every control plane it cannot confirm is a
// development installation, and that it touches nothing when it
// refuses.
func TestDropDatabaseRejectsNonDevelopmentControlPlane(t *testing.T) {
	tests := []struct {
		name string
		tier string
	}{
		{
			name: "production tier is refused",
			tier: ControlPlaneTierProd,
		},
		{
			name: "unrecognized tier is refused",
			tier: "staging",
		},
		{
			name: "absent tier is refused",
			tier: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// seed a namespace at the tier under test alongside a
			// running control plane the drop would otherwise act on
			namespace := "threeport-control-plane"
			kubeClient := testKubeClient(
				testNamespace(namespace, test.tier),
				testManagedDeployment("threeport-api-server", namespace),
			)
			var scaled []string
			allowDeploymentScaleDown(kubeClient, &scaled)
			mapper := testNamespaceMapper()
			cpi := &ControlPlaneInstaller{Opts: Options{Namespace: namespace}}

			err := cpi.DropDatabase(kubeClient, &mapper)

			// the refusal is reported as the tier error so callers can
			// match it without reading the message
			if err == nil {
				t.Fatal("expected drop to be refused, got nil error")
			}
			if !errors.Is(err, ErrControlPlaneNotDevelopment) {
				t.Errorf("expected error to match ErrControlPlaneNotDevelopment, got: %v", err)
			}

			// a refused drop leaves the control plane running: nothing
			// is scaled down and no drop is issued
			if len(scaled) > 0 {
				t.Errorf("expected no deployment to be scaled down on a refused drop, got %v", scaled)
			}
			if _, getErr := kubeClient.Resource(jobGVR).Namespace(namespace).Get(
				context.Background(), dropDatabaseJobName, metav1.GetOptions{},
			); getErr == nil {
				t.Error("expected no database drop job to be created on a refused drop")
			}
		})
	}
}

// TestDropDatabaseRunsDropStatementOnDevelopmentControlPlane asserts
// that a development control plane is quiesced and its schema dropped
// by a statement issued against the running database, and that every
// stateful resource the database depends on is left in place.
func TestDropDatabaseRunsDropStatementOnDevelopmentControlPlane(t *testing.T) {
	// seed a development namespace with a running control plane, the
	// database, its data volume, and the message broker
	namespace := "threeport-control-plane"
	kubeClient := testKubeClient(
		testNamespace(namespace, ControlPlaneTierDev),
		testManagedDeployment("threeport-api-server", namespace),
		testStatefulSet("crdb", namespace),
		testStatefulSet("nats-js", namespace),
		testVolumeClaim("datadir-crdb-0", namespace),
	)
	var scaled []string
	allowDeploymentScaleDown(kubeClient, &scaled)
	createdJob := succeedDropJob(kubeClient)
	mapper := testNamespaceMapper()
	cpi := &ControlPlaneInstaller{Opts: Options{Namespace: namespace}}

	if err := cpi.DropDatabase(kubeClient, &mapper); err != nil {
		t.Fatalf("expected drop to succeed on a development control plane, got: %v", err)
	}

	// nothing may be writing to the schema while it is dropped
	if len(scaled) != 1 || scaled[0] != "threeport-api-server" {
		t.Errorf("expected the api server deployment to be scaled down, got %v", scaled)
	}

	// the drop is a statement against the running database, naming the
	// database and cascading to everything in it. it first cancels any
	// paused schema change, which would otherwise hold the drop open
	// forever waiting on a change that never resumes
	statement := dropDatabaseJobStatement(t, createdJob)
	for _, want := range []string{
		"CANCEL JOBS",
		"paused",
		"NEW SCHEMA CHANGE",
		"DROP DATABASE",
		database.ThreeportDatabaseName,
		"CASCADE",
	} {
		if !strings.Contains(statement, want) {
			t.Errorf("expected the drop statement to contain %q, got %q", want, statement)
		}
	}

	// the cancel has to precede the drop, since cancelling afterward would
	// not release a drop that is already blocked
	if strings.Index(statement, "CANCEL JOBS") > strings.Index(statement, "DROP DATABASE") {
		t.Errorf("expected the cancel to precede the drop, got %q", statement)
	}

	// the job runs once and is cleared, so a later drop is not refused
	// by a job left behind by this one
	if _, err := kubeClient.Resource(jobGVR).Namespace(namespace).Get(
		context.Background(), dropDatabaseJobName, metav1.GetOptions{},
	); err == nil {
		t.Error("expected the database drop job to be removed once it succeeded")
	}

	// the data volume, the database and the message broker are all
	// outside the schema and survive the drop
	for _, survivor := range []struct {
		gvr  schema.GroupVersionResource
		name string
	}{
		{gvr: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}, name: "crdb"},
		{gvr: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}, name: "nats-js"},
		{gvr: schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}, name: "datadir-crdb-0"},
	} {
		if _, err := kubeClient.Resource(survivor.gvr).Namespace(namespace).Get(
			context.Background(), survivor.name, metav1.GetOptions{},
		); err != nil {
			t.Errorf("expected %s/%s to survive the drop, got: %v", survivor.gvr.Resource, survivor.name, err)
		}
	}
}

// TestDropDatabaseReportsFailedJob asserts that a drop whose pod keeps
// failing is reported rather than waited out, and that the failure
// names where to look for what the database said.
func TestDropDatabaseReportsFailedJob(t *testing.T) {
	namespace := "threeport-control-plane"
	kubeClient := testKubeClient(testNamespace(namespace, ControlPlaneTierDev))
	kubeClient.PrependReactor("create", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
		job, ok := action.(k8stesting.CreateAction).GetObject().(*unstructured.Unstructured)
		if !ok {
			return false, nil, nil
		}
		// one more failure than the job allows, which is where
		// kubernetes stops replacing the pod
		if err := unstructured.SetNestedField(
			job.Object, int64(dropDatabaseJobBackoffLimit+1), "status", "failed",
		); err != nil {
			return false, nil, err
		}

		return false, nil, nil
	})
	mapper := testNamespaceMapper()
	cpi := &ControlPlaneInstaller{Opts: Options{Namespace: namespace}}

	err := cpi.DropDatabase(kubeClient, &mapper)
	if err == nil {
		t.Fatal("expected a failed drop job to be reported, got nil error")
	}
	if !strings.Contains(err.Error(), namespace) {
		t.Errorf("expected the error to name the namespace to inspect, got: %v", err)
	}
}

// TestDropMessageBrokerStateRejectsNonDevelopmentControlPlane asserts
// that the broker drop refuses every control plane it cannot confirm is
// a development installation, and that it touches nothing when it
// refuses.
func TestDropMessageBrokerStateRejectsNonDevelopmentControlPlane(t *testing.T) {
	for _, tier := range []string{ControlPlaneTierProd, "staging", ""} {
		t.Run(fmt.Sprintf("tier %q is refused", tier), func(t *testing.T) {
			namespace := "threeport-control-plane"
			kubeClient := testKubeClient(testNamespace(namespace, tier))
			mapper := testNamespaceMapper()
			cpi := &ControlPlaneInstaller{Opts: Options{Namespace: namespace}}

			err := cpi.DropMessageBrokerState(kubeClient, &mapper)

			// the refusal is reported as the tier error so callers can
			// match it without reading the message
			if err == nil {
				t.Fatal("expected drop to be refused, got nil error")
			}
			if !errors.Is(err, ErrControlPlaneNotDevelopment) {
				t.Errorf("expected error to match ErrControlPlaneNotDevelopment, got: %v", err)
			}

			// a refused drop issues nothing against the broker
			if _, getErr := kubeClient.Resource(jobGVR).Namespace(namespace).Get(
				context.Background(), dropMessageBrokerJobName, metav1.GetOptions{},
			); getErr == nil {
				t.Error("expected no message broker drop job to be created on a refused drop")
			}
		})
	}
}

// TestDropMessageBrokerStateRemovesEveryStreamOnDevelopmentControlPlane
// asserts that the broker drop removes streams by name rather than
// naming any one of them, so notification streams and the key-value
// buckets holding reconciliation locks both go, and that the broker
// itself and its data volume are left in place.
func TestDropMessageBrokerStateRemovesEveryStreamOnDevelopmentControlPlane(t *testing.T) {
	namespace := "threeport-control-plane"
	kubeClient := testKubeClient(
		testNamespace(namespace, ControlPlaneTierDev),
		testStatefulSet("nats-js", namespace),
		testVolumeClaim("datadir-nats-js-0", namespace),
	)
	createdJob := succeedDropJob(kubeClient)
	mapper := testNamespaceMapper()
	cpi := &ControlPlaneInstaller{Opts: Options{Namespace: namespace}}

	if err := cpi.DropMessageBrokerState(kubeClient, &mapper); err != nil {
		t.Fatalf("expected drop to succeed on a development control plane, got: %v", err)
	}

	// the removal enumerates whatever the broker holds and removes each
	// one, so a stream added by a later release needs no change here
	script := dropJobShellScript(t, createdJob)
	for _, want := range []string{"nats stream ls --names", "nats stream rm", "--force"} {
		if !strings.Contains(script, want) {
			t.Errorf("expected the drop script to contain %q, got %q", want, script)
		}
	}

	// the job runs once and is cleared, so a later drop is not refused
	// by a job left behind by this one
	if _, err := kubeClient.Resource(jobGVR).Namespace(namespace).Get(
		context.Background(), dropMessageBrokerJobName, metav1.GetOptions{},
	); err == nil {
		t.Error("expected the message broker drop job to be removed once it succeeded")
	}

	// the broker and its data volume are outside the streams and survive
	for _, survivor := range []struct {
		gvr  schema.GroupVersionResource
		name string
	}{
		{gvr: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}, name: "nats-js"},
		{gvr: schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}, name: "datadir-nats-js-0"},
	} {
		if _, err := kubeClient.Resource(survivor.gvr).Namespace(namespace).Get(
			context.Background(), survivor.name, metav1.GetOptions{},
		); err != nil {
			t.Errorf("expected %s/%s to survive the drop, got: %v", survivor.gvr.Resource, survivor.name, err)
		}
	}
}

// dropJobShellScript returns the shell script the drop job was created
// to run, read back out of the job's container command.
func dropJobShellScript(t *testing.T, job *unstructured.Unstructured) string {
	t.Helper()

	command := dropJobCommand(t, job)

	// the script is the argument the shell is told to interpret
	for i, arg := range command {
		if arg == "-c" && i+1 < len(command) {
			return command[i+1]
		}
	}

	t.Fatalf("expected the command to interpret a script, got %v", command)

	return ""
}

// dropDatabaseJobStatement returns the SQL the drop job was created to
// run, read back out of the job's container command.
func dropDatabaseJobStatement(t *testing.T, job *unstructured.Unstructured) string {
	t.Helper()

	command := dropJobCommand(t, job)

	// the statement is the argument the client is told to execute
	for i, arg := range command {
		if arg == "--execute" && i+1 < len(command) {
			return command[i+1]
		}
	}

	t.Fatalf("expected the command to execute a statement, got %v", command)

	return ""
}

// dropJobCommand returns the container command a drop job was created
// with, so a caller can assert on the argument it cares about.
func dropJobCommand(t *testing.T, job *unstructured.Unstructured) []string {
	t.Helper()

	containers, found, err := unstructured.NestedSlice(job.Object, "spec", "template", "spec", "containers")
	if err != nil || !found || len(containers) == 0 {
		t.Fatalf("expected the drop job to define a container, got found=%v err=%v", found, err)
	}

	container, ok := containers[0].(map[string]interface{})
	if !ok {
		t.Fatal("expected the drop job's container to be an object")
	}

	command, found, err := unstructured.NestedStringSlice(container, "command")
	if err != nil || !found {
		t.Fatalf("expected the drop job's container to define a command, got found=%v err=%v", found, err)
	}

	return command
}
