package observability

import (
	"strings"
	"testing"

	v0 "github.com/threeport/threeport/pkg/api/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestGetObservabilityStackInstanceOperationsAllEnabled covers that the
// operations builder appends the observability dashboard, logging, and
// metrics operations in order when both feature flags are enabled.
func TestGetObservabilityStackInstanceOperationsAllEnabled(t *testing.T) {
	// build a config with logging and metrics enabled
	cfg := &ObservabilityStackInstanceConfig{
		observabilityStackInstance: &v0.ObservabilityStackInstance{
			Instance: v0.Instance{
				Name: util.Ptr("stack"),
			},
			LoggingEnabled: util.Ptr(true),
			MetricsEnabled: util.Ptr(true),
		},
	}

	// exercise the operations builder
	ops := cfg.getObservabilityStackInstanceOperations()

	// assert all three operations are appended in the expected order
	wantNames := []string{"observability dashboard", "logging", "metrics"}
	if got, want := len(ops.Operations), len(wantNames); got != want {
		t.Fatalf("operations length = %d, want %d", got, want)
	}
	for i, want := range wantNames {
		if got := ops.Operations[i].Name; got != want {
			t.Errorf("Operations[%d].Name = %q, want %q", i, got, want)
		}
		// assert Create and Delete handlers are wired
		if ops.Operations[i].Create == nil {
			t.Errorf("Operations[%d].Create is nil", i)
		}
		if ops.Operations[i].Delete == nil {
			t.Errorf("Operations[%d].Delete is nil", i)
		}
	}
}

// TestGetObservabilityStackInstanceOperationsMinimal covers that the operations
// builder omits the logging and metrics operations when the feature flags are
// false; only the observability dashboard operation remains.
func TestGetObservabilityStackInstanceOperationsMinimal(t *testing.T) {
	// build a config with both feature flags disabled
	cfg := &ObservabilityStackInstanceConfig{
		observabilityStackInstance: &v0.ObservabilityStackInstance{
			Instance: v0.Instance{
				Name: util.Ptr("stack"),
			},
			LoggingEnabled: util.Ptr(false),
			MetricsEnabled: util.Ptr(false),
		},
	}

	// exercise the operations builder
	ops := cfg.getObservabilityStackInstanceOperations()

	// assert only the dashboard operation is appended
	if got, want := len(ops.Operations), 1; got != want {
		t.Fatalf("operations length = %d, want %d", got, want)
	}
	if got, want := ops.Operations[0].Name, "observability dashboard"; got != want {
		t.Errorf("Operations[0].Name = %q, want %q", got, want)
	}
}

// TestGetObservabilityStackInstanceOperationsLoggingOnly covers that only the
// dashboard and logging operations appear when metrics are disabled but
// logging is enabled.
func TestGetObservabilityStackInstanceOperationsLoggingOnly(t *testing.T) {
	// build a config with logging enabled and metrics disabled
	cfg := &ObservabilityStackInstanceConfig{
		observabilityStackInstance: &v0.ObservabilityStackInstance{
			Instance: v0.Instance{
				Name: util.Ptr("stack"),
			},
			LoggingEnabled: util.Ptr(true),
			MetricsEnabled: util.Ptr(false),
		},
	}

	// exercise the operations builder
	ops := cfg.getObservabilityStackInstanceOperations()

	// assert dashboard and logging are appended, metrics is absent
	wantNames := []string{"observability dashboard", "logging"}
	if got, want := len(ops.Operations), len(wantNames); got != want {
		t.Fatalf("operations length = %d, want %d", got, want)
	}
	for i, want := range wantNames {
		if got := ops.Operations[i].Name; got != want {
			t.Errorf("Operations[%d].Name = %q, want %q", i, got, want)
		}
	}
}

// TestGetObservabilityStackInstanceOperationsMetricsOnly covers that only the
// dashboard and metrics operations appear when logging is disabled but
// metrics is enabled.
func TestGetObservabilityStackInstanceOperationsMetricsOnly(t *testing.T) {
	// build a config with metrics enabled and logging disabled
	cfg := &ObservabilityStackInstanceConfig{
		observabilityStackInstance: &v0.ObservabilityStackInstance{
			Instance: v0.Instance{
				Name: util.Ptr("stack"),
			},
			LoggingEnabled: util.Ptr(false),
			MetricsEnabled: util.Ptr(true),
		},
	}

	// exercise the operations builder
	ops := cfg.getObservabilityStackInstanceOperations()

	// assert dashboard and metrics are appended, logging is absent
	wantNames := []string{"observability dashboard", "metrics"}
	if got, want := len(ops.Operations), len(wantNames); got != want {
		t.Fatalf("operations length = %d, want %d", got, want)
	}
	for i, want := range wantNames {
		if got := ops.Operations[i].Name; got != want {
			t.Errorf("Operations[%d].Name = %q, want %q", i, got, want)
		}
	}
}

// TestSetMergedObservabilityStackInstanceValuesMetricsDisabled covers that the
// merger populates the grafana, kube-prometheus-stack, loki, and promtail
// value documents from the instance and definition inputs when metrics are
// disabled; the grafana service monitor block is NOT appended.
func TestSetMergedObservabilityStackInstanceValuesMetricsDisabled(t *testing.T) {
	// build a config with representative helm values on both sides and
	// metrics disabled so the service monitor override does not merge in
	cfg := &ObservabilityStackInstanceConfig{
		observabilityStackInstance: &v0.ObservabilityStackInstance{
			MetricsEnabled:                        util.Ptr(false),
			GrafanaHelmValuesDocument:             util.Ptr("grafana: instance\n"),
			KubePrometheusStackHelmValuesDocument: util.Ptr("kps: instance\n"),
			LokiHelmValuesDocument:                util.Ptr("loki: instance\n"),
			PromtailHelmValuesDocument:            util.Ptr("promtail: instance\n"),
		},
		observabilityStackDefinition: &v0.ObservabilityStackDefinition{
			GrafanaHelmValuesDocument:             util.Ptr("grafana: definition\n"),
			KubePrometheusStackHelmValuesDocument: util.Ptr("kps: definition\n"),
			LokiHelmValuesDocument:                util.Ptr("loki: definition\n"),
			PromtailHelmValuesDocument:            util.Ptr("promtail: definition\n"),
		},
	}

	// exercise the merger
	if err := cfg.setMergedObservabilityStackInstanceValues(); err != nil {
		t.Fatalf("setMergedObservabilityStackInstanceValues returned error: %v", err)
	}

	// assert every merged field is populated from the inputs; override takes
	// precedence over base so the "definition" scalar wins for each key
	checks := []struct {
		field string
		got   string
		want  string
	}{
		{"grafanaHelmValuesDocument", cfg.grafanaHelmValuesDocument, "grafana: definition"},
		{"kubePrometheusStackHelmValuesDocument", cfg.kubePrometheusStackHelmValuesDocument, "kps: definition"},
		{"lokiHelmValuesDocument", cfg.lokiHelmValuesDocument, "loki: definition"},
		{"promtailHelmValuesDocument", cfg.promtailHelmValuesDocument, "promtail: definition"},
	}
	for _, c := range checks {
		if !strings.Contains(c.got, c.want) {
			t.Errorf("%s = %q, want to contain %q", c.field, c.got, c.want)
		}
	}

	// assert the grafana service monitor block was NOT merged in when metrics
	// are disabled; the serviceMonitor key must be absent from the grafana doc
	if strings.Contains(cfg.grafanaHelmValuesDocument, "serviceMonitor") {
		t.Errorf("grafanaHelmValuesDocument = %q, must not include serviceMonitor block when metrics disabled", cfg.grafanaHelmValuesDocument)
	}
}

// TestSetMergedObservabilityStackInstanceValuesMetricsEnabled covers that the
// merger folds the grafana service monitor block into the grafana values
// document when metrics are enabled.
func TestSetMergedObservabilityStackInstanceValuesMetricsEnabled(t *testing.T) {
	// build a config with metrics enabled and empty grafana values so the
	// service monitor block dominates the merged output
	cfg := &ObservabilityStackInstanceConfig{
		observabilityStackInstance: &v0.ObservabilityStackInstance{
			MetricsEnabled: util.Ptr(true),
		},
		observabilityStackDefinition: &v0.ObservabilityStackDefinition{},
	}

	// exercise the merger
	if err := cfg.setMergedObservabilityStackInstanceValues(); err != nil {
		t.Fatalf("setMergedObservabilityStackInstanceValues returned error: %v", err)
	}

	// assert the grafana service monitor key appears in the merged grafana doc
	if !strings.Contains(cfg.grafanaHelmValuesDocument, "serviceMonitor") {
		t.Errorf("grafanaHelmValuesDocument = %q, want it to include the serviceMonitor block when metrics enabled", cfg.grafanaHelmValuesDocument)
	}
}

// TestSetMergedObservabilityStackInstanceValuesEmptyInputs covers that the
// merger returns empty strings for every field when both the instance and
// definition provide nil helm values documents and metrics are disabled.
func TestSetMergedObservabilityStackInstanceValuesEmptyInputs(t *testing.T) {
	// build a config with nil values on both sides and metrics disabled
	cfg := &ObservabilityStackInstanceConfig{
		observabilityStackInstance: &v0.ObservabilityStackInstance{
			MetricsEnabled: util.Ptr(false),
		},
		observabilityStackDefinition: &v0.ObservabilityStackDefinition{},
	}

	// exercise the merger
	if err := cfg.setMergedObservabilityStackInstanceValues(); err != nil {
		t.Fatalf("setMergedObservabilityStackInstanceValues returned error: %v", err)
	}

	// assert every merged field is empty since both sides supplied nothing
	checks := map[string]string{
		"grafanaHelmValuesDocument":             cfg.grafanaHelmValuesDocument,
		"kubePrometheusStackHelmValuesDocument": cfg.kubePrometheusStackHelmValuesDocument,
		"lokiHelmValuesDocument":                cfg.lokiHelmValuesDocument,
		"promtailHelmValuesDocument":            cfg.promtailHelmValuesDocument,
	}
	for field, got := range checks {
		if got != "" {
			t.Errorf("%s = %q, want empty", field, got)
		}
	}
}
