package v1

import (
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ManagementCommandConditionOwner = "managementcommand.adx-mon.azure.com"
	ManagementCommandCriteriaMatch  = "managementcommand.adx-mon.azure.com/CriteriaMatch"
	ManagementCommandExecuted       = "managementcommand.adx-mon.azure.com/Executed"
)

const (
	ReasonManagementCommandExecutionSucceeded = "ExecutionSucceeded"
	ReasonManagementCommandExecutionFailed    = "ExecutionFailed"
	ReasonManagementCommandCriteriaError      = "CriteriaEvaluationFailed"
)

// ManagementCommandSpec defines the desired state of ManagementCommand
type ManagementCommandSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Database is the target database for a management command. Not all management commands
	// are database specific.
	// +kubebuilder:validation:Optional
	Database string `json:"database,omitempty"`

	// Body is the management command to execute
	Body string `json:"body"`

	// CriteriaExpression is an optional CEL (Common Expression Language) expression evaluated against
	// operator / ingestor cluster labels (region, environment, cloud, and any --cluster-labels key/value
	// pairs). All labels are exposed as string variables. Example:
	//
	//   criteriaExpression: "environment == 'prod' && region == 'eastus'"
	//
	// Semantics:
	//   * Empty / omitted expression => the ManagementCommand always executes when selected.
	//   * When specified, the expression must evaluate to true; false skips execution.
	//   * CEL parse, type-check, or evaluation errors surface via status and block execution until
	//     corrected.
	CriteriaExpression string `json:"criteriaExpression,omitempty"`
}

// ManagementCommandStatus defines the observed state of ManagementCommand
type ManagementCommandStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Conditions is a list of conditions that apply to the Function
	// +kubebuilder:validation:Optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

func (m *ManagementCommand) GetCondition() *metav1.Condition {
	return meta.FindStatusCondition(m.Status.Conditions, ManagementCommandConditionOwner)
}

func (m *ManagementCommand) SetCondition(c metav1.Condition) {
	if c.Type == "" {
		c.Type = ManagementCommandConditionOwner
	}

	existing := meta.FindStatusCondition(m.Status.Conditions, c.Type)
	if c.Reason == "" {
		if existing != nil && existing.Reason != "" {
			c.Reason = existing.Reason
		} else {
			c.Reason = defaultOwnerReason(c.Status)
		}
	}
	if c.Message == "" && existing != nil {
		c.Message = existing.Message
	}
	if c.ObservedGeneration == 0 {
		c.ObservedGeneration = m.GetGeneration()
	}
	if c.LastTransitionTime.IsZero() {
		c.LastTransitionTime = metav1.Now()
	}

	meta.SetStatusCondition(&m.Status.Conditions, c)
}

// SetCriteriaMatchCondition records the criteria evaluation state for the ManagementCommand.
func (m *ManagementCommand) SetCriteriaMatchCondition(matched bool, expression string, evaluationErr error, clusterLabels map[string]string) {
	condition := NewCriteriaCondition(ManagementCommandCriteriaMatch, m.GetGeneration(), matched, expression, evaluationErr, clusterLabels)
	m.SetCondition(condition)
}

// SetExecutionCondition updates both the ManagementCommandExecuted condition and the owner condition to reflect execution state.
func (m *ManagementCommand) SetExecutionCondition(status metav1.ConditionStatus, reason, message string) {
	execution := NewOwnerCondition(ManagementCommandExecuted, m.GetGeneration(), status, reason, message)
	m.SetCondition(execution)

	owner := execution
	owner.Type = ManagementCommandConditionOwner
	m.SetCondition(owner)
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ManagementCommand is the Schema for the managementcommands API
type ManagementCommand struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ManagementCommandSpec   `json:"spec,omitempty"`
	Status ManagementCommandStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ManagementCommandList contains a list of ManagementCommand
type ManagementCommandList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ManagementCommand `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ManagementCommand{}, &ManagementCommandList{})
}
