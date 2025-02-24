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

// FunctionSpec defines the desired state of Function
type FunctionSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// This flag tells the controller to suspend subsequent executions, it does
	// not apply to already started executions.  Defaults to false.
	// +optional
	Suspend *bool `json:"suspend,omitempty"`

	// Database is the name of the database in which the function will be created
	Database string `json:"database"`
	// Body is the KQL body of the function
	Body string `json:"body"`
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
	// Conditions is a list of conditions that apply to the Function
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
