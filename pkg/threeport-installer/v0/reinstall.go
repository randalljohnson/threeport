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

// deploymentGVR is reused by the quiesce and verify phases (which
// target the rest-api Deployment by name) and by the sweep target
// set (which lists matching Deployments by label).
var deploymentGVR = schema.GroupVersionResource{
	Group:    "apps",
	Version:  "v1",
	Resource: "deployments",
}

// sweepTarget pairs a GVR with whether it's namespace-scoped. Cluster
// -scoped kinds (ClusterRole, ClusterRoleBinding) skip the .Namespace
// call on the dynamic client.
type sweepTarget struct {
	gvr        schema.GroupVersionResource
	namespaced bool
}

// sweepTargets is the closed set of kinds the reinstall sweep walks.
// Each kind that the installer can create is listed here; resources
// whose loss is unrecoverable (cascade-delete, data, external state)
// are deliberately excluded:
//
//	customresourcedefinitions - cascade-deletes every cr of that type
//	namespaces                - cascade-deletes everything in the ns
//	persistentvolumeclaims    - data loss
//	statefulsets              - the persistent label protects cockroach
//	                            and nats specifically; no other
//	                            installer-managed statefulset exists
var sweepTargets = []sweepTarget{
	{deploymentGVR, true},
	{schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}, true},
	{schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"}, true},
	{schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"}, true},
	{schema.GroupVersionResource{Group: "", Version: "v1", Resource: "serviceaccounts"}, true},
	{schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "roles"}, true},
	{schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "rolebindings"}, true},
	{schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles"}, false},
	{schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterrolebindings"}, false},
}

// restApiDeploymentReadyTimeout caps how long Reinstall will wait for
// the rest-api deployment to roll back to ready after the reapply.
const restApiDeploymentReadyTimeout = 5 * time.Minute

// Reinstall sweeps every installer-managed stateless deployment in the
// control plane namespace and then reapplies the install path so the
// pods come back with fresh images and specs. Designed for dev
// environments only; production-safe upgrades are a separate flow.
//
// The control plane's stateful side - cockroachdb, nats, the
// certificate authority, the external load balancer - is preserved by
// the persistent label that those resources carry, so dev users keep
// their database contents and the api endpoint's public ip across
// reinstalls.
//
// Phases: quiesce -> sweep -> reapply -> verify. The quiesce step
// scales the rest-api to zero before sweeping so in-cluster
// reconcilers (the controllers) don't race the sweep by recreating
// resources we just deleted.
func (cpi *ControlPlaneInstaller) Reinstall(
	kubeClient dynamic.Interface,
	mapper *meta.RESTMapper,
	authConfig *auth.AuthConfig,
) error {
	ns := cpi.Opts.Namespace

	fmt.Println("Info: scaling rest-api to 0 and waiting for pods to terminate")
	if err := cpi.quiesceForReinstall(kubeClient, ns); err != nil {
		return fmt.Errorf("failed to quiesce control plane for reinstall: %w", err)
	}

	fmt.Println("Info: sweeping installer-managed stateless resources across deployments, configmaps, secrets, services, serviceaccounts, roles, rolebindings, clusterroles, clusterrolebindings")
	deleted, err := cpi.sweepReinstallTargets(kubeClient, ns)
	if err != nil {
		return fmt.Errorf("failed to sweep stateless resources: %w", err)
	}
	fmt.Printf("Info: swept %d stateless resource(s)\n", deleted)

	// reapply uses CreateOrUpdate so resources that survived the sweep
	// (configmaps, secrets, rbac, services) are patched in place. set
	// the flag explicitly in case the installer was constructed for a
	// fresh install path.
	cpi.Opts.CreateOrUpdateKubeResources = true

	// dependencies first - encryption-key, nats config, crdb config,
	// api load balancer service. existence guards on the encryption
	// secret and db cert secret keep their content intact across this
	// reapply; the empty arguments below only matter for fresh installs,
	// which reinstall doesn't run.
	fmt.Println("Info: reapplying threeport control plane dependencies (nats, crdb, encryption-key, api load balancer)")
	if err := cpi.InstallThreeportControlPlaneDependencies(kubeClient, mapper, "", nil); err != nil {
		return fmt.Errorf("failed to reapply control plane dependencies: %w", err)
	}

	// api server tls assets: pass the cluster-loaded authConfig so
	// any cert secret that doesn't exist yet (e.g. a brand-new
	// controller added since the last install) gets signed by the
	// existing CA. existing cert secrets survive the sweep and are
	// reused as-is - install functions check existence before
	// re-issuing.
	fmt.Println("Info: ensuring threeport api tls assets")
	if err := cpi.InstallThreeportAPITLS(kubeClient, mapper, authConfig); err != nil {
		return fmt.Errorf("failed to reapply threeport api tls assets: %w", err)
	}

	fmt.Println("Info: reapplying threeport api deployment")
	if err := cpi.UpdateThreeportAPIDeployment(kubeClient, mapper, nil); err != nil {
		return fmt.Errorf("failed to reapply threeport api deployment: %w", err)
	}

	fmt.Printf("Info: reapplying %d threeport controller(s)\n", len(cpi.Opts.ControllerList))
	if err := cpi.InstallThreeportControllers(kubeClient, mapper, authConfig); err != nil {
		return fmt.Errorf("failed to reapply threeport controllers: %w", err)
	}

	fmt.Println("Info: reapplying threeport agent")
	if err := cpi.InstallThreeportAgent(kubeClient, mapper, authConfig); err != nil {
		return fmt.Errorf("failed to reapply threeport agent: %w", err)
	}

	fmt.Printf("Info: waiting for rest-api deployment to become ready (timeout %s)\n", restApiDeploymentReadyTimeout)
	if err := cpi.waitForRestAPIReady(kubeClient, ns, restApiDeploymentReadyTimeout); err != nil {
		return fmt.Errorf("rest-api did not become ready after reinstall: %w", err)
	}

	return nil
}

// quiesceForReinstall scales the rest-api deployment to zero and
// waits for its pods to fully terminate. Controllers can't drive
// reconciliation without the api, so this stops them from racing
// the sweep.
func (cpi *ControlPlaneInstaller) quiesceForReinstall(
	kubeClient dynamic.Interface,
	namespace string,
) error {
	name := cpi.Opts.RestApiInfo.ServiceResourceName

	patch := []byte(`{"spec":{"replicas":0}}`)
	_, err := kubeClient.Resource(deploymentGVR).Namespace(namespace).Patch(
		context.Background(),
		name,
		"application/strategic-merge-patch+json",
		patch,
		metav1.PatchOptions{},
	)
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to scale rest-api deployment to zero: %w", err)
	}

	// poll until the deployment reports zero ready replicas. nothing
	// downstream needs the pods to be fully gone (they're already not
	// serving traffic once ready=0), so this stays simple.
	deadline := time.Now().Add(90 * time.Second)
	for {
		dep, getErr := kubeClient.Resource(deploymentGVR).Namespace(namespace).Get(
			context.Background(), name, metav1.GetOptions{},
		)
		if errors.IsNotFound(getErr) {
			return nil
		}
		if getErr != nil {
			return fmt.Errorf("failed to read rest-api deployment status: %w", getErr)
		}
		ready, _, _ := unstructuredNestedInt(dep.Object, "status", "readyReplicas")
		if ready == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("rest-api deployment still has %d ready replica(s) after quiesce timeout", ready)
		}
		time.Sleep(2 * time.Second)
	}
}

