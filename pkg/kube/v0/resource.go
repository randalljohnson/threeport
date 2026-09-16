package v0

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	yamlv3 "gopkg.in/yaml.v3"
	kubeerr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	kubemetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/yaml"
)

// createRetryAttempts is the number of times a transient kube-apiserver
// error (server-side timeouts, quota-evaluator timeouts, storage stalls) is
// retried before the caller sees the failure. The budget has to outlast the
// stall it absorbs: etcd gives a write several seconds before answering
// "request timed out", so a window shorter than that reports failure while
// the cause is still clearing. Seven attempts over the delays below spans
// roughly 24 seconds, long enough for a storage stall to pass and short
// enough that a genuine misconfiguration does not hide for minutes.
const createRetryAttempts = 7

// createRetryBaseDelay backs off exponentially between retries starting
// from this base (500ms, 1s, 2s, 4s, 8s) so the apiserver has a chance to
// catch up before the next attempt. It is a variable rather than a constant
// so tests can shrink the wait.
var createRetryBaseDelay = 500 * time.Millisecond

// createRetryMaxDelay caps the exponential backoff so the last attempts stay
// a fixed interval apart instead of doubling past the useful range.
const createRetryMaxDelay = 8 * time.Second

// transientStorageMessages are the kube-apiserver 500 bodies that name a
// condition which clears on its own. kube-apiserver passes a storage error it
// does not recognize straight through, so these arrive as a generic internal
// error whose reason says nothing; the message is the only thing that
// distinguishes them from a deterministic 500.
//
// Every entry describes a write that either has not been committed or cannot
// be confirmed, which makes it safe to send again: a create that did commit
// before timing out answers AlreadyExists on the next attempt, and no caller
// in this repository uses generateName, so a resend cannot produce a
// duplicate under a different name.
var transientStorageMessages = []string{
	"resource quota evaluation timed out",
	"etcdserver: request timed out",
	"etcdserver: leader changed",
	"etcdserver: no leader",
	"etcdserver: too many requests",
}

// isTransientKubeError reports whether a Kubernetes API error is worth
// retrying. Covers server-side timeouts, temporary service unavailable
// responses, and the internal-error surfaces listed in
// transientStorageMessages. Every other error (AlreadyExists, NotFound,
// Forbidden, Invalid) is deterministic and should not be retried.
func isTransientKubeError(err error) bool {
	if err == nil {
		return false
	}
	if kubeerr.IsServerTimeout(err) || kubeerr.IsTimeout(err) || kubeerr.IsServiceUnavailable(err) || kubeerr.IsTooManyRequests(err) {
		return true
	}
	if !kubeerr.IsInternalError(err) {
		return false
	}
	for _, msg := range transientStorageMessages {
		if strings.Contains(err.Error(), msg) {
			return true
		}
	}
	return false
}

// GetResource returns a specific Kubernetes resource.  If an empty string for
// namespace is provided, this function will search for a non-namespaced
// resource.  Namespaced resources must have the namespace provided, even if in
// the "default" namespace.  Core resources should provide "core" or  an empty
// string for kubeAPIGroup.
func GetResource(
	kubeAPIGroup string,
	kubeAPIVersion string,
	kubeKind string,
	namespace string,
	resourceName string,
	kubeClient dynamic.Interface,
	mapper meta.RESTMapper,
) (*unstructured.Unstructured, error) {
	// map the resource kind
	var gvk schema.GroupVersionKind
	if kubeAPIGroup == "" || kubeAPIGroup == "core" {
		gvk = schema.GroupVersionKind{Version: kubeAPIVersion, Kind: kubeKind}
	} else {
		gvk = schema.GroupVersionKind{Group: kubeAPIGroup, Version: kubeAPIVersion, Kind: kubeKind}
	}
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, fmt.Errorf("failed to map kubernetes API version and kind: %w", err)
	}

	// create a resource client using the mapping
	var resourceClient dynamic.ResourceInterface
	if namespace == "" {
		resourceClient = kubeClient.Resource(mapping.Resource)
	} else {
		resourceClient = kubeClient.Resource(mapping.Resource).Namespace(namespace)
	}

	// get the resource
	resource, err := resourceClient.Get(context.Background(), resourceName, kubemetav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get resource from kubernetes API: %w", err)
	}

	return resource, nil
}

