package v0

// ObservabilityStackDefinition defines an observability stack.
type ObservabilityStackDefinition struct {
	Common         `swaggerignore:"true" mapstructure:",squash"`
	Definition     `mapstructure:",squash"`
	Reconciliation `mapstructure:",squash"`

	// Dashboard
	// The observability dashboard definition that belongs to this resource.
	ObservabilityDashboardDefinitionID *uint `query:"observabilitydashboarddefinitionid" validate:"optional" relationship:"owns"`

	// The version of the grafana helm chart to use from the helm repo, e.g. 1.2.3
	GrafanaHelmChartVersion *string `query:"grafanahelmchartversion" validate:"optional"`

	// Optional Helm workload definition values that can be provided to configure the
	// underlying grafana chart.
	GrafanaHelmValuesDocument *string `query:"grafanahelmvaluesdocument" validate:"optional"`

	// Metrics
	// The metrics definition that belongs to this resource.
	MetricsDefinitionID *uint `query:"metricsdefinitionid" validate:"optional" relationship:"owns"`

	// The version of the kube-prometheus-stack helm chart to use from the helm repo, e.g. 1.2.3
	KubePrometheusStackHelmChartVersion *string `query:"kubeprometheusstackhelmchartversion" validate:"optional"`

	// Optional Helm workload definition values that can be provided to configure the
	// underlying kube-prometheus-stack chart.
	KubePrometheusStackHelmValuesDocument *string `query:"kubeprometheusstackhelmvaluesdocument" validate:"optional"`

	// Logging
	// The logging definition that belongs to this resource.
	LoggingDefinitionID *uint `query:"loggingdefinitionid" validate:"optional" relationship:"owns"`

	// The version of the loki helm chart to use from the helm repo, e.g. 1.2.3
	LokiHelmChartVersion *string `query:"lokihelmchartversion" validate:"optional"`

	// Optional Helm workload definition values that can be provided to configure the
	// underlying loki chart.
	LokiHelmValuesDocument *string `query:"lokihelmvaluesdocument" validate:"optional"`

	// The version of the promtail helm chart to use from the helm repo, e.g. 1.2.3
	PromtailHelmChartVersion *string `query:"promtailhelmchartversion" validate:"optional"`

	// Optional Helm workload definition values that can be provided to configure the
	// underlying promtail chart.
	PromtailHelmValuesDocument *string `query:"promtailhelmvaluesdocument" validate:"optional"`

	// The associated observability stack instances that are deployed from this definition.
	ObservabilityStackInstances []*ObservabilityStackInstance `json:"ObservabilityStackInstances,omitempty" validate:"optional,association"`
}

// ObservabilityStackInstance is a deployed instance of an observability stack.
type ObservabilityStackInstance struct {
	Common         `swaggerignore:"true" mapstructure:",squash"`
	Instance       `mapstructure:",squash"`
	Reconciliation `mapstructure:",squash"`

	// The observability stack definition that belongs to this resource.
	ObservabilityStackDefinitionID *uint `query:"observabilitystackdefinitionid" gorm:"not null" validate:"required" relationship:"requires"`

	// The kubernetes runtime where the observability stack is installed.
	KubernetesRuntimeInstanceID *uint `query:"kubernetesruntimeinstanceid" gorm:"not null" validate:"required" relationship:"requires"`

	// If true, metrics will be enabled for the observability stack.
	MetricsEnabled *bool `query:"metricsenabled" gorm:"default:true" validate:"optional"`

	// If true, logging will be enabled for the observability stack.
	LoggingEnabled *bool `query:"loggingenabled" gorm:"default:true" validate:"optional"`

	// Dashboard
	// The observability dashboard instance that belongs to this resource.
	ObservabilityDashboardInstanceID *uint `query:"observabilitydashboardinstanceid" validate:"optional" relationship:"owns"`

	// Optional Helm workload instance values that can be provided to configure the
	// underlying grafana chart.
	GrafanaHelmValuesDocument *string `query:"grafanahelmvaluesdocument" validate:"optional"`

	// Metrics
	// The metrics instance that belongs to this resource.
	MetricsInstanceID *uint `query:"metricsinstanceid" validate:"optional" relationship:"owns"`

	// Optional Helm workload instance values that can be provided to configure the
	// underlying kube-prometheus-stack chart.
	KubePrometheusStackHelmValuesDocument *string `query:"kubeprometheusstackhelmvaluesdocument" validate:"optional"`

	// Logging
	// The logging instance that belongs to this resource.
	LoggingInstanceID *uint `query:"logginginstanceid" validate:"optional" relationship:"owns"`

	// Optional Helm workload instance values that can be provided to configure the
	// underlying loki chart.
	LokiHelmValuesDocument *string `query:"lokihelmvaluesdocument" validate:"optional"`

	// Optional Helm workload Instancehat can be provided to configure the
	// underlying promtail chart.
	PromtailHelmValuesDocument *string `query:"promtailhelmvaluesdocument" validate:"optional"`
}