// sweepReinstallTargets deletes every resource matching the
// managed-by=threeport-installer label across the closed sweepTargets
// set, skipping anything marked persistent, then blocks until the
// deletions actually clear from the api. Returns the count of
// deletions issued.
//
// Uses background cascade so the owner (deployment, clusterrolebinding
// etc.) leaves the api on first call and the same-named reapply that
// follows doesn't collide with a still-terminating object. Pods owned
// by deleted deployments are gc'd asynchronously - the new deployment
// created right after gets its own replicaset + pods.
func (cpi *ControlPlaneInstaller) sweepReinstallTargets(
	kubeClient dynamic.Interface,
	namespace string,
) (int, error) {
	selector := fmt.Sprintf(
		"%s=%s,%s!=%s",
		LabelManagedBy, LabelManagedByValue,
		LabelPersistent, LabelPersistentValue,
	)

	deletePolicy := metav1.DeletePropagationBackground
	deleteOpts := metav1.DeleteOptions{PropagationPolicy: &deletePolicy}

	count := 0
	for _, target := range sweepTargets {
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
			return count, fmt.Errorf(
				"failed to list %s matching %q: %w",
				target.gvr.Resource, selector, err,
			)
		}

		for _, obj := range list.Items {
			name := obj.GetName()
			if err := ri.Delete(context.Background(), name, deleteOpts); err != nil && !errors.IsNotFound(err) {
				return count, fmt.Errorf(
					"failed to delete %s/%s: %w",
					target.gvr.Resource, name, err,
				)
			}
			count++
		}
	}

	// k8s Delete is async: the call returns once the deletion is
	// initiated, not once the object is gone. wait until the label
	// selector returns empty across every swept kind, so the reapply
	// step doesn't collide with a still-terminating same-named object.
	if err := cpi.waitForSweepComplete(kubeClient, namespace, selector); err != nil {
		return count, fmt.Errorf("sweep deletions did not clear: %w", err)
	}

	return count, nil
}

