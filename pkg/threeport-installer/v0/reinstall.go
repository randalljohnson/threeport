package v0

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/threeport/threeport/pkg/api-server/v0/database"
	auth "github.com/threeport/threeport/pkg/auth/v0"
	kube "github.com/threeport/threeport/pkg/kube/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// deploymentGVR is reused by the scale-down and verify phases (which
// target Deployments by name or by label).
var deploymentGVR = schema.GroupVersionResource{
	Group:    "apps",
	Version:  "v1",
	Resource: "deployments",
}

// deleteTarget pairs a GVR with whether it's namespace-scoped. Cluster
// -scoped kinds (ClusterRole, ClusterRoleBinding) skip the .Namespace
// call on the dynamic client.
type deleteTarget struct {
	gvr        schema.GroupVersionResource
	namespaced bool
}

// deleteTargets is the closed set of kinds the reinstall delete phase
// walks. Each kind that the installer can create is listed here;
// resources whose loss is unrecoverable (cascade-delete, data,
// external state) are deliberately excluded:
//
//	customresourcedefinitions - cascade-deletes every cr of that type
//	namespaces                - cascade-deletes everything in the ns
//	persistentvolumeclaims    - data loss
//	statefulsets              - the persistent label protects cockroach
//	                            and nats specifically; no other
//	                            installer-managed statefulset exists
var deleteTargets = []deleteTarget{
	{gvr: deploymentGVR, namespaced: true},
	{gvr: schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}, namespaced: true},
	{gvr: schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"}, namespaced: true},
	{gvr: schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"}, namespaced: true},
	{gvr: schema.GroupVersionResource{Group: "", Version: "v1", Resource: "serviceaccounts"}, namespaced: true},
	{gvr: schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "roles"}, namespaced: true},
	{gvr: schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "rolebindings"}, namespaced: true},
	{gvr: schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles"}, namespaced: false},
	{gvr: schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterrolebindings"}, namespaced: false},
}

// restApiDeploymentReadyTimeout caps how long Reinstall will wait for
// the rest-api deployment to roll back to ready after install.
const restApiDeploymentReadyTimeout = 5 * time.Minute

// dropDatabaseJobName is the one-off job that issues the drop against
// the running database. The name is fixed, so a job left behind by an
// interrupted run is replaced instead of accumulating alongside it.
const dropDatabaseJobName = "threeport-drop-database"

// dropDatabaseJobBackoffLimit is how many times kubernetes replaces the
// drop job's pod before it reports the job failed. Retries cover a
// database still finishing its own startup when the job first runs.
const dropDatabaseJobBackoffLimit = 3

// dropMessageBrokerJobName is the one-off job that removes the message
// broker's streams. Fixed for the same reason the database drop job's
// name is fixed.
const dropMessageBrokerJobName = "threeport-drop-message-broker"

// dropMessageBrokerJobBackoffLimit is how many times kubernetes replaces
// the broker drop job's pod before it reports the job failed. Retries
// cover a broker still electing its leader when the job first runs.
const dropMessageBrokerJobBackoffLimit = 3

// dbRootCertsMountPath is where the drop job mounts the database's root
// credentials. The cockroach client reads the whole directory, so the
// three files the secret holds are named the way it expects.
const dbRootCertsMountPath = "/cockroach/cockroach-certs"

// dbRootCertsFileMode makes the mounted credentials readable only by
// the user running the job. The cockroach client refuses a client key
// that anyone else can read.
const dbRootCertsFileMode = 0600

// jobGVR is the kind the database drop creates. It appears in no delete
// target list: the drop removes its own job.
var jobGVR = schema.GroupVersionResource{
	Group:    "batch",
	Version:  "v1",
	Resource: "jobs",
}

// ErrControlPlaneNotDevelopment reports that an operation restricted to
// development control planes was attempted against one installed at a
// different tier, or against one whose tier cannot be established.
var ErrControlPlaneNotDevelopment = errors.New("control plane is not installed at the development tier")

// DropDatabase drops the control plane's schema, so the next install
// recreates the database empty and the migrations run from scratch. The
// data is not recoverable afterward.
//
// Only a control plane installed at the development tier may be
// dropped. The tier is read from the namespace in the target cluster
// rather than taken from the caller, so pointing a development command
// at a production cluster is refused by that cluster's own record of
// how it was installed. A namespace carrying no tier is refused too,
// which covers control planes installed before the tier was recorded.
//
// The drop is issued as SQL against the running database rather than by
// deleting the database's kubernetes resources, so everything outside
// the schema survives: the volume holding the data directory, the
// database's own certificates, the certificate authority, the message
// broker's data, and the api's external endpoint. Nobody has to
// re-issue or re-download credentials afterward, and no volume has to
// be reprovisioned.
//
// Control plane deployments are scaled to zero first, so no component
// is writing to the schema while it is being dropped. The install that
// follows brings them back.
func (cpi *ControlPlaneInstaller) DropDatabase(
	kubeClient dynamic.Interface,
	mapper *meta.RESTMapper,
) error {
	namespace := cpi.Opts.Namespace

	tier, err := cpi.getInstalledTier(kubeClient, mapper)
	if err != nil {
		return err
	}
	if tier != ControlPlaneTierDev {
		return fmt.Errorf(
			"%w: namespace %s reports tier %q, refusing to drop its database",
			ErrControlPlaneNotDevelopment, namespace, tier,
		)
	}

	// quiesce the control plane first so nothing is mid-write when its
	// tables go away
	if err := cpi.scaleDownDeployments(kubeClient, namespace); err != nil {
		return fmt.Errorf("failed to scale control plane down before the drop: %w", err)
	}

	if err := cpi.runDropDatabaseJob(kubeClient, namespace); err != nil {
		return err
	}

	fmt.Println("Info: database dropped, it will be recreated empty by the install that follows")

	return nil
}

// DropMessageBrokerState removes every stream the message broker holds,
// which takes their consumers and any undelivered notifications with
// them. The next install recreates the streams, and each controller
// recreates its own consumer and lock bucket as it starts.
//
// This belongs with the database drop rather than beside it as a choice.
// The broker's streams carry notifications naming rows by identifier and
// its key-value buckets carry reconciliation locks keyed the same way,
// so dropping the database without dropping the broker leaves messages
// and locks pointing at rows that no longer exist. Worse, a durable
// consumer keeps whatever configuration it was created with: a delivery
// limit set by an older release survives every later install, and
// nothing reports the divergence, so an object can stop being reconciled
// with no record of anything having given up.
//
// The caller quiesces the control plane before calling this, which the
// database drop already does, so no controller is holding a lock or
// mid-delivery while the streams go away.
//
// The removal is issued with the broker's own command line client over
// the network, so the volume holding the broker's data directory
// survives and no volume has to be reprovisioned.
func (cpi *ControlPlaneInstaller) DropMessageBrokerState(
	kubeClient dynamic.Interface,
	mapper *meta.RESTMapper,
) error {
	namespace := cpi.Opts.Namespace

	tier, err := cpi.getInstalledTier(kubeClient, mapper)
	if err != nil {
		return err
	}
	if tier != ControlPlaneTierDev {
		return fmt.Errorf(
			"%w: namespace %s reports tier %q, refusing to drop its message broker state",
			ErrControlPlaneNotDevelopment, namespace, tier,
		)
	}

	if err := cpi.runDropMessageBrokerJob(kubeClient, namespace); err != nil {
		return err
	}

	fmt.Println("Info: message broker streams dropped, they will be recreated by the install that follows")

	return nil
}

// runDropMessageBrokerJob removes every stream from a one-off job in the
// control plane namespace, waits for it to report success, and removes
// it. Key-value buckets are streams too, so listing by name reaches the
// reconciliation lock buckets alongside the notification streams and one
// pass clears both.
func (cpi *ControlPlaneInstaller) runDropMessageBrokerJob(
	kubeClient dynamic.Interface,
	namespace string,
) error {
	// clear a job left behind by an interrupted run: a job's pod
	// template is immutable, so creating over one is refused
	if err := cpi.deleteDropJob(
		kubeClient, namespace, dropMessageBrokerJobName, "message broker",
	); err != nil {
		return err
	}

	// remove each stream by name. an empty broker yields an empty list
	// and the loop does nothing, so a repeat run is not an error
	script := "set -e; for stream in $(nats stream ls --names); do nats stream rm \"$stream\" --force; done"
	fmt.Println("Info: removing every message broker stream and key-value bucket")

	job := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "batch/v1",
			"kind":       "Job",
			"metadata": map[string]interface{}{
				"name":      dropMessageBrokerJobName,
				"namespace": namespace,
			},
			"spec": map[string]interface{}{
				"backoffLimit": int64(dropMessageBrokerJobBackoffLimit),
				"template": map[string]interface{}{
					"spec": map[string]interface{}{
						"restartPolicy": "Never",
						"containers": []interface{}{
							map[string]interface{}{
								"name":  "drop-message-broker",
								"image": natsBoxImage,
								"env": []interface{}{
									map[string]interface{}{
										"name":  "NATS_URL",
										"value": natsServiceName,
									},
								},
								"command": []interface{}{"/bin/sh", "-c", script},
							},
						},
					},
				},
			},
		},
	}

	if _, err := kubeClient.Resource(jobGVR).Namespace(namespace).Create(
		context.Background(), job, metav1.CreateOptions{},
	); err != nil {
		return fmt.Errorf("failed to create message broker drop job %s: %w", dropMessageBrokerJobName, err)
	}

	if err := cpi.waitForDropJob(
		kubeClient, namespace, dropMessageBrokerJobName, dropMessageBrokerJobBackoffLimit, "message broker",
	); err != nil {
		return err
	}

	// the job's pod holds the only record of what the drop reported, so
	// it is cleared only once the job has reported success
	return cpi.deleteDropJob(kubeClient, namespace, dropMessageBrokerJobName, "message broker")
}

