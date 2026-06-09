package v0

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

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

	// scale every installer-managed Deployment to zero so in-cluster
	// reconcilers stop driving state before the delete fan-out.
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
		if err != nil && !errors.IsNotFound(err) {
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
			if err := ri.Delete(context.Background(), name, deleteOpts); err != nil && !errors.IsNotFound(err) {
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
		if err != nil && !errors.IsNotFound(err) {
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
