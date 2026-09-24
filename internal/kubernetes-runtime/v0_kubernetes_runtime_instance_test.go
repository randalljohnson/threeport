package kubernetesruntime

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"

	v0 "github.com/threeport/threeport/pkg/api/v0"
	controller "github.com/threeport/threeport/pkg/controller/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestGetCloudProviderForInfraProvider covers the supported-provider mapping
// table and the unsupported-provider error path.
func TestGetCloudProviderForInfraProvider(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantOutput  string
		wantErr     bool
		wantErrText string
	}{
		{
			name:       "eks maps to aws",
			input:      v0.KubernetesRuntimeInfraProviderEKS,
			wantOutput: util.AwsProvider,
		},
		{
			name:       "gke maps to gcp",
			input:      v0.KubernetesRuntimeInfraProviderGKE,
			wantOutput: util.GcpProvider,
		},
		{
			name:       "kind maps to aws for testing defaults",
			input:      v0.KubernetesRuntimeInfraProviderKind,
			wantOutput: util.AwsProvider,
		},
		{
			name:        "unsupported infra provider returns error",
			input:       "unsupported-provider",
			wantErr:     true,
			wantErrText: "not supported",
		},
		{
			name:        "empty string returns unsupported error",
			input:       "",
			wantErr:     true,
			wantErrText: "not supported",
		},
		{
			name:        "oke not in switch statement returns error",
			input:       v0.KubernetesRuntimeInfraProviderOKE,
			wantErr:     true,
			wantErrText: "not supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// invoke the function under test
			got, err := GetCloudProviderForInfraProvider(tt.input)

			// verify error expectation
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantErrText) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErrText, err.Error())
				}
				// error path returns empty string per contract
				if got != "" {
					t.Errorf("expected empty output on error, got %q", got)
				}
				return
			}

			// verify happy-path return value
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.wantOutput {
				t.Errorf("expected %q, got %q", tt.wantOutput, got)
			}
		})
	}
}

// newTestLogger builds a no-op logger for reconcile-function tests.
func newTestLogger() *logr.Logger {
	l := logr.Discard()
	return &l
}

// boolPtr allocates a *bool from a literal.
func boolPtr(b bool) *bool { return &b }

// TestV0KubernetesRuntimeInstanceCreated_ReconciledShortCircuits asserts the
// created reconciler returns immediately when the instance is already
// marked Reconciled=true, without touching the API client.
func TestV0KubernetesRuntimeInstanceCreated_ReconciledShortCircuits(t *testing.T) {
	// build an instance already marked reconciled
	instance := &v0.KubernetesRuntimeInstance{
		Reconciliation: v0.Reconciliation{
			Reconciled: boolPtr(true),
		},
	}
	// pass a zero Reconciler; the short-circuit path must not access it
	r := &controller.Reconciler{}
	log := newTestLogger()

	// call the reconciler
	requeue, err := v0KubernetesRuntimeInstanceCreated(r, instance, log)

	// verify no error and no requeue delay on the short-circuit
	if err != nil {
		t.Fatalf("unexpected error on reconciled short-circuit: %v", err)
	}
	if requeue != 0 {
		t.Errorf("expected requeue 0, got %d", requeue)
	}
}

// TestV0KubernetesRuntimeInstanceUpdated_EarlyReturns covers the three
// short-circuit branches at the top of the updated reconciler: nil endpoint,
// unparsable endpoint, and a deletion already scheduled.
func TestV0KubernetesRuntimeInstanceUpdated_EarlyReturns(t *testing.T) {
	badEndpoint := "://not a url"
	emptyHost := "http://"
	goodEndpoint := "https://api.example.com"
	deleteTime := time.Now()

	tests := []struct {
		name     string
		instance *v0.KubernetesRuntimeInstance
	}{
		{
			name:     "nil api endpoint returns without work",
			instance: &v0.KubernetesRuntimeInstance{APIEndpoint: nil},
		},
		{
			name:     "unparsable api endpoint returns without work",
			instance: &v0.KubernetesRuntimeInstance{APIEndpoint: &badEndpoint},
		},
		{
			name:     "endpoint with empty host returns without work",
			instance: &v0.KubernetesRuntimeInstance{APIEndpoint: &emptyHost},
		},
		{
			name: "scheduled-for-deletion returns without work",
			instance: &v0.KubernetesRuntimeInstance{
				APIEndpoint: &goodEndpoint,
				Reconciliation: v0.Reconciliation{
					DeletionScheduled: &deleteTime,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// zero Reconciler; short-circuit paths must not touch APIClient
			r := &controller.Reconciler{}
			log := newTestLogger()

			// call the reconciler
			requeue, err := v0KubernetesRuntimeInstanceUpdated(r, tt.instance, log)

			// verify no error and no requeue on short-circuit
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if requeue != 0 {
				t.Errorf("expected requeue 0, got %d", requeue)
			}
		})
	}
}

// TestV0KubernetesRuntimeInstanceDeleted_UnscheduledIsError asserts the
// deleted reconciler surfaces an error when it fires before a deletion is
// scheduled, guarding against out-of-order notifications.
func TestV0KubernetesRuntimeInstanceDeleted_UnscheduledIsError(t *testing.T) {
	// deletion notification with no scheduled marker
	instance := &v0.KubernetesRuntimeInstance{
		Reconciliation: v0.Reconciliation{
			DeletionScheduled: nil,
		},
	}
	r := &controller.Reconciler{}
	log := newTestLogger()

	// call the reconciler
	requeue, err := v0KubernetesRuntimeInstanceDeleted(r, instance, log)

	// verify the guard fired: error returned, no requeue
	if err == nil {
		t.Fatalf("expected error when deletion not scheduled, got nil")
	}
	if !strings.Contains(err.Error(), "not scheduled") {
		t.Errorf("expected error mentioning 'not scheduled', got %q", err.Error())
	}
	if requeue != 0 {
		t.Errorf("expected requeue 0, got %d", requeue)
	}
	// static-message errors should not wrap another sentinel
	if errors.Unwrap(err) != nil {
		t.Errorf("expected no wrapped error under the static guard, got %v", errors.Unwrap(err))
	}
}

// TestV0KubernetesRuntimeInstanceDeleted_ConfirmedShortCircuits asserts the
// deleted reconciler returns without work once deletion is already confirmed.
func TestV0KubernetesRuntimeInstanceDeleted_ConfirmedShortCircuits(t *testing.T) {
	scheduled := time.Now()
	confirmed := scheduled.Add(time.Minute)
	// deletion both scheduled and confirmed; nothing more to do
	instance := &v0.KubernetesRuntimeInstance{
		Reconciliation: v0.Reconciliation{
			DeletionScheduled: &scheduled,
			DeletionConfirmed: &confirmed,
		},
	}
	r := &controller.Reconciler{}
	log := newTestLogger()

	// call the reconciler
	requeue, err := v0KubernetesRuntimeInstanceDeleted(r, instance, log)

	// verify no error and no requeue on confirmed short-circuit
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if requeue != 0 {
		t.Errorf("expected requeue 0, got %d", requeue)
	}
}
