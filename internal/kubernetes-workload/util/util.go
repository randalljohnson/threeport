package util

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	v0 "github.com/threeport/threeport/pkg/api/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// UnmarshalUniqueKubernetesWorkloadResourceInstance gets a unique kubernetes
// workload resource instance and unmarshals it.
func UnmarshalUniqueKubernetesWorkloadResourceInstance(workloadResourceInstances *[]v0.KubernetesWorkloadResourceInstance, kind string) (map[string]interface{}, error) {

	// filter out service objects
	workloadResourceInstance, err := GetUniqueKubernetesWorkloadResourceInstance(workloadResourceInstances, kind)
	if err != nil {
		return nil, fmt.Errorf("failed to get kubernetes workload resource instances from kubernetes workload instance: %w", err)
	}

	// unmarshal service object
	service, err := util.UnmarshalJSON(*workloadResourceInstance.JSONDefinition)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal kubernetes workload resource instance object: %w", err)
	}

	return service, nil
}

// UnmarshalUniqueKubernetesWorkloadResourceDefinition gets a unique kubernetes
// workload resource definition and unmarshals it.
func UnmarshalUniqueKubernetesWorkloadResourceDefinition(workloadResourceDefinitions *[]v0.KubernetesWorkloadResourceDefinition, kind string) (map[string]interface{}, error) {

	// filter out service objects
	workloadResourceDefinition, err := GetUniqueKubernetesWorkloadResourceDefinition(workloadResourceDefinitions, kind)
	if err != nil {
		return nil, fmt.Errorf("failed to get kubernetes workload resource definitions from kubernetes workload definition: %w", err)
	}

	// unmarshal service object
	service, err := util.UnmarshalJSON(*workloadResourceDefinition.JSONDefinition)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal kubernetes workload resource definition object: %w", err)
	}

	return service, nil
}

// UnmarshalUniqueKubernetesWorkloadResourceDefinitionByName gets a unique
// kubernetes workload resource definition by name and unmarshals it.
func UnmarshalUniqueKubernetesWorkloadResourceDefinitionByName(workloadResourceDefinitions *[]v0.KubernetesWorkloadResourceDefinition, kind, name string) (map[string]interface{}, error) {

	// filter out service objects
	workloadResourceDefinition, err := GetUniqueKubernetesWorkloadResourceDefinitionByName(workloadResourceDefinitions, kind, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get kubernetes workload resource definitions from kubernetes workload definition: %w", err)
	}

	// unmarshal service object
	service, err := util.UnmarshalJSON(*workloadResourceDefinition.JSONDefinition)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal kubernetes workload resource definition object: %w", err)
	}

	return service, nil
}

// UnmarshalKubernetesWorkloadResourceDefinition gets a kubernetes workload
// resource definition by kind and name and unmarshals it.
func UnmarshalKubernetesWorkloadResourceDefinition(workloadResourceDefinitions *[]v0.KubernetesWorkloadResourceDefinition, kind, name string) (map[string]interface{}, error) {

	// filter out service objects
	workloadResourceDefinition, err := GetKubernetesWorkloadResourceDefinition(workloadResourceDefinitions, kind, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get kubernetes workload resource definitions from kubernetes workload definition: %w", err)
	}

	// unmarshal service object
	service, err := util.UnmarshalJSON(*workloadResourceDefinition.JSONDefinition)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal kubernetes workload resource definition object: %w", err)
	}

	return service, nil
}

// UnmarshalKubernetesWorkloadResourceInstance gets a kubernetes workload
// resource instance by kind and name and unmarshals it.
func UnmarshalKubernetesWorkloadResourceInstance(workloadResourceInstances *[]v0.KubernetesWorkloadResourceInstance, kind, name string) (map[string]interface{}, error) {

	// filter out service objects
	workloadResourceInstance, err := GetKubernetesWorkloadResourceInstance(workloadResourceInstances, kind, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get kubernetes workload resource instances from kubernetes workload instance: %w", err)
	}

	// unmarshal service object
	service, err := util.UnmarshalJSON(*workloadResourceInstance.JSONDefinition)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal kubernetes workload resource instance object: %w", err)
	}

	return service, nil
}

// GetUniqueKubernetesWorkloadResourceInstance gets a unique kubernetes workload
// resource instance.
func GetUniqueKubernetesWorkloadResourceInstance(workloadResourceInstances *[]v0.KubernetesWorkloadResourceInstance, kind string) (*v0.KubernetesWorkloadResourceInstance, error) {

	var objects []v0.KubernetesWorkloadResourceInstance
	for _, wri := range *workloadResourceInstances {

		mapDef, err := util.UnmarshalJSON(*wri.JSONDefinition)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal json: %w", err)
		}

		if mapDef["kind"] == kind {
			objects = append(objects, wri)
		}
	}

	if len(objects) == 0 {
		return nil, fmt.Errorf("kubernetes workload resource instance not found")
	}
	if len(objects) > 1 {
		return nil, fmt.Errorf("multiple kubernetes workload resource instances found")
	}

	return &objects[0], nil
}