// runDropDatabaseJob issues the drop from a one-off job in the control
// plane namespace, waits for it to report success, and removes it. The
// job runs the cockroach client against the running database with the
// same root credentials the installer already keeps in the cluster for
// database initialization.
func (cpi *ControlPlaneInstaller) runDropDatabaseJob(
	kubeClient dynamic.Interface,
	namespace string,
) error {
	// clear a job left behind by an interrupted run: a job's pod
	// template is immutable, so creating over one is refused
	if err := cpi.deleteDropJob(kubeClient, namespace, dropDatabaseJobName, "database"); err != nil {
		return err
	}

	statement := fmt.Sprintf("DROP DATABASE IF EXISTS %s CASCADE", database.ThreeportDatabaseName)
	fmt.Printf("Info: running %q against the database\n", statement)

	job := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "batch/v1",
			"kind":       "Job",
			"metadata": map[string]interface{}{
				"name":      dropDatabaseJobName,
				"namespace": namespace,
			},
			"spec": map[string]interface{}{
				"backoffLimit": int64(dropDatabaseJobBackoffLimit),
				"template": map[string]interface{}{
					"spec": map[string]interface{}{
						"restartPolicy": "Never",
						"containers": []interface{}{
							map[string]interface{}{
								"name":  "drop-database",
								"image": fmt.Sprintf("cockroachdb/cockroach:%s", DatabaseImageTag),
								"command": []interface{}{
									"/cockroach/cockroach",
									"sql",
									fmt.Sprintf("--certs-dir=%s", dbRootCertsMountPath),
									fmt.Sprintf("--host=%s", database.ThreeportDatabaseHost),
									fmt.Sprintf("--port=%s", database.ThreeportDatabasePort),
									"--execute",
									statement,
								},
								"volumeMounts": []interface{}{
									map[string]interface{}{
										"name":      dbRootCertSecretName,
										"mountPath": dbRootCertsMountPath,
									},
								},
							},
						},
						"volumes": []interface{}{
							map[string]interface{}{
								"name": dbRootCertSecretName,
								"secret": map[string]interface{}{
									"secretName":  dbRootCertSecretName,
									"defaultMode": int64(dbRootCertsFileMode),
								},
							},
						},
					},
				},
			},
		},
	}

	if _, err := kubeClient.Resource(jobGVR).Namespace(namespace).Create(
		context.Background(), job, metav1.CreateOptions{},
	); err != nil {
		return fmt.Errorf("failed to create database drop job %s: %w", dropDatabaseJobName, err)
	}

	if err := cpi.waitForDropJob(
		kubeClient, namespace, dropDatabaseJobName, dropDatabaseJobBackoffLimit, "database",
	); err != nil {
		return err
	}

	// the job's pod holds the only record of what the drop reported, so
	// it is cleared only once the job has reported success
	return cpi.deleteDropJob(kubeClient, namespace, dropDatabaseJobName, "database")
}

