package observability

import "testing"

// TestObservabilityDashboardName covers that the helper composes the object
// name by appending the observability-dashboard suffix to the input.
func TestObservabilityDashboardName(t *testing.T) {
	// exercise the naming helper with a representative stack name
	got := ObservabilityDashboardName("mystack")
	// assert the suffix convention is preserved
	if want := "mystack-observability-dashboard"; got != want {
		t.Fatalf("ObservabilityDashboardName(%q) = %q, want %q", "mystack", got, want)
	}
	// assert that the helper still runs with an empty input
	if got := ObservabilityDashboardName(""); got != "-observability-dashboard" {
		t.Fatalf("ObservabilityDashboardName(\"\") = %q, want %q", got, "-observability-dashboard")
	}
}

// TestMetricsName covers that the helper appends the metrics suffix.
func TestMetricsName(t *testing.T) {
	// exercise with a representative name and assert the suffix
	if got, want := MetricsName("stack"), "stack-metrics"; got != want {
		t.Fatalf("MetricsName(%q) = %q, want %q", "stack", got, want)
	}
}

// TestLoggingName covers that the helper appends the logging suffix.
func TestLoggingName(t *testing.T) {
	// exercise with a representative name and assert the suffix
	if got, want := LoggingName("stack"), "stack-logging"; got != want {
		t.Fatalf("LoggingName(%q) = %q, want %q", "stack", got, want)
	}
}

// TestGrafanaChartName covers that the helper appends the grafana suffix.
func TestGrafanaChartName(t *testing.T) {
	// exercise with a representative name and assert the suffix
	if got, want := GrafanaChartName("obs"), "obs-grafana"; got != want {
		t.Fatalf("GrafanaChartName(%q) = %q, want %q", "obs", got, want)
	}
}

// TestKubePrometheusStackChartName covers that the helper appends the
// kube-prometheus-stack suffix.
func TestKubePrometheusStackChartName(t *testing.T) {
	// exercise with a representative name and assert the suffix
	if got, want := KubePrometheusStackChartName("obs"), "obs-kube-prometheus-stack"; got != want {
		t.Fatalf("KubePrometheusStackChartName(%q) = %q, want %q", "obs", got, want)
	}
}

// TestLokiHelmChartName covers that the helper appends the loki suffix.
func TestLokiHelmChartName(t *testing.T) {
	// exercise with a representative name and assert the suffix
	if got, want := LokiHelmChartName("logs"), "logs-loki"; got != want {
		t.Fatalf("LokiHelmChartName(%q) = %q, want %q", "logs", got, want)
	}
}

// TestPromtailHelmChartName covers that the helper appends the promtail suffix.
func TestPromtailHelmChartName(t *testing.T) {
	// exercise with a representative name and assert the suffix
	if got, want := PromtailHelmChartName("logs"), "logs-promtail"; got != want {
		t.Fatalf("PromtailHelmChartName(%q) = %q, want %q", "logs", got, want)
	}
}

// TestHelmRepoConstants covers that the exported helm repo URL constants
// point at the expected upstream repositories.
func TestHelmRepoConstants(t *testing.T) {
	// assert the grafana repo URL matches the published chart repo
	if want := "https://grafana.github.io/helm-charts"; GrafanaHelmRepo != want {
		t.Fatalf("GrafanaHelmRepo = %q, want %q", GrafanaHelmRepo, want)
	}
	// assert the prometheus community repo URL matches the published chart repo
	if want := "https://prometheus-community.github.io/helm-charts"; PrometheusCommunityHelmRepo != want {
		t.Fatalf("PrometheusCommunityHelmRepo = %q, want %q", PrometheusCommunityHelmRepo, want)
	}
}
