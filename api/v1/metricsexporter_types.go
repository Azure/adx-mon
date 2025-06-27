/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// MetricsExporterSpec defines the desired state of MetricsExporter
type MetricsExporterSpec struct {
	// Database is the name of the database to query
	Database string `json:"database"`

	// Body is the KQL query to execute
	Body string `json:"body"`

	// Interval defines how often to execute the query and refresh metrics
	Interval metav1.Duration `json:"interval"`

	// Transform defines how to convert query results to metrics
	Transform TransformConfig `json:"transform"`

	// Criteria for cluster-based execution selection (same pattern as SummaryRule)
	// Key/Value pairs used to determine when a metrics exporter can execute. If empty, always execute. Keys and values
	// are deployment specific and configured on adxexporter instances. For example, an adxexporter instance may be
	// started with `--cluster-labels=region=eastus,environment=production`. If a MetricsExporter has `criteria: {region: [eastus], environment: [production]}`, then the rule will only
	// execute on that adxexporter. Any key/values pairs must match (case-insensitive) for the rule to execute.
	Criteria map[string][]string `json:"criteria,omitempty"`
}

// TransformConfig defines how to convert query results to metrics
type TransformConfig struct {
	// MetricNameColumn specifies which column contains the metric name
	MetricNameColumn string `json:"metricNameColumn,omitempty"`

	// ValueColumn specifies which column contains the metric value
	ValueColumn string `json:"valueColumn"`

	// TimestampColumn specifies which column contains the timestamp
	TimestampColumn string `json:"timestampColumn"`

	// LabelColumns specifies columns to use as metric labels
	LabelColumns []string `json:"labelColumns,omitempty"`

	// DefaultMetricName provides a fallback if MetricNameColumn is not specified
	DefaultMetricName string `json:"defaultMetricName,omitempty"`
}

// MetricsExporterStatus defines the observed state of MetricsExporter
type MetricsExporterStatus struct {
	// Conditions is an array of current observed MetricsExporter conditions
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// MetricsExporter is the Schema for the metricsexporters API
type MetricsExporter struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MetricsExporterSpec   `json:"spec,omitempty"`
	Status MetricsExporterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// MetricsExporterList contains a list of MetricsExporter
type MetricsExporterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MetricsExporter `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MetricsExporter{}, &MetricsExporterList{})
}
