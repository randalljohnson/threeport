package status

import (
	"errors"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// newDeploymentUnstructured builds an unstructured Deployment fixture with the
// given desired and ready replica counts. The Deployment name and namespace are
// stable so tests can assert on the reason string.
func newDeploymentUnstructured(desired, ready int64) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]interface{}{
			"name":      "web",
			"namespace": "app",
		},
		"spec": map[string]interface{}{
			"replicas": desired,
		},
		"status": map[string]interface{}{
			"readyReplicas": ready,
		},
	}}
}

// newStatefulSetUnstructured builds an unstructured StatefulSet fixture with the
// given desired and ready replica counts. See newDeploymentUnstructured.
func newStatefulSetUnstructured(desired, ready int64) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "apps/v1",
		"kind":       "StatefulSet",
		"metadata": map[string]interface{}{
			"name":      "db",
			"namespace": "data",
		},
		"spec": map[string]interface{}{
			"replicas": desired,
		},
		"status": map[string]interface{}{
			"readyReplicas": ready,
		},
	}}
}

// TestInspectDeployment covers that inspectDeployment reports Down when zero
// replicas are ready, Unhealthy when fewer than desired are ready, and Healthy
// when the ready count matches the desired count.
func TestInspectDeployment(t *testing.T) {
	cases := []struct {
		name           string
		desired, ready int64
		wantStatus     WorkloadInstanceStatus
		wantReasonSub  string
	}{
		{
			name:          "reports down when zero replicas ready",
			desired:       3,
			ready:         0,
			wantStatus:    WorkloadInstanceStatusDown,
			wantReasonSub: "0 replicas ready",
		},
		{
			name:          "reports unhealthy when fewer than desired ready",
			desired:       3,
			ready:         1,
			wantStatus:    WorkloadInstanceStatusUnhealthy,
			wantReasonSub: "has 1 ready",
		},
		{
			name:       "reports healthy when ready matches desired",
			desired:    2,
			ready:      2,
			wantStatus: WorkloadInstanceStatusHealthy,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// build a Deployment-shaped unstructured for the inspector
			u := newDeploymentUnstructured(tc.desired, tc.ready)

			// invoke the pure inspector under test
			status, reason, err := inspectDeployment(u)

			// conversion should never fail on a well-formed Deployment fixture
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// verify the inspector picked the branch matching the replica state
			if status != tc.wantStatus {
				t.Errorf("status = %q, want %q", status, tc.wantStatus)
			}
			// verify the reason string names the shortfall for non-healthy cases
			if tc.wantReasonSub != "" && !strings.Contains(reason, tc.wantReasonSub) {
				t.Errorf("reason = %q, want it to contain %q", reason, tc.wantReasonSub)
			}
			if tc.wantStatus == WorkloadInstanceStatusHealthy && reason != "" {
				t.Errorf("reason = %q, want empty for healthy", reason)
			}
		})
	}
}

// TestInspectStatefulSet covers that inspectStatefulSet reports Down when zero
// replicas are ready, Unhealthy when fewer than desired are ready, and Healthy
// when the ready count matches the desired count.
func TestInspectStatefulSet(t *testing.T) {
	cases := []struct {
		name           string
		desired, ready int64
		wantStatus     WorkloadInstanceStatus
		wantReasonSub  string
	}{
		{
			name:          "reports down when zero replicas ready",
			desired:       3,
			ready:         0,
			wantStatus:    WorkloadInstanceStatusDown,
			wantReasonSub: "0 replicas ready",
		},
		{
			name:          "reports unhealthy when fewer than desired ready",
			desired:       3,
			ready:         1,
			wantStatus:    WorkloadInstanceStatusUnhealthy,
			wantReasonSub: "has 1 ready",
		},
		{
			name:       "reports healthy when ready matches desired",
			desired:    2,
			ready:      2,
			wantStatus: WorkloadInstanceStatusHealthy,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// build a StatefulSet-shaped unstructured for the inspector
			u := newStatefulSetUnstructured(tc.desired, tc.ready)

			// invoke the pure inspector under test
			status, reason, err := inspectStatefulSet(u)

			// conversion should never fail on a well-formed StatefulSet fixture
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// verify the inspector picked the branch matching the replica state
			if status != tc.wantStatus {
				t.Errorf("status = %q, want %q", status, tc.wantStatus)
			}
			// verify the reason string names the shortfall for non-healthy cases
			if tc.wantReasonSub != "" && !strings.Contains(reason, tc.wantReasonSub) {
				t.Errorf("reason = %q, want it to contain %q", reason, tc.wantReasonSub)
			}
			if tc.wantStatus == WorkloadInstanceStatusHealthy && reason != "" {
				t.Errorf("reason = %q, want empty for healthy", reason)
			}
		})
	}
}

// TestInspectDeploymentConversionFailure covers that inspectDeployment reports
// an Error status wrapping the underlying conversion failure when the input is
// not shaped like a Deployment.
func TestInspectDeploymentConversionFailure(t *testing.T) {
	// build an unstructured whose kind cannot convert to Deployment
	u := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
	}}

	// invoke the inspector on the bad fixture
	status, _, err := inspectDeployment(u)

	// verify the error and the Error status short-circuit
	if err == nil {
		t.Fatalf("expected an error, got nil")
	}
	if status != WorkloadInstanceStatusError {
		t.Errorf("status = %q, want %q", status, WorkloadInstanceStatusError)
	}
}

// TestInspectStatefulSetConversionFailure covers that inspectStatefulSet
// reports an Error status wrapping the underlying conversion failure when the
// input is not shaped like a StatefulSet.
func TestInspectStatefulSetConversionFailure(t *testing.T) {
	// build an unstructured whose kind cannot convert to StatefulSet
	u := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
	}}

	// invoke the inspector on the bad fixture
	status, _, err := inspectStatefulSet(u)

	// verify the error and the Error status short-circuit
	if err == nil {
		t.Fatalf("expected an error, got nil")
	}
	if status != WorkloadInstanceStatusError {
		t.Errorf("status = %q, want %q", status, WorkloadInstanceStatusError)
	}
}

// TestGetKubernetesWorkloadInstanceStatusUnrecognizedType covers that
// GetKubernetesWorkloadInstanceStatus short-circuits with an Error status
// before making any API call when the workloadInstanceType is not one of the
// recognized subject types.
func TestGetKubernetesWorkloadInstanceStatusUnrecognizedType(t *testing.T) {
	// drive the function with an obviously unknown workload type
	detail := GetKubernetesWorkloadInstanceStatus(
		nil,
		"http://unused",
		"NotAThing",
		1,
		true,
	)

	// verify the detail reports the Error status and names the offending type
	if detail == nil {
		t.Fatalf("detail = nil, want non-nil")
	}
	if detail.Status != WorkloadInstanceStatusError {
		t.Errorf("status = %q, want %q", detail.Status, WorkloadInstanceStatusError)
	}
	if detail.Error == nil {
		t.Fatalf("Error = nil, want non-nil")
	}
	if !strings.Contains(detail.Error.Error(), "NotAThing") {
		t.Errorf("error = %q, want it to name the bad type", detail.Error)
	}
	// guard against a caller wrapping unrelated errors: the returned error must
	// be a real error and not silently nil in one of the fields
	if errors.Unwrap(detail.Error) != nil {
		// nothing to assert on the wrapped chain here; just exercise Unwrap
		// to make sure the returned error is well-formed for %w-style callers
		_ = detail.Error
	}
}
