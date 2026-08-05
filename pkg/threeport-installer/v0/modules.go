package v0

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"

	client "github.com/threeport/threeport/pkg/client/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// ModuleDeploymentScale records the replica count a module deployment
// carried before it was scaled down, so the same count can be put back
// once whatever required it to stop is finished.
type ModuleDeploymentScale struct {
	Namespace string
	Name      string
	Replicas  int64
}

// DiscoverModuleNamespaces returns the namespaces holding the modules
// registered with this control plane, read from the control plane's own
// registry rather than from any convention the module has to follow.
//
// Every module registers its controllers on startup, and records each
// one's deployment qualified by the namespace it runs in. That makes the
// registry the authoritative answer to which namespaces belong to
// modules, and it stays correct for a module this code has never heard
// of. The core control plane registers itself the same way and is
// skipped, along with its own namespace, so a module installed beside
// the control plane is never returned as if it were separate.
//
// The registry lives in the database, so a caller that is about to
// destroy the database has to ask first and hold the answer.
func (cpi *ControlPlaneInstaller) DiscoverModuleNamespaces(
	apiClient *http.Client,
	apiEndpoint string,
) ([]string, error) {
	moduleApis, err := client.GetModuleApis(apiClient, apiEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to get registered module APIs: %w", err)
	}

	var namespaces []string
	seen := map[string]bool{cpi.Opts.Namespace: true}

	for _, moduleApi := range *moduleApis {
		// the core API registers itself in the same table; its
		// controllers are the ones the reinstall already manages
		if moduleApi.Core != nil && *moduleApi.Core {
			continue
		}
		if moduleApi.ID == nil {
			continue
		}

		controllers, err := client.GetModuleControllersByQueryString(
			apiClient,
			apiEndpoint,
			fmt.Sprintf("moduleapiid=%d", *moduleApi.ID),
		)
		if err != nil {
			return nil, fmt.Errorf(
				"failed to get controllers registered by module API %d: %w",
				*moduleApi.ID, err,
			)
		}

		for _, controller := range *controllers {
			if controller.DeploymentName == nil {
				continue
			}
			// the deployment is recorded as namespace/name
			namespace, _, qualified := strings.Cut(*controller.DeploymentName, "/")
			if !qualified || namespace == "" {
				continue
			}
			if seen[namespace] {
				continue
			}
			seen[namespace] = true
			namespaces = append(namespaces, namespace)
		}
	}

	return namespaces, nil
}

// ScaleDownModules scales every deployment in the given namespaces to
// zero, waits for their pods to drain, and returns the replica counts
// they were running at. Pass the result to RestoreModuleScale to put
// them back.
//
// Every deployment in the namespace is scaled, not only the controllers
// the registry names. A module's own API server is not registered as a
// controller, and it is the component that reads and writes most, so a
// sweep limited to registered controllers would leave running exactly
// what most needs to stop.
func (cpi *ControlPlaneInstaller) ScaleDownModules(
	kubeClient dynamic.Interface,
	namespaces []string,
) ([]ModuleDeploymentScale, error) {
	var scales []ModuleDeploymentScale

	for _, namespace := range namespaces {
		deployList, err := kubeClient.Resource(deploymentGVR).Namespace(namespace).List(
			context.Background(), metav1.ListOptions{},
		)
		if err != nil {
			return nil, fmt.Errorf("failed to list deployments in module namespace %s: %w", namespace, err)
		}

		for _, deployment := range deployList.Items {
			name := deployment.GetName()
			replicas, _, _ := util.NestedInt64OrFloat64(deployment.Object, "spec", "replicas")
			// a deployment already at zero is left out of the record, so
			// restoring never starts something that was deliberately down
			if replicas == 0 {
				continue
			}

			if err := cpi.setDeploymentReplicas(kubeClient, namespace, name, 0); err != nil {
				return nil, err
			}
			scales = append(scales, ModuleDeploymentScale{
				Namespace: namespace,
				Name:      name,
				Replicas:  replicas,
			})
		}
	}

	if len(scales) == 0 {
		return scales, nil
	}

	fmt.Printf("Info: scaled %d module deployment(s) to 0 across %s\n", len(scales), strings.Join(namespaces, ", "))

	// 3s poll x 60 attempts = 3 minute aggregate deadline.
	if err := util.Retry(60, 3, func() error {
		pending := 0
		for _, namespace := range namespaces {
			current, err := kubeClient.Resource(deploymentGVR).Namespace(namespace).List(
				context.Background(), metav1.ListOptions{},
			)
			if err != nil {
				return fmt.Errorf("failed to list deployments while waiting for module scale-down: %w", err)
			}
			for _, deployment := range current.Items {
				ready, _, _ := util.NestedInt64OrFloat64(deployment.Object, "status", "readyReplicas")
				if ready > 0 {
					pending += int(ready)
				}
			}
		}
		if pending > 0 {
			return fmt.Errorf("%d module replica(s) still present", pending)
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("module deployments did not scale to zero: %w", err)
	}

	return scales, nil
}

// RestoreModuleScale returns each recorded deployment to the replica
// count it was running at. It does not wait for the pods to become
// ready: a module brought back after its schema was dropped runs its own
// migrations on startup, which takes as long as it takes and reports
// itself through the module's own status.
//
// A deployment that has since been removed is skipped rather than
// treated as a failure, so a module uninstalled while the control plane
// was down does not block the ones that are still there.
func (cpi *ControlPlaneInstaller) RestoreModuleScale(
	kubeClient dynamic.Interface,
	scales []ModuleDeploymentScale,
) error {
	if len(scales) == 0 {
		return nil
	}

	for _, scale := range scales {
		if err := cpi.setDeploymentReplicas(
			kubeClient, scale.Namespace, scale.Name, scale.Replicas,
		); err != nil {
			return err
		}
	}

	fmt.Printf("Info: restored %d module deployment(s) to their original replica count\n", len(scales))

	return nil
}

// setDeploymentReplicas patches a deployment's replica count, treating a
// deployment that is no longer there as nothing to do.
func (cpi *ControlPlaneInstaller) setDeploymentReplicas(
	kubeClient dynamic.Interface,
	namespace string,
	name string,
	replicas int64,
) error {
	patch := []byte(fmt.Sprintf(`{"spec":{"replicas":%d}}`, replicas))
	_, err := kubeClient.Resource(deploymentGVR).Namespace(namespace).Patch(
		context.Background(),
		name,
		"application/strategic-merge-patch+json",
		patch,
		metav1.PatchOptions{},
	)
	if err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("failed to scale deployment %s/%s to %d: %w", namespace, name, replicas, err)
	}

	return nil
}