// GetUniqueKubernetesWorkloadResourceInstanceByName gets a unique kubernetes
// workload resource instance by name.
func GetUniqueKubernetesWorkloadResourceInstanceByName(workloadResourceInstances *[]v0.KubernetesWorkloadResourceInstance, kind, name string) (*v0.KubernetesWorkloadResourceInstance, error) {

	var objects []v0.KubernetesWorkloadResourceInstance
	for _, wri := range *workloadResourceInstances {

		mapDef, err := util.UnmarshalJSON(*wri.JSONDefinition)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal json: %w", err)
		}

		// get object kind
		manifestKind, kindFound, err := unstructured.NestedString(mapDef, "kind")
		if err != nil {
			return nil, fmt.Errorf("failed to get kind: %w", err)
		}
		if !kindFound {
			return nil, fmt.Errorf("kind not found")
		}

		// get object name
		manifestName, nameFound, err := unstructured.NestedString(mapDef, "metadata", "name")
		if err != nil {
			return nil, fmt.Errorf("failed to get name: %w", err)
		}
		if !nameFound {
			return nil, fmt.Errorf("name not found")
		}

		if manifestKind == kind &&
			manifestName == name {
			objects = append(objects, wri)
		}
	}

	if len(objects) == 0 {
		return nil, fmt.Errorf("kubernetes workload resource instance not found")
	}
	if len(objects) > 1 {
		return nil, fmt.Errorf("multiple kubernetes workload resource instances found")
	}

	return &objects[0], nil
}

// GetUniqueKubernetesWorkloadResourceDefinition gets a unique kubernetes
// workload resource definition.
func GetUniqueKubernetesWorkloadResourceDefinition(workloadResourceDefinitions *[]v0.KubernetesWorkloadResourceDefinition, kind string) (*v0.KubernetesWorkloadResourceDefinition, error) {

	var objects []v0.KubernetesWorkloadResourceDefinition
	for _, wrd := range *workloadResourceDefinitions {

		mapDef, err := util.UnmarshalJSON(*wrd.JSONDefinition)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal json: %w", err)
		}

		if mapDef["kind"] == kind {
			objects = append(objects, wrd)
		}
	}

	if len(objects) == 0 {
		return nil, fmt.Errorf("kubernetes workload resource definition not found")
	}
	if len(objects) > 1 {
		return nil, fmt.Errorf("multiple kubernetes workload resource definitions found")
	}

	return &objects[0], nil

}

// GetUniqueKubernetesWorkloadResourceDefinitionByName gets a unique kubernetes
// workload resource definition by name.
func GetUniqueKubernetesWorkloadResourceDefinitionByName(workloadResourceDefinitions *[]v0.KubernetesWorkloadResourceDefinition, kind, name string) (*v0.KubernetesWorkloadResourceDefinition, error) {

	var objects []v0.KubernetesWorkloadResourceDefinition
	for _, wrd := range *workloadResourceDefinitions {

		mapDef, err := util.UnmarshalJSON(*wrd.JSONDefinition)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal json: %w", err)
		}

		// get object kind
		manifestKind, kindFound, err := unstructured.NestedString(mapDef, "kind")
		if err != nil {
			return nil, fmt.Errorf("failed to get kind: %w", err)
		}
		if !kindFound {
			return nil, fmt.Errorf("kind not found")
		}

		// get object name
		manifestName, nameFound, err := unstructured.NestedString(mapDef, "metadata", "name")
		if err != nil {
			return nil, fmt.Errorf("failed to get name: %w", err)
		}
		if !nameFound {
			return nil, fmt.Errorf("name not found")
		}

		if manifestKind == kind &&
			manifestName == name {
			objects = append(objects, wrd)
		}
	}

	if len(objects) == 0 {
		return nil, fmt.Errorf("kubernetes workload resource definition not found")
	}
	if len(objects) > 1 {
		return nil, fmt.Errorf("multiple kubernetes workload resource definitions found")
	}

	return &objects[0], nil

}

// GetKubernetesWorkloadResourceDefinition gets a kubernetes workload resource
// definition by kind and name.
func GetKubernetesWorkloadResourceDefinition(workloadResourceDefinitions *[]v0.KubernetesWorkloadResourceDefinition, kind, name string) (*v0.KubernetesWorkloadResourceDefinition, error) {

	var objects []v0.KubernetesWorkloadResourceDefinition
	for _, wrd := range *workloadResourceDefinitions {

		mapDef, err := util.UnmarshalJSON(*wrd.JSONDefinition)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal json: %w", err)
		}

		if mapDef["kind"] == kind &&
			mapDef["metadata"].(map[string]interface{})["name"] == name {
			objects = append(objects, wrd)
		}
	}

	if len(objects) == 0 {
		return nil, fmt.Errorf("kubernetes workload resource definition not found")
	}
	if len(objects) > 1 {
		return nil, fmt.Errorf("multiple kubernetes workload resource definitions found")
	}

	return &objects[0], nil

}

// GetKubernetesWorkloadResourceInstance gets a kubernetes workload resource
// instance by kind and name.
func GetKubernetesWorkloadResourceInstance(workloadResourceInstances *[]v0.KubernetesWorkloadResourceInstance, kind, name string) (*v0.KubernetesWorkloadResourceInstance, error) {

	var objects []v0.KubernetesWorkloadResourceInstance
	for _, wri := range *workloadResourceInstances {

		mapDef, err := util.UnmarshalJSON(*wri.JSONDefinition)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal json: %w", err)
		}

		if mapDef["kind"] == kind &&
			mapDef["metadata"].(map[string]interface{})["name"] == name {
			objects = append(objects, wri)
		}
	}

	if len(objects) == 0 {
		return nil, fmt.Errorf("kubernetes workload resource instance not found")
	}
	if len(objects) > 1 {
		return nil, fmt.Errorf("multiple kubernetes workload resource instances found")
	}

	return &objects[0], nil

}
