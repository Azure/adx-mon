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
	"time"

	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/clock"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

const (
	// MetricsExporterOwner is the owner of the metrics exporter
	MetricsExporterOwner = "metricsexporter.adx-mon.azure.com"
	// MetricsExporterLastSuccessfulExecution tracks the end time of the last successful query execution
	MetricsExporterLastSuccessfulExecution = "metricsexporter.adx-mon.azure.com/LastSuccessfulExecution"
)

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

	// MetricNamePrefix provides optional team/project namespacing for all metrics
	MetricNamePrefix string `json:"metricNamePrefix,omitempty"`

	// ValueColumn specifies which column contains the metric value
	ValueColumn string `json:"valueColumn"`

	// ValueColumns specifies columns to use as metric values
	ValueColumns []string `json:"valueColumns"`

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

// GetCondition gets the main condition for the MetricsExporter
func (me *MetricsExporter) GetCondition() *metav1.Condition {
	return meta.FindStatusCondition(me.Status.Conditions, MetricsExporterOwner)
}

// SetCondition sets the main condition for the MetricsExporter
func (me *MetricsExporter) SetCondition(condition metav1.Condition) {
	condition.Type = MetricsExporterOwner
	condition.ObservedGeneration = me.GetGeneration()
	meta.SetStatusCondition(&me.Status.Conditions, condition)
}

// GetLastExecutionTime extracts the last execution time from MetricsExporter status
func (me *MetricsExporter) GetLastExecutionTime() *time.Time {
	condition := meta.FindStatusCondition(me.Status.Conditions, MetricsExporterLastSuccessfulExecution)
	if condition == nil {
		return nil
	}

	// Parse the time from the message field
	if condition.Message == "" {
		return nil
	}

	t, err := time.Parse(time.RFC3339Nano, condition.Message)
	if err != nil {
		return nil
	}

	return &t
}

// SetLastExecutionTime updates the last execution time in MetricsExporter status
func (me *MetricsExporter) SetLastExecutionTime(endTime time.Time) {
	condition := &metav1.Condition{
		Type:               MetricsExporterLastSuccessfulExecution,
		Status:             metav1.ConditionTrue,
		Reason:             "ExecutionCompleted",
		Message:            endTime.UTC().Format(time.RFC3339Nano),
		ObservedGeneration: me.GetGeneration(),
	}

	meta.SetStatusCondition(&me.Status.Conditions, *condition)
}

// ShouldExecuteQuery determines if a query should be executed based on timing and interval
// This follows the same pattern as SummaryRule.ShouldSubmitRule
func (me *MetricsExporter) ShouldExecuteQuery(clk clock.Clock) bool {
	if clk == nil {
		clk = clock.RealClock{}
	}

	lastSuccessfulEndTime := me.GetLastExecutionTime()
	cnd := me.GetCondition()
	if cnd == nil {
		// For first-time execution, initialize the condition with a timestamp
		// that's one interval back from current time
		cnd = &metav1.Condition{
			LastTransitionTime: metav1.Time{Time: clk.Now().Add(-me.Spec.Interval.Duration)},
		}
	}

	// Don't execute queries if the MetricsExporter is being deleted
	if me.DeletionTimestamp != nil {
		return false
	}

	// Determine if the query should be executed based on several criteria:
	// 1. MetricsExporter has been updated (new generation)
	// 2. It's time for the next interval execution (based on actual time windows)
	return cnd.ObservedGeneration != me.GetGeneration() || // A new version of this CRD was created
		(lastSuccessfulEndTime != nil && clk.Since(*lastSuccessfulEndTime) >= me.Spec.Interval.Duration) || // Time for next interval
		(lastSuccessfulEndTime == nil && clk.Since(cnd.LastTransitionTime.Time) >= me.Spec.Interval.Duration) // First execution timing
}

// NextExecutionWindow calculates the next execution window for a MetricsExporter
// This follows the same pattern as SummaryRule.NextExecutionWindow
func (me *MetricsExporter) NextExecutionWindow(clk clock.Clock) (windowStartTime time.Time, windowEndTime time.Time) {
	if clk == nil {
		clk = clock.RealClock{}
	}

	// Calculate the next execution window based on the last successful execution
	lastSuccessfulEndTime := me.GetLastExecutionTime()
	if lastSuccessfulEndTime == nil {
		// First execution: start from current time aligned to interval boundary, going back one interval
		now := clk.Now().UTC()
		// Align to interval boundary for consistency
		alignedNow := now.Truncate(me.Spec.Interval.Duration)
		windowEndTime = alignedNow
		windowStartTime = windowEndTime.Add(-me.Spec.Interval.Duration)
	} else {
		// Subsequent executions: start from where the last successful execution ended
		windowStartTime = *lastSuccessfulEndTime
		windowStartTime = windowStartTime.Truncate(me.Spec.Interval.Duration)
		windowEndTime = windowStartTime.Add(me.Spec.Interval.Duration)

		// Ensure we don't execute future windows
		now := clk.Now().UTC().Truncate(time.Minute)
		if windowEndTime.After(now) {
			windowEndTime = now
		}
	}
	return
}
