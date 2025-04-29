package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AlerterSpec defines the desired state of Alerter
type AlerterSpec struct {
	// +kubebuilder:validation:Optional
	// Image is the image to use for the alerter
	Image string `json:"image,omitempty"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Format=uri
	NotificationEndpoint string `json:"notificationEndpoint"`

	// +kubebuilder:validation:Required
	// ADXClusterSelector is a label selector used to select the ADX clusters CRDs
	ADXClusterSelector *metav1.LabelSelector `json:"adxClusterSelector"`
}

// AlerterStatus defines the observed state of Alerter
type AlerterStatus struct {
	// +kubebuilder:validation:Optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Alerter is the Schema for the alerters API
type Alerter struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AlerterSpec   `json:"spec,omitempty"`
	Status AlerterStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// AlerterList contains a list of Alerter
type AlerterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Alerter `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Alerter{}, &AlerterList{})
}