// ObservabilityDashboardDefinition is the definition of an observability dashboard.
type ObservabilityDashboardDefinition struct {
	Common         `swaggerignore:"true" mapstructure:",squash"`
	Definition     `mapstructure:",squash"`
	Reconciliation `mapstructure:",squash"`

	// The Grafana Helm workload definition that belongs to this resource.
	GrafanaHelmWorkloadDefinitionID *uint `query:"grafanahelmworkloaddefinitionid" validate:"optional" relationship:"owns;type:HelmWorkloadDefinition"`

	// The version of the grafana helm chart to use from the helm repo, e.g. 1.2.3
	GrafanaHelmChartVersion *string `query:"grafanahelmchartversion" gorm:"default:'7.2.1'" validate:"optional"`

	// Optional Helm workload definition values that can be provided to configure the
	// underlying grafana chart.
	GrafanaHelmValuesDocument *string `query:"grafanahelmvaluesdocument" validate:"optional"`

	// The associated observability dashboard instances that are deployed from this definition.
	ObservabilityDashboardInstances []*ObservabilityDashboardInstance `json:"ObservabilityDashboardInstances,omitempty" validate:"optional,association"`
}

// ObservabilityDashboardInstances is a deployed instance of an observability dashboard.
type ObservabilityDashboardInstance struct {
	Common         `swaggerignore:"true" mapstructure:",squash"`
	Instance       `mapstructure:",squash"`
	Reconciliation `mapstructure:",squash"`

	// The observability dashboard definition that belongs to this resource.
	ObservabilityDashboardDefinitionID *uint `query:"observabilitydashboarddefinitionid" gorm:"not null" validate:"required" relationship:"requires"`

	// The kubernetes runtime where the observability dashboard is installed.
	KubernetesRuntimeInstanceID *uint `query:"kubernetesruntimeinstanceid" gorm:"not null" validate:"required" relationship:"requires"`

	// The Grafana Helm workload instance that belongs to this resource.
	GrafanaHelmWorkloadInstanceID *uint `query:"grafanahelmworkloadinstanceid" validate:"optional" relationship:"owns;type:HelmWorkloadInstance"`

	// Optional Helm workload definition values that can be provided to configure the
	// underlying grafana chart.
	GrafanaHelmValuesDocument *string `query:"grafanahelmvaluedsdocument" validate:"optional"`
}

// MetricsDefinition is the definition of a metrics aggregation layer for a workload.
type MetricsDefinition struct {
	Common         `swaggerignore:"true" mapstructure:",squash"`
	Definition     `mapstructure:",squash"`
	Reconciliation `mapstructure:",squash"`

	// The kube-prometheus-stack Helm workload definition that belongs to this resource.
	KubePrometheusStackHelmWorkloadDefinitionID *uint `query:"kubeprometheusstackhelmworkloaddefinitionid" validate:"optional" relationship:"owns;type:HelmWorkloadDefinition"`

	// The version of the kube-prometheus-stack helm chart to use from the helm repo, e.g. 1.2.3
	KubePrometheusStackHelmChartVersion *string `query:"kubeprometheusstackhelmchartversion" gorm:"default:'55.8.1'" validate:"optional"`

	// Optional Helm workload definition values that can be provided to configure the
	// underlying kube-prometheus-stack chart.
	KubePrometheusStackHelmValuesDocument *string `query:"kubeprometheusstackhelmvaluesdocument" validate:"optional"`

	// The associated metrics instances that are deployed from this definition.
	MetricsInstances []*MetricsInstance `json:"MetricsInstances,omitempty" validate:"optional,association"`
}

