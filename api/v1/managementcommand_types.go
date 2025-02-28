package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ManagementCommandSpec defines the desired state of ManagementCommand
type ManagementCommandSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Database is the target database for a management command. Not all management commands
	// are database specific.
	// +kubebuilder:validation:Optional
	Database string `json:"foo,omitempty"`

	// Body is the management command to execute
	Body string `json:"body"`
}

// ManagementCommandStatus defines the observed state of ManagementCommand
type ManagementCommandStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Conditions is a list of conditions that apply to the Function
	// +kubebuilder:validation:Optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
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
