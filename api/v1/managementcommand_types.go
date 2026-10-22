package v1

import (
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.
const ManagementCommandConditionOwner = "managementcommand.adx-mon.azure.com"

// ManagementCommandScope defines the scope at which a management command operates
type ManagementCommandScope string

const (
	// ScopeDatabase indicates the command targets a specific database (requires Database field)
	ScopeDatabase ManagementCommandScope = "Database"
	// ScopeAllDatabases indicates the command should be applied to all databases
	ScopeAllDatabases ManagementCommandScope = "AllDatabases"
	// ScopeCluster indicates the command is cluster-scoped
	ScopeCluster ManagementCommandScope = "Cluster"
)

// ManagementCommandSpec defines the desired state of ManagementCommand
type ManagementCommandSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Database is the target database for a management command. Not all management commands
	// are database specific. When non-empty, this field takes precedence over Scope.
	// Deprecated: For cluster-scoped commands, use Scope: Cluster instead of leaving empty.
	// +kubebuilder:validation:Optional
	Database string `json:"database,omitempty"`

	// Scope defines the execution scope of the management command.
	// - "Database": Command targets the database specified in the Database field (requires Database)
	// - "AllDatabases": Command is applied to all databases in the cluster
	// - "Cluster": Command is cluster-scoped (e.g., cluster policies)
	// When Database field is non-empty, it takes precedence over this field for backwards compatibility.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum=Database;AllDatabases;Cluster
	Scope ManagementCommandScope `json:"scope,omitempty"`

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
	if c.ObservedGeneration == 0 {
		c.Reason = "Created"
	} else {
		c.Reason = "Updated"
	}
	if c.Status == metav1.ConditionFalse {
		c.Reason = "Failed"
	}
	c.ObservedGeneration = m.GetGeneration()
	c.Type = ManagementCommandConditionOwner

	meta.SetStatusCondition(&m.Status.Conditions, c)
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