// MetricsInstances is a deployed instance of a metrics aggregation layer for a workload.
type MetricsInstance struct {
	Common         `swaggerignore:"true" mapstructure:",squash"`
	Instance       `mapstructure:",squash"`
	Reconciliation `mapstructure:",squash"`

	// The metrics definition that belongs to this resource.
	MetricsDefinitionID *uint `query:"metricsdefinitionid" gorm:"not null" validate:"required" relationship:"requires"`

	// The kubernetes runtime where the metrics is installed.
	KubernetesRuntimeInstanceID *uint `query:"kubernetesruntimeinstanceid" gorm:"not null" validate:"required" relationship:"requires"`

	// The kube-prometheus-stack helm workload instance that belongs to this resource.
	KubePrometheusStackHelmWorkloadInstanceID *uint `query:"kubeprometheusstackhelmworkloadinstanceid" validate:"optional" relationship:"owns;type:HelmWorkloadInstance"`

	// Optional Helm workload instance values that can be provided to configure the
	// underlying kube-prometheus-stack chart.
	KubePrometheusStackHelmValuesDocument *string `query:"kubeprometheusstackhelmvaluesdocument" validate:"optional"`
}

// LoggingDefinition is the definition of a logging implementation for a workload.
type LoggingDefinition struct {
	Common         `swaggerignore:"true" mapstructure:",squash"`
	Definition     `mapstructure:",squash"`
	Reconciliation `mapstructure:",squash"`

	// The loki Helm workload definition that belongs to this resource.
	LokiHelmWorkloadDefinitionID *uint `query:"lokihelmworkloaddefinitionid" validate:"optional" relationship:"owns;type:HelmWorkloadDefinition"`

	// The promtail Helm workload definition that belongs to this resource.
	PromtailHelmWorkloadDefinitionID *uint `query:"promtailhelmworkloaddefinitionid" validate:"optional" relationship:"owns;type:HelmWorkloadDefinition"`

	// The version of the loki helm chart to use from the helm repo, e.g. 1.2.3
	LokiHelmChartVersion *string `query:"lokihelmchartversion" gorm:"default:'5.41.6'" validate:"optional"`

	// The version of the promtail helm chart to use from the helm repo, e.g. 1.2.3
	PromtailHelmChartVersion *string `query:"promtailhelmchartversion" gorm:"default:'6.15.3'" validate:"optional"`

	// Optional Helm workload definition values that can be provided to configure the
	// underlying loki chart.
	LokiHelmValuesDocument *string `query:"lokihelmvaluesdocument" validate:"optional"`

	// Optional Helm workload definition values that can be provided to configure the
	// underlying promtail chart.
	PromtailHelmValuesDocument *string `query:"promtailhelmvaluesdocument" validate:"optional"`

	// The associated logging instances that are deployed from this definition.
	LoggingInstances []*LoggingInstance `json:"LoggingInstances,omitempty" validate:"optional,association"`
}

// LoggingInstances is a deployed instance of a logging implementation for a workload.
type LoggingInstance struct {
	Common         `swaggerignore:"true" mapstructure:",squash"`
	Instance       `mapstructure:",squash"`
	Reconciliation `mapstructure:",squash"`

	// The logging definition that belongs to this resource.
	LoggingDefinitionID *uint `query:"loggingdefinitionid" gorm:"not null" validate:"required" relationship:"requires"`

	// The kubernetes runtime where the logging is installed.
	KubernetesRuntimeInstanceID *uint `query:"kubernetesruntimeinstanceid" gorm:"not null" validate:"required" relationship:"requires"`

	// The loki helm workload instance that belongs to this resource.
	LokiHelmWorkloadInstanceID *uint `query:"lokihelmworkloadinstanceid" validate:"optional" relationship:"owns;type:HelmWorkloadInstance"`

	// The promtail helm workload instance that belongs to this resource.
	PromtailHelmWorkloadInstanceID *uint `query:"promtailhelmworkloadinstanceid" validate:"optional" relationship:"owns;type:HelmWorkloadInstance"`

	// Optional Helm workload instance values that can be provided to configure the
	// underlying loki chart.
	LokiHelmValuesDocument *string `query:"lokihelmvaluesdocument" validate:"optional"`

	// Optional Helm workload instance values that can be provided to configure the
	// underlying promtail chart.
	PromtailHelmValuesDocument *string `query:"promtailhelmvaluesdocument" validate:"optional"`
}
