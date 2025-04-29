package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CollectorSpec defines the desired state of Collector
type CollectorSpec struct {
	//+kubebuilder:validation:Optional
	// Image is the container image to use for the collector component. Optional; if omitted, a default image will be used.
	Image string `json:"image,omitempty"`

	//+kubebuilder:validation:Optional
	//+kubebuilder:validation:Format=uri
	// IngestorEndpoint is the URI endpoint for the ingestor service that this collector should send data to. Optional; if omitted, the operator will configure it automatically.
	IngestorEndpoint string `json:"ingestorEndpoint,omitempty"`
}

const CollectorConditionOwner = "collector.adx-mon.azure.com"

// CollectorStatus defines the observed state of Collector
type CollectorStatus struct {
	//+kubebuilder:validation:Optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Collector is the Schema for the collectors API
type Collector struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CollectorSpec   `json:"spec,omitempty"`
	Status CollectorStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// CollectorList contains a list of Collector
type CollectorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Collector `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Collector{}, &CollectorList{})
}