// waitForDropJob polls a drop job until it reports one successful
// completion, or reports that its pod has failed as many times as the
// job allows. A job that has exhausted its retries will not recover, so
// it is reported straight away rather than waited out. The subject names
// what is being dropped, so the caller's failure reads in its own terms.
func (cpi *ControlPlaneInstaller) waitForDropJob(
	kubeClient dynamic.Interface,
	namespace string,
	jobName string,
	backoffLimit int64,
	subject string,
) error {
	var jobFailed error

	// 3s poll x 60 attempts = 3 minute aggregate deadline.
	if err := util.Retry(60, 3, func() error {
		job, err := kubeClient.Resource(jobGVR).Namespace(namespace).Get(
			context.Background(), jobName, metav1.GetOptions{},
		)
		if err != nil {
			return fmt.Errorf("failed to read %s drop job status: %w", subject, err)
		}

		failed, _, _ := util.NestedInt64OrFloat64(job.Object, "status", "failed")
		if failed > backoffLimit {
			jobFailed = fmt.Errorf(
				"%s drop job %s failed after %d pod attempt(s): inspect its pod logs in namespace %s",
				subject, jobName, failed, namespace,
			)
			return nil
		}

		succeeded, _, _ := util.NestedInt64OrFloat64(job.Object, "status", "succeeded")
		if succeeded > 0 {
			return nil
		}

		return fmt.Errorf("%s drop job %s has not completed", subject, jobName)
	}); err != nil {
		return fmt.Errorf("%s drop did not complete: %w", subject, err)
	}

	return jobFailed
}