// CreateResource takes an unstructured object, dynamic client interface and rest
// mapper and creates the resource in the target Kubernetes cluster.  If the
// object already exists, it returns the object.
func CreateResource(
	kubeObject *unstructured.Unstructured,
	kubeClient dynamic.Interface,
	mapper meta.RESTMapper,
) (*unstructured.Unstructured, error) {
	// get the mapping for resource from kube object's group, kind
	mapping, err := getResourceMapping(kubeObject, mapper)
	if err != nil {
		return nil, fmt.Errorf("failed to get REST mapping for kubernetes resource: %w", err)
	}

	// create the kube resource, retrying transient apiserver errors
	// (quota-evaluator timeout, server-side timeouts, storage stalls) so a
	// busy control plane during genesis bring-up does not abort the whole
	// install.
	var result *unstructured.Unstructured
	err = retryTransient(func() error {
		var createErr error
		result, createErr = kubeClient.
			Resource(mapping.Resource).
			Namespace(kubeObject.GetNamespace()).
			Create(context.Background(), kubeObject, kubemetav1.CreateOptions{})
		if kubeerr.IsAlreadyExists(createErr) {
			result = kubeObject
			return nil
		}
		return createErr
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes resource:%w", err)
	}
	return result, nil
}

// retryTransient runs op until it succeeds, until it fails with an error no
// amount of waiting fixes, or until the attempt budget is spent, backing off
// exponentially between tries. The budget-exhausted error names the attempt
// count so a failure that outlasted the window reads differently from one
// the predicate never recognized as retriable.
func retryTransient(op func() error) error {
	var err error
	delay := createRetryBaseDelay
	for attempt := 0; attempt < createRetryAttempts; attempt++ {
		err = op()
		if err == nil {
			return nil
		}
		if !isTransientKubeError(err) {
			return err
		}
		if attempt < createRetryAttempts-1 {
			time.Sleep(delay)
			if delay *= 2; delay > createRetryMaxDelay {
				delay = createRetryMaxDelay
			}
		}
	}
	return fmt.Errorf("gave up after %d attempts: %w", createRetryAttempts, err)
}

// CreateOrUpdateResource takes an unstructured object, dynamic client interface and rest
// mapper and creates the resource in the target Kubernetes cluster if it doesn't already
// exist.  If the resource exists, it is updated.
func CreateOrUpdateResource(
	kubeObject *unstructured.Unstructured,
	kubeClient dynamic.Interface,
	mapper meta.RESTMapper,
) (*unstructured.Unstructured, error) {
	// get the mapping for resource from kube object's group, kind
	mapping, err := getResourceMapping(kubeObject, mapper)
	if err != nil {
		return nil, fmt.Errorf("failed to get REST mapping for kubernetes resource: %w", err)
	}

	// create the kube resource, retrying the same transient conditions
	// CreateResource rides out. This path carries reinstalls and child
	// control plane installs, which run against a control plane already
	// under load, so it meets those stalls more often rather than less.
	var result *unstructured.Unstructured
	err = retryTransient(func() error {
		var createErr error
		result, createErr = kubeClient.
			Resource(mapping.Resource).
			Namespace(kubeObject.GetNamespace()).
			Create(context.TODO(), kubeObject, kubemetav1.CreateOptions{})
		return createErr
	})
	if err != nil {

		// if the resource already exists, update it

		switch {
		case kubeerr.IsAlreadyExists(err):
			if result, err = UpdateResource(kubeObject, kubeClient, mapper, mapping); err != nil {
				return nil, fmt.Errorf("failed to update kubernetes resource:%w", err)
			}

		// If the resource is an existing service and its nodeport is already configured, the
		// kube API will return an IsInvalid error instead of an IsAlreadyExists error.
		// If the service is not already created and is also invalid, then an error should
		// be thrown by UpdateResource.
		case kubeerr.IsInvalid(err) &&
			mapping.GroupVersionKind.Kind == "Service":
			if result, err = UpdateResource(kubeObject, kubeClient, mapper, mapping); err != nil {
				return nil, fmt.Errorf("failed to update kubernetes resource:%w", err)
			}
		default:
			return nil, fmt.Errorf("failed to create kubernetes resource:%w", err)
		}
	}

	return result, nil
}

// UpdateResource updates a Kubernetes resource.
func UpdateResource(
	kubeObject *unstructured.Unstructured,
	kubeClient dynamic.Interface,
	mapper meta.RESTMapper,
	mapping *meta.RESTMapping,
) (*unstructured.Unstructured, error) {

	// get the existing resource
	existingResource, err := GetResource(
		kubeObject.GroupVersionKind().Group,
		kubeObject.GroupVersionKind().Version,
		kubeObject.GroupVersionKind().Kind,
		kubeObject.GetNamespace(),
		kubeObject.GetName(),
		kubeClient,
		mapper,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get existing resource: %w", err)
	}

	// set the resource version
	kubeObject.SetResourceVersion(existingResource.GetResourceVersion())

	// update the resource
	result, err := kubeClient.
		Resource(mapping.Resource).
		Namespace(kubeObject.GetNamespace()).
		Update(context.TODO(), kubeObject, kubemetav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to update kubernetes resource:%w", err)
	}

	return result, nil
}

// DeleteResource takes an unstructured object, dynamic client interface and rest
// mapper and deletes the resource in the target Kubernetes cluster.
//
// A missing CRD on the target cluster, meaning no REST mapping for the
// object's GroupKind, counts as already deleted: an unregistered kind cannot
// have instances, so there is nothing left to remove. That matches how a
// NotFound on the delete call itself is treated, and together they keep a
// delete reconciler from retrying forever on a resource that has nowhere to
// live.
func DeleteResource(
	kubeObject *unstructured.Unstructured,
	kubeClient dynamic.Interface,
	mapper meta.RESTMapper,
) error {
	// get the mapping for resource from kube object's group, kind
	mapping, err := getResourceMapping(kubeObject, mapper)
	if err != nil {
		if meta.IsNoMatchError(err) {
			return nil
		}
		return fmt.Errorf("failed to get REST mapping for kubernetes resource: %w", err)
	}

	// delete the kube resource
	err = kubeClient.
		Resource(mapping.Resource).
		Namespace(kubeObject.GetNamespace()).
		Delete(context.Background(), kubeObject.GetName(), kubemetav1.DeleteOptions{})
	if err != nil && !kubeerr.IsNotFound(err) {
		return fmt.Errorf("failed to delete kubernetes resource:%w", err)
	}

	return nil
}

// DeletePod deletes a pod.
func DeletePod(
	kubeClient dynamic.Interface,
	mapper *meta.RESTMapper,
	name,
	namespace string,
) error {

	// initiate namespace deletion
	pod := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
			},
		},
	}

	// delete the pod
	// get the mapping for resource from kube object's group, kind
	mapping, err := getResourceMapping(pod, *mapper)
	if err != nil {
		return fmt.Errorf("failed to get REST mapping for kubernetes resource: %w", err)
	}

	// Define your label selector here
	labelSelector := kubemetav1.LabelSelector{
		MatchLabels: map[string]string{
			"app.kubernetes.io/name": fmt.Sprintf("threeport-%s", name),
		},
	}

	// convert label selector to string
	selector := labels.Set(labelSelector.MatchLabels).String()

	// list all resources matching the label selector
	resourceList, err := kubeClient.
		Resource(mapping.Resource).
		Namespace(pod.GetNamespace()).
		List(
			context.Background(),
			kubemetav1.ListOptions{LabelSelector: selector},
		)
	if err != nil {
		return fmt.Errorf("failed to list kubernetes resources: %w", err)
	}

	// delete the kube resource
	for _, resource := range resourceList.Items {
		err = kubeClient.
			Resource(mapping.Resource).
			Namespace(pod.GetNamespace()).
			Delete(context.Background(), resource.GetName(), kubemetav1.DeleteOptions{})
		if err != nil && !kubeerr.IsNotFound(err) {
			return fmt.Errorf("failed to delete kubernetes resource:%w", err)
		}
	}

	return nil
}

