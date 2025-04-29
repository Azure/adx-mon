package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// IngestorSpec defines the desired state of Ingestor
type IngestorSpec struct {
	// +kubebuilder:validation:Optional
	// Image is the image to use for the ingestor
	Image string `json:"image,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default=1
	// Replicas is the number of replicas to run
	Replicas int32 `json:"replicas,omitempty"`

	// +kubebuilder:validation:Optional
	// Endpoint is the endpoint to use for the ingestor. If running in a cluster, this should be the service name
	// otherwise the operator will generate an endpoint.
	Endpoint string `json:"endpoint,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default=false
	// ExposeExternally is a flag to indicate if the ingestor should be exposed externally as reflected in the Endpoint.
	ExposeExternally bool `json:"exposeExternally,omitempty"`

	// +kubebuilder:validation:Required
	// ADXClusterSelector is a label selector used to select the ADX clusters CRDs
	ADXClusterSelector *metav1.LabelSelector `json:"adxClusterSelector"`
}

const IngestorConditionOwner = "ingestor.adx-mon.azure.com"

// IngestorStatus defines the observed state of Ingestor
type IngestorStatus struct {
	// +kubebuilder:validation:Optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Ingestor is the Schema for the ingestors API
type Ingestor struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IngestorSpec   `json:"spec,omitempty"`
	Status IngestorStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// IngestorList contains a list of Ingestor
type IngestorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Ingestor `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Ingestor{}, &IngestorList{})
}
