package v0

import (
	"context"
	"fmt"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"

	v0 "github.com/threeport/threeport/pkg/api/v0"
)

// ParseApis splits a comma-separated --apis flag value into a clean
// slice, trimming whitespace from each entry and dropping empty
// fragments produced by leading or trailing commas.
func ParseApis(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// SelectControllersByGroup filters allControllers down to those whose
// component name corresponds to one of the requested sdk-config
// ApiObjectGroup names. Group names follow sdk-config.yaml exactly
// (e.g. "kubernetes_workload", "gateway"); the convention
// "<group>-controller" with underscores converted to dashes maps a
// group to its component name. An empty groupNames slice returns
// allControllers unchanged. Unknown group names produce an error
// listing the valid choices.
func SelectControllersByGroup(
	groupNames []string,
	allControllers []*v0.ControlPlaneComponent,
) ([]*v0.ControlPlaneComponent, error) {
	if len(groupNames) == 0 {
		return allControllers, nil
	}

	// build name lookup once so each requested group is resolved in
	// constant time.
	byName := make(map[string]*v0.ControlPlaneComponent, len(allControllers))
	for _, controller := range allControllers {
		byName[controller.Name] = controller
	}

	selected := make([]*v0.ControlPlaneComponent, 0, len(groupNames))
	for _, groupName := range groupNames {
		controllerName := controllerNameForGroup(groupName)
		controller, ok := byName[controllerName]
		if !ok {
			return nil, fmt.Errorf(
				"unknown api object group %q: valid choices are %s",
				groupName,
				strings.Join(ApiObjectGroupNames, ", "),
			)
		}
		selected = append(selected, controller)
	}

	return selected, nil
}

// DetectInstalledControllerNames returns the component names of the
// controller Deployments currently present in the control plane
// namespace. Uses the installer's managed-by label so it only counts
// installer-managed deployments. The rest-api and agent deployments
// are excluded because they aren't group-scoped controllers and are
// reapplied on every install path.
func DetectInstalledControllerNames(
	kubeClient dynamic.Interface,
	namespace string,
) ([]string, error) {
	selector := fmt.Sprintf("%s=%s", LabelManagedBy, LabelManagedByValue)

	list, err := kubeClient.Resource(deploymentGVR).Namespace(namespace).List(
		context.Background(),
		metav1.ListOptions{LabelSelector: selector},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to list installer-managed deployments in namespace %q: %w",
			namespace, err,
		)
	}

	names := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		deployName := item.GetName()
		if deployName == ThreeportAPIServiceResourceName || deployName == ThreeportAgentDeployName {
			continue
		}
		// installer deploys controllers as threeport-<name>; strip the
		// prefix to recover the component name in ControllerList.
		stripped := strings.TrimPrefix(deployName, "threeport-")
		names = append(names, stripped)
	}

	sort.Strings(names)
	return names, nil
}

// SelectControllersForReinstall picks the controller subset for a
// reinstall. When explicitGroups is non-empty, it defers to
// SelectControllersByGroup. Otherwise it auto-detects from the
// cluster's installer-managed deployments and filters allControllers
// to that set. The detected return value reports which path was
// taken so callers can log it.
func SelectControllersForReinstall(
	kubeClient dynamic.Interface,
	namespace string,
	explicitGroups []string,
	allControllers []*v0.ControlPlaneComponent,
) ([]*v0.ControlPlaneComponent, []string, bool, error) {
	// explicit groups path: --apis-supplied set, trust user selection
	if len(explicitGroups) > 0 {
		selected, err := SelectControllersByGroup(explicitGroups, allControllers)
		if err != nil {
			return nil, nil, false, err
		}
		names := make([]string, 0, len(selected))
		for _, controller := range selected {
			names = append(names, controller.Name)
		}
		return selected, names, false, nil
	}

	// auto-detect path: mirror what is currently installed in the cluster
	detectedNames, err := DetectInstalledControllerNames(kubeClient, namespace)
	if err != nil {
		return nil, nil, true, fmt.Errorf("failed to detect installed controllers: %w", err)
	}

	wanted := make(map[string]struct{}, len(detectedNames))
	for _, name := range detectedNames {
		wanted[name] = struct{}{}
	}

	// intersect with allControllers to preserve canonical install order
	selected := make([]*v0.ControlPlaneComponent, 0, len(detectedNames))
	selectedNames := make([]string, 0, len(detectedNames))
	for _, controller := range allControllers {
		if _, ok := wanted[controller.Name]; ok {
			selected = append(selected, controller)
			selectedNames = append(selectedNames, controller.Name)
		}
	}

	return selected, selectedNames, true, nil
}

// controllerNameForGroup maps an sdk-config ApiObjectGroup name to
// its controller component name, mirroring the convention in
// pkg/sdk/v0/gen/generator.go: append "-controller" and convert
// underscores to dashes.
func controllerNameForGroup(groupName string) string {
	return strings.ReplaceAll(fmt.Sprintf("%s-controller", groupName), "_", "-")
}

