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
	"fmt"
	"sort"
	"strings"

	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// FunctionReconciled indicates whether the Function has been successfully processed by the ingestor.
	// True => the Function body has been executed in Kusto and is up to date; False => reconciliation failed or is pending retries.
	FunctionReconciled = "function.adx-mon.azure.com/Reconciled"
	// FunctionDatabaseMatch tracks whether the Function spec database matches the executing ingestor's database (case-insensitive).
	FunctionDatabaseMatch = "function.adx-mon.azure.com/DatabaseMatch"
	// FunctionCriteriaMatch signals the evaluation outcome of CriteriaExpression against the ingestor's cluster labels.
	FunctionCriteriaMatch = "function.adx-mon.azure.com/CriteriaMatch"
)

// AllDatabases is a special value that indicates all databases
// +k8s:deepcopy-gen=false
const AllDatabases = "*"

// FunctionSpec defines the desired state of Function
type FunctionSpec struct {
	//+kubebuilder:validation:Optional
	// This flag tells the controller to suspend subsequent executions, it does
	// not apply to already started executions.  Defaults to false.
	Suspend *bool `json:"suspend,omitempty"`

	//+kubebuilder:validation:Required
	// Database is the name of the database in which the function will be created
	Database string `json:"database"`

	//+kubebuilder:validation:Required
	// Body is the KQL body of the function
	Body string `json:"body"`

	//+kubebuilder:validation:Optional
	// AppliedEndpoint is a JSON-serialized of the endpoints that the function
	// is applied to. This is set by the operator and is read-only for users.
	AppliedEndpoint string `json:"appliedEndpoint,omitempty"`

	// CriteriaExpression is an optional CEL (Common Expression Language) expression evaluated against
	// operator cluster labels (region, environment, cloud, and any --cluster-labels key/value pairs).
	// Every label is exposed as a string variable. Example:
	//
	//   criteriaExpression: "environment == 'prod' && region in ['eastus','westus']"
	//
	// Semantics:
	//   * Empty / omitted expression => the Function always reconciles.
	//   * When specified, the expression must evaluate to true for reconciliation; false skips quietly.
	//   * CEL parse, type-check, or evaluation errors surface via status and block reconciliation until
	//     corrected.
	CriteriaExpression string `json:"criteriaExpression,omitempty"`
}

// FunctionStatusEnum defines the possible status values for FunctionStatus
type FunctionStatusEnum string

const (
	PermanentFailure FunctionStatusEnum = "PermanentFailure"
	Failed           FunctionStatusEnum = "Failed"
	Success          FunctionStatusEnum = "Success"
)

