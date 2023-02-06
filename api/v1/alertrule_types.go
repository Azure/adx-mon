/*
Copyright 2023.

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

// AlertRuleSpec defines the desired state of AlertRule
type AlertRuleSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Database          string          `json:"database,omitempty"`
	Interval          metav1.Duration `json:"interval,omitempty"`
	Query             string          `json:"query,omitempty"`
	RoutingID         string          `json:"routingID,omitempty"`
	TSG               string          `json:"tsg,omitempty"`
	AutoMitigateAfter metav1.Duration `json:"autoMitigateAfter,omitempty"`
}

// AlertRuleStatus defines the observed state of AlertRule
type AlertRuleStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Status        string      `json:"status,omitempty"`
	Message       string      `json:"message,omitempty"`
	LastQueryTime metav1.Time `json:"lastQueryTime,omitempty"`
	LastAlertTime metav1.Time `json:"lastAlertTime,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// AlertRule is the Schema for the alertrules API
type AlertRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AlertRuleSpec   `json:"spec,omitempty"`
	Status AlertRuleStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// AlertRuleList contains a list of AlertRule
type AlertRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AlertRule `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AlertRule{}, &AlertRuleList{})
}