// deleteDropJob removes a drop job and waits for it to leave the api.
// The delete cascades to the job's pod, so a completed pod does not
// linger in the control plane namespace.
func (cpi *ControlPlaneInstaller) deleteDropJob(
	kubeClient dynamic.Interface,
	namespace string,
	jobName string,
	subject string,
) error {
	deletePolicy := metav1.DeletePropagationForeground
	deleteOpts := metav1.DeleteOptions{PropagationPolicy: &deletePolicy}

	if err := kubeClient.Resource(jobGVR).Namespace(namespace).Delete(
		context.Background(), jobName, deleteOpts,
	); err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete %s drop job %s: %w", subject, jobName, err)
	}

	// 3s poll x 20 attempts = 1 minute aggregate deadline.
	if err := util.Retry(20, 3, func() error {
		_, err := kubeClient.Resource(jobGVR).Namespace(namespace).Get(
			context.Background(), jobName, metav1.GetOptions{},
		)
		if err == nil {
			return fmt.Errorf("%s drop job %s still terminating", subject, jobName)
		}
		if !k8serrors.IsNotFound(err) {
			return fmt.Errorf("failed to check %s drop job %s: %w", subject, jobName, err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("%s drop job did not finish terminating: %w", subject, err)
	}

	return nil
}

// getInstalledTier reads the tier a control plane was installed with
// from its namespace. It reports an error when the namespace is absent
// or carries no tier, so a caller gating a destructive operation on the
// result never proceeds on a missing value.
func (cpi *ControlPlaneInstaller) getInstalledTier(
	kubeClient dynamic.Interface,
	mapper *meta.RESTMapper,
) (ControlPlaneTier, error) {
	namespace, err := kube.GetResource(
		"", "v1", "Namespace",
		"", cpi.Opts.Namespace,
		kubeClient, *mapper,
	)
	if err != nil {
		return "", fmt.Errorf("failed to read control plane namespace %s: %w", cpi.Opts.Namespace, err)
	}

	tier := namespace.GetLabels()[LabelTier]
	if tier == "" {
		return "", fmt.Errorf(
			"%w: namespace %s records no tier, so it cannot be confirmed as a development installation",
			ErrControlPlaneNotDevelopment, cpi.Opts.Namespace,
		)
	}

	return ControlPlaneTier(tier), nil
}

// Reinstall deletes every installer-managed stateless resource in the
// control plane namespace and then re-runs the install path so the
// pods come back with fresh images and specs. Designed for dev
// environments only; production-safe upgrades are a separate flow.
//
// The control plane's stateful side - cockroachdb, nats, the
// certificate authority, the external load balancer - is preserved by
// the persistent label that those resources carry, so dev users keep
// their database contents and the api endpoint's public ip across
// reinstalls.
//
// Phases: delete -> install -> verify. The delete step scales control
// plane deployments to zero before issuing deletes so in-cluster
// reconcilers (the controllers) don't race by recreating resources we
// just deleted.
func (cpi *ControlPlaneInstaller) Reinstall(
	kubeClient dynamic.Interface,
	mapper *meta.RESTMapper,
	authConfig *auth.AuthConfig,
) error {
	ns := cpi.Opts.Namespace

	if err := cpi.deleteForReinstall(kubeClient, ns); err != nil {
		return fmt.Errorf("failed to delete control plane resources: %w", err)
	}

	// install uses CreateOrUpdate so resources that survived the delete
	// phase (api-ca, api-cert, persistent statefulsets) are patched in
	// place. set the flag explicitly in case the installer was
	// constructed for a fresh install path.
	cpi.Opts.CreateOrUpdateKubeResources = true

	// dependencies first - encryption-key, nats config, crdb config,
	// api load balancer service. existence guards on the encryption
	// secret and db cert secret keep their content intact across this
	// install; the empty arguments below only matter for fresh installs,
	// which reinstall doesn't run.
	fmt.Println("Info: installing threeport control plane dependencies (nats, crdb, encryption-key, api load balancer)")
	if err := cpi.InstallThreeportControlPlaneDependencies(kubeClient, mapper, "", nil); err != nil {
		return fmt.Errorf("failed to install control plane dependencies: %w", err)
	}

	// api-ca and api-cert secrets carry the persistent label and survive the delete; no re-issue needed.

	fmt.Println("Info: installing threeport api deployment")
	if err := cpi.UpdateThreeportAPIDeployment(kubeClient, mapper, nil); err != nil {
		return fmt.Errorf("failed to install threeport api deployment: %w", err)
	}

	fmt.Printf("Info: installing %d threeport controller(s)\n", len(cpi.Opts.ControllerList))
	if err := cpi.InstallThreeportControllers(kubeClient, mapper, authConfig); err != nil {
		return fmt.Errorf("failed to install threeport controllers: %w", err)
	}

	fmt.Println("Info: installing threeport agent")
	if err := cpi.InstallThreeportAgent(kubeClient, mapper, authConfig); err != nil {
		return fmt.Errorf("failed to install threeport agent: %w", err)
	}

	fmt.Printf("Info: waiting for rest-api deployment to become ready (timeout %s)\n", restApiDeploymentReadyTimeout)
	if err := cpi.waitForRestAPIReady(kubeClient, ns, restApiDeploymentReadyTimeout); err != nil {
		return fmt.Errorf("rest-api did not become ready after reinstall: %w", err)
	}

	return nil
}

// deleteForReinstall performs the combined delete phase: scale every
// installer-managed Deployment to zero, wait for pods to drain, then
// foreground-cascade delete every stateless resource matching the
// managed-by label, then wait for the api to report empty across every
// kind.
//
// Reinstall covers any change to deployment spec, including fields
// that are immutable on a running deployment (volume mounts, selectors,
// init containers) - not just the image-swap that `strategy: Recreate`
// handles. Scale to 0 + foreground-cascade delete + recreate means the
// new pods come up against the fresh spec without trying to patch
// through any in-place restriction. Stateful resources (CRDB data,
// NATS data, the CA, rest-api external IP) are deliberately excluded
// from the delete.
func (cpi *ControlPlaneInstaller) deleteForReinstall(
	kubeClient dynamic.Interface,
	namespace string,
) error {
	selector := fmt.Sprintf(
		"%s=%s,%s!=%s",
		LabelManagedBy, LabelManagedByValue,
		LabelPersistent, LabelPersistentValue,
	)

	if err := cpi.scaleDownDeployments(kubeClient, namespace); err != nil {
		return err
	}

	// foreground-cascade delete the stateless resource set. foreground
	// holds the owner in the api until its dependents are gone, so the
	// install step that follows doesn't collide with a still-terminating
	// same-named object.
	fmt.Println("Info: deleting installer-managed stateless resources across deployments, configmaps, secrets, services, serviceaccounts, roles, rolebindings, clusterroles, clusterrolebindings")
	deletePolicy := metav1.DeletePropagationForeground
	deleteOpts := metav1.DeleteOptions{PropagationPolicy: &deletePolicy}

	count := 0
	for _, target := range deleteTargets {
		// cluster-scoped resources (clusterroles, clusterrolebindings)
		// skip the namespace call; namespaced resources pin to the
		// control plane namespace.
		var ri dynamic.ResourceInterface
		if target.namespaced {
			ri = kubeClient.Resource(target.gvr).Namespace(namespace)
		} else {
			ri = kubeClient.Resource(target.gvr)
		}

		list, err := ri.List(context.Background(), metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return fmt.Errorf(
				"failed to list %s matching %q: %w",
				target.gvr.Resource, selector, err,
			)
		}

		for _, obj := range list.Items {
			name := obj.GetName()
			if err := ri.Delete(context.Background(), name, deleteOpts); err != nil && !k8serrors.IsNotFound(err) {
				return fmt.Errorf(
					"failed to delete %s/%s: %w",
					target.gvr.Resource, name, err,
				)
			}
			count++
		}
	}
	fmt.Printf("Info: deleted %d stateless resource(s)\n", count)

	// k8s Delete is async: the call returns once the deletion is
	// initiated, not once the object is gone. wait until the label
	// selector returns empty across every kind so the install step
	// doesn't collide with a still-terminating same-named object.
	//
	// Stuck-mode shape: a single resource hung in termination
	// (finalizer not cleared, SIGTERM handler hanging on in-flight
	// reconcile, unresponsive node) holds the deadline. Many concurrent
	// fast-terminating resources is the expected case, not the worst
	// case.
	var sample string
	if err := util.Retry(60, 3, func() error {
		pending := 0
		sample = ""
		for _, target := range deleteTargets {
			var ri dynamic.ResourceInterface
			if target.namespaced {
				ri = kubeClient.Resource(target.gvr).Namespace(namespace)
			} else {
				ri = kubeClient.Resource(target.gvr)
			}
			list, err := ri.List(context.Background(), metav1.ListOptions{LabelSelector: selector})
			if err != nil {
				return fmt.Errorf("failed to list %s while waiting for delete: %w", target.gvr.Resource, err)
			}
			if n := len(list.Items); n > 0 {
				pending += n
				if sample == "" {
					sample = fmt.Sprintf("%s/%s", target.gvr.Resource, list.Items[0].GetName())
				}
			}
		}
		if pending > 0 {
			return fmt.Errorf("%d resource(s) still terminating (e.g. %s)", pending, sample)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("control plane resources did not finish terminating: %w", err)
	}

	return nil
}

// scaleDownDeployments scales every installer-managed Deployment in the
// namespace to zero and waits for their pods to drain. Both callers
// need the control plane to stop driving state before they act on it:
// the reinstall so in-cluster reconcilers don't recreate resources it
// just deleted, and the database drop so no component is writing to a
// schema being dropped.
func (cpi *ControlPlaneInstaller) scaleDownDeployments(
	kubeClient dynamic.Interface,
	namespace string,
) error {
	selector := fmt.Sprintf(
		"%s=%s,%s!=%s",
		LabelManagedBy, LabelManagedByValue,
		LabelPersistent, LabelPersistentValue,
	)

	fmt.Println("Info: scaling all control plane deployments to 0 and waiting for pods to terminate")
	deployList, err := kubeClient.Resource(deploymentGVR).Namespace(namespace).List(
		context.Background(), metav1.ListOptions{LabelSelector: selector},
	)
	if err != nil {
		return fmt.Errorf("failed to list control plane deployments: %w", err)
	}
	patch := []byte(`{"spec":{"replicas":0}}`)
	for _, dep := range deployList.Items {
		name := dep.GetName()
		_, err := kubeClient.Resource(deploymentGVR).Namespace(namespace).Patch(
			context.Background(),
			name,
			"application/strategic-merge-patch+json",
			patch,
			metav1.PatchOptions{},
		)
		if err != nil && !k8serrors.IsNotFound(err) {
			return fmt.Errorf("failed to scale deployment %s to zero: %w", name, err)
		}
	}

	// 3s poll x 60 attempts = 3 minute aggregate deadline.
	if err := util.Retry(60, 3, func() error {
		current, err := kubeClient.Resource(deploymentGVR).Namespace(namespace).List(
			context.Background(), metav1.ListOptions{LabelSelector: selector},
		)
		if err != nil {
			return fmt.Errorf("failed to list deployments while waiting for scale-down: %w", err)
		}
		pending := 0
		for _, dep := range current.Items {
			ready, _, _ := util.NestedInt64OrFloat64(dep.Object, "status", "readyReplicas")
			if ready > 0 {
				pending += int(ready)
			}
		}
		if pending > 0 {
			return fmt.Errorf("%d ready replica(s) still present", pending)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("control plane deployments did not scale to zero: %w", err)
	}

	return nil
}

// waitForRestAPIReady polls the rest-api deployment until its ready
// replicas reach 1, or returns an error at the deadline.
func (cpi *ControlPlaneInstaller) waitForRestAPIReady(
	kubeClient dynamic.Interface,
	namespace string,
	timeout time.Duration,
) error {
	name := cpi.Opts.RestApiInfo.ServiceResourceName
	// 3s poll x ceil(timeout / 3s) attempts.
	attemptsMax := int(timeout / (3 * time.Second))
	return util.Retry(attemptsMax, 3, func() error {
		dep, err := kubeClient.Resource(deploymentGVR).Namespace(namespace).Get(
			context.Background(), name, metav1.GetOptions{},
		)
		if err != nil && !k8serrors.IsNotFound(err) {
			return fmt.Errorf("failed to read rest-api deployment status: %w", err)
		}
		if err == nil {
			ready, _, _ := util.NestedInt64OrFloat64(dep.Object, "status", "readyReplicas")
			if ready >= 1 {
				return nil
			}
		}
		return fmt.Errorf("rest-api deployment not yet ready")
	})
}

// LoadAuthConfigFromCluster reads the persistent api-ca Secret and
// rebuilds an AuthConfig backed by its cert and private key. Reinstall
// passes the returned config into the install functions so any new
// controller added since the last install gets a cert signed by the
// cluster's existing CA, without rotating it. Existing controllers'
// cert secrets survive the delete and aren't re-issued.
func (cpi *ControlPlaneInstaller) LoadAuthConfigFromCluster(
	kubeClient dynamic.Interface,
	mapper *meta.RESTMapper,
) (*auth.AuthConfig, error) {
	secret, err := kube.GetResource(
		"", "v1", "Secret",
		cpi.Opts.Namespace, ThreeportApiCaSecret,
		kubeClient, *mapper,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load api-ca secret: %w", err)
	}

	// kube secrets store values base64-encoded under data; decode each
	// to get the underlying PEM bytes the cert/key parsers expect.
	caB64, _, err := unstructured.NestedString(secret.Object, "data", "tls.crt")
	if err != nil || caB64 == "" {
		return nil, fmt.Errorf("api-ca secret missing data.tls.crt")
	}
	keyB64, _, err := unstructured.NestedString(secret.Object, "data", "tls.key")
	if err != nil || keyB64 == "" {
		return nil, fmt.Errorf("api-ca secret missing data.tls.key")
	}

	caPem, err := base64.StdEncoding.DecodeString(caB64)
	if err != nil {
		return nil, fmt.Errorf("failed to base64-decode ca cert: %w", err)
	}
	keyPem, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		return nil, fmt.Errorf("failed to base64-decode ca key: %w", err)
	}

	caBlock, _ := pem.Decode(caPem)
	if caBlock == nil {
		return nil, fmt.Errorf("ca cert pem block missing")
	}
	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ca cert: %w", err)
	}

	keyBlock, _ := pem.Decode(keyPem)
	if keyBlock == nil {
		return nil, fmt.Errorf("ca key pem block missing")
	}
	caKey, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ca key: %w", err)
	}

	return &auth.AuthConfig{
		CAConfig:                  caCert,
		CAPrivateKey:              *caKey,
		CA:                        caBlock.Bytes,
		CAPemEncoded:              string(caPem),
		CABase64Encoded:           util.Base64Encode(string(caPem)),
		CAPrivateKeyPemEncoded:    string(keyPem),
		CAPrivateKeyBase64Encoded: util.Base64Encode(string(keyPem)),
	}, nil
}