// waitForSweepComplete polls the sweep target set until every kind
// reports zero label-matching resources, or the deadline elapses.
// Worst-case shape: many controllers being torn down at once, each
// with a single replica, all clearing well within the deadline.
func (cpi *ControlPlaneInstaller) waitForSweepComplete(
	kubeClient dynamic.Interface,
	namespace string,
	selector string,
) error {
	deadline := time.Now().Add(2 * time.Minute)
	for {
		pending := 0
		var sample string
		for _, target := range sweepTargets {
			var ri dynamic.ResourceInterface
			if target.namespaced {
				ri = kubeClient.Resource(target.gvr).Namespace(namespace)
			} else {
				ri = kubeClient.Resource(target.gvr)
			}
			list, err := ri.List(context.Background(), metav1.ListOptions{LabelSelector: selector})
			if err != nil {
				return fmt.Errorf("failed to list %s while waiting for sweep: %w", target.gvr.Resource, err)
			}
			if n := len(list.Items); n > 0 {
				pending += n
				if sample == "" {
					sample = fmt.Sprintf("%s/%s", target.gvr.Resource, list.Items[0].GetName())
				}
			}
		}
		if pending == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%d resource(s) still terminating after 2m (e.g. %s)", pending, sample)
		}
		time.Sleep(2 * time.Second)
	}
}

// waitForRestAPIReady polls the rest-api deployment until its ready
// replicas reach 1, or returns an error at the deadline.
func (cpi *ControlPlaneInstaller) waitForRestAPIReady(
	kubeClient dynamic.Interface,
	namespace string,
	timeout time.Duration,
) error {
	name := cpi.Opts.RestApiInfo.ServiceResourceName
	deadline := time.Now().Add(timeout)
	for {
		dep, err := kubeClient.Resource(deploymentGVR).Namespace(namespace).Get(
			context.Background(), name, metav1.GetOptions{},
		)
		if err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to read rest-api deployment status: %w", err)
		}
		if err == nil {
			ready, _, _ := unstructuredNestedInt(dep.Object, "status", "readyReplicas")
			if ready >= 1 {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("rest-api deployment not ready after %s", timeout)
		}
		time.Sleep(3 * time.Second)
	}
}

// LoadAuthConfigFromCluster reads the persistent api-ca Secret and
// rebuilds an AuthConfig backed by its cert and private key. Reinstall
// passes the returned config into the install functions so any new
// controller added since the last install gets a cert signed by the
// cluster's existing CA, without rotating it. Existing controllers'
// cert secrets survive the sweep and aren't re-issued.
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

// unstructuredNestedInt fetches a nested int field from an
// unstructured object's map, returning zero+false when missing. Wraps
// the json.Number / float64 numeric ambiguity gorm and the kube
// dynamic client produce.
func unstructuredNestedInt(obj map[string]interface{}, fields ...string) (int64, bool, error) {
	cur := obj
	for i, f := range fields {
		v, ok := cur[f]
		if !ok {
			return 0, false, nil
		}
		if i == len(fields)-1 {
			switch n := v.(type) {
			case int64:
				return n, true, nil
			case float64:
				return int64(n), true, nil
			default:
				return 0, false, fmt.Errorf("field %q has unexpected type %T", f, v)
			}
		}
		next, ok := v.(map[string]interface{})
		if !ok {
			return 0, false, nil
		}
		cur = next
	}
	return 0, false, nil
}