// DeleteLabelledPodsInNamespace takes a namespace, set of labels, kube client
// and mapper and deletes all the pods.
func DeleteLabelledPodsInNamespace(
	namespace string,
	labels map[string]string,
	restConfig *rest.Config,
) error {
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to generate Kubernete clientset from REST config: %w", err)
	}

	var labelSelectorSlice []string
	for k, v := range labels {
		labelSelectorSlice = append(labelSelectorSlice, fmt.Sprintf("%s=%s", k, v))
	}
	labelSelectors := strings.Join(labelSelectorSlice, ",")

	pods, err := clientset.CoreV1().Pods(namespace).List(context.Background(), kubemetav1.ListOptions{
		LabelSelector: labelSelectors,
	})
	if err != nil {
		return fmt.Errorf("failed get pods in namespace %s with desired labels: %w", namespace, err)
	}

	for _, pod := range pods.Items {
		err := clientset.CoreV1().Pods(namespace).Delete(context.Background(), pod.Name, kubemetav1.DeleteOptions{})
		if err != nil {
			return fmt.Errorf("failed to delete pod %s: %w", pod.Name, err)
		}
	}

	return nil
}

// GetJsonResourcesFromYamlDoc takes a YAML document with any number of
// Kubernetes resources defined and returns a slice of JSON objects as byte
// arrays.
func GetJsonResourcesFromYamlDoc(yamlDoc string) ([][]byte, error) {
	decoder := yamlv3.NewDecoder(strings.NewReader(yamlDoc))

	var jsonObjects [][]byte
	for {
		// decode the next resource, exit loop if the end has been reached
		var node yamlv3.Node
		err := decoder.Decode(&node)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return jsonObjects, fmt.Errorf("failed to decode yaml node in kubernetes workload definition: %w", err)
		}

		// marshal the yaml
		yamlContent, err := yamlv3.Marshal(&node)
		if err != nil {
			return jsonObjects, fmt.Errorf("failed to marshal yaml from kubernetes workload definition: %w", err)
		}

		// convert yaml to json
		jsonContent, err := yaml.YAMLToJSON(yamlContent)
		if err != nil {
			return jsonObjects, fmt.Errorf("failed to convert yaml to json: %w", err)
		}

		jsonObjects = append(jsonObjects, jsonContent)
	}

	return jsonObjects, nil
}

// getResourceMapping gets the REST mapping for a given unstructured Kubernetes
// object.
func getResourceMapping(kubeObject *unstructured.Unstructured, mapper meta.RESTMapper) (*meta.RESTMapping, error) {
	gk := schema.GroupKind{
		Group: kubeObject.GroupVersionKind().Group,
		Kind:  kubeObject.GetKind(),
	}
	mapping, err := mapper.RESTMapping(gk)
	if err != nil {
		return nil, fmt.Errorf("failed to map kube object group kind to resource: %w", err)
	}

	return mapping, nil
}
