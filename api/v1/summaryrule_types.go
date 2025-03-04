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

// SummaryRuleSpec defines the desired state of SummaryRule
type SummaryRuleSpec struct {
	// Database is the name of the database in which the function will be created
	Database string `json:"database"`
	// Table is rule output destination
	Table string `json:"table"`
	// Name is the name of the rule
	Name string `json:"name"`
	// Body is the KQL body of the function
	Body string `json:"body"`
	// Interval is the cadence at which the rule will be executed
	Interval metav1.Duration `json:"interval"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// SummaryRule is the Schema for the summaryrules API
type SummaryRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SummaryRuleSpec   `json:"spec,omitempty"`
	Status SummaryRuleStatus `json:"status,omitempty"`
}

// SummaryRuleStatus defines the observed state of Function
type SummaryRuleStatus struct {
	// Conditions is an array of current observed SummaryRule conditions
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true

// SummaryRuleList contains a list of SummaryRule
type SummaryRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SummaryRule `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SummaryRule{}, &SummaryRuleList{})
}