// FunctionStatus defines the observed state of Function
type FunctionStatus struct {
	// ObservedGeneration is the most recent generation observed for this Function
	ObservedGeneration int64 `json:"observedGeneration"`
	// LastTimeReconciled is the last time the Function was reconciled
	LastTimeReconciled metav1.Time `json:"lastTimeReconciled"`
	// Message is a human-readable message indicating details about the Function
	Message string `json:"message,omitempty"`
	// Reason is a string that communicates the reason for a transition
	Reason string `json:"reason,omitempty"`
	// Status is an enum that represents the status of the Function
	Status FunctionStatusEnum `json:"status"`
	// Error is a string that communicates any error message if one exists
	Error string `json:"error,omitempty"`
	// Conditions conveys detailed reconciliation state in a Kubernetes-native format.
	// Controllers set FunctionDatabaseMatch and FunctionCriteriaMatch to surface gating
	// decisions (skipped due to database mismatch or criteria mismatch) and
	// FunctionReconciled to report the outcome of the most recent reconciliation attempt.
	//
	// Example:
	//   status:
	//     conditions:
	//     - type: function.adx-mon.azure.com/DatabaseMatch
	//       status: "False"
	//       reason: DatabaseMismatch
	//       message: "Function database 'AKSProd' does not match available databases: aksprod, aksinfra"
	//     - type: function.adx-mon.azure.com/Reconciled
	//       status: "False"
	//       reason: DatabaseMismatchSkipped
	//       message: "Function skipped due to database mismatch"
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Function defines a KQL function to be maintained in the Kusto cluster
type Function struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FunctionSpec   `json:"spec,omitempty"`
	Status FunctionStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// FunctionList contains a list of Function
type FunctionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Function `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Function{}, &FunctionList{})
}

// GetCondition returns the FunctionReconciled condition, or nil if it has not been set.
func (f *Function) GetCondition() *metav1.Condition {
	return meta.FindStatusCondition(f.Status.Conditions, FunctionReconciled)
}

// SetCondition updates/creates a condition entry while ensuring mandatory fields are populated.
// This method satisfies the ConditionedObject interface and should generally be used via the
// higher level helpers (SetReconcileCondition, SetDatabaseMatchCondition, SetCriteriaMatchCondition)
// that provide semantic defaults.
func (f *Function) SetCondition(condition metav1.Condition) {
	if condition.Type == "" {
		condition.Type = FunctionReconciled
	}
	if condition.ObservedGeneration == 0 {
		condition.ObservedGeneration = f.GetGeneration()
	}
	if condition.LastTransitionTime.IsZero() {
		condition.LastTransitionTime = metav1.Now()
	}
	meta.SetStatusCondition(&f.Status.Conditions, condition)
}

// SetReconcileCondition updates the FunctionReconciled condition with status, reason, and message.
// ObservedGeneration is synchronised with the Function's current generation to signal which spec was processed.
func (f *Function) SetReconcileCondition(status metav1.ConditionStatus, reason, message string) {
	condition := metav1.Condition{
		Type:               FunctionReconciled,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: f.GetGeneration(),
		LastTransitionTime: metav1.Now(),
	}
	meta.SetStatusCondition(&f.Status.Conditions, condition)
	f.Status.ObservedGeneration = f.GetGeneration()
}

// SetDatabaseMatchCondition captures whether the Function's configured database matches the ingestor's database.
// availableDBs should list databases visible to the ingestor (case-insensitive display string).
func (f *Function) SetDatabaseMatchCondition(matched bool, configuredDB, availableDBs string) {
	reason := "DatabaseMatched"
	status := metav1.ConditionTrue
	message := fmt.Sprintf("Function database %q matches ingestor database %q", configuredDB, availableDBs)
	if configuredDB == AllDatabases {
		reason = "DatabaseWildcard"
		message = "Function configured for all databases"
	}
	if !matched {
		reason = "DatabaseMismatch"
		status = metav1.ConditionFalse
		if strings.TrimSpace(availableDBs) == "" {
			message = fmt.Sprintf("Function database %q does not match any configured ingestor endpoints", configuredDB)
		} else {
			message = fmt.Sprintf("Function database %q does not match available ingestor databases: %s", configuredDB, availableDBs)
		}
	}
	condition := metav1.Condition{
		Type:               FunctionDatabaseMatch,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: f.GetGeneration(),
		LastTransitionTime: metav1.Now(),
	}
	meta.SetStatusCondition(&f.Status.Conditions, condition)
}

// SetCriteriaMatchCondition records the outcome of CriteriaExpression evaluation. Optional clusterLabels may be provided
// as the final variadic parameter to enrich messages for debugging.
func (f *Function) SetCriteriaMatchCondition(matched bool, expression string, err error, clusterLabels ...map[string]string) {
	labels := map[string]string{}
	if len(clusterLabels) > 0 && clusterLabels[0] != nil {
		labels = clusterLabels[0]
	}
	labelSummary := formatClusterLabels(labels)
	reason := ReasonCriteriaMatched
	status := metav1.ConditionTrue
	message := fmt.Sprintf("Criteria expression %q matched cluster labels: %s", expression, labelSummary)
	if expression == "" {
		reason = ReasonCriteriaMatched
		message = "No criteria expression configured; defaulting to match"
	}
	if err != nil {
		reason = ReasonCriteriaExpressionError
		status = metav1.ConditionFalse
		message = fmt.Sprintf("Criteria expression %q failed: %v (cluster labels: %s)", expression, err, labelSummary)
	} else if !matched {
		reason = ReasonCriteriaNotMatched
		status = metav1.ConditionFalse
		message = fmt.Sprintf("Criteria expression %q evaluated to false for cluster labels: %s", expression, labelSummary)
	}
	condition := metav1.Condition{
		Type:               FunctionCriteriaMatch,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: f.GetGeneration(),
		LastTransitionTime: metav1.Now(),
	}
	meta.SetStatusCondition(&f.Status.Conditions, condition)
}

func formatClusterLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return "(none)"
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, labels[k]))
	}
	return strings.Join(parts, ", ")
}
