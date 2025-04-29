package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ADXClusterSpec defines the desired state of ADXCluster
type ADXClusterSpec struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=^[a-zA-Z0-9-]+$
	// +kubebuilder:validation:MaxLength=100
	// ADX cluster valid name
	ClusterName string `json:"clusterName"`

	// +kubebuilder:validation:Format=uri
	// ADX url
	Endpoint string `json:"endpoint,omitempty"`

	// +kubebuilder:validation:Optional
	// Optional
	Provision *ADXClusterProvisionSpec `json:"provision,omitempty"`
}

type ADXClusterProvisionSpec struct {
	// +kubebuilder:validation:Optional
	// Azure sub-id, optional
	SubscriptionId string `json:"subscriptionId,omitempty"`
	// +kubebuilder:validation:Optional
	// Azure resource-group, optional
	ResourceGroup string `json:"resourceGroup,omitempty"`
	// +kubebuilder:validation:Optional
	// Azure location, optional
	Location string `json:"location,omitempty"`
	// +kubebuilder:validation:Optional
	// ADX sku name, optional
	SkuName string `json:"skuName,omitempty"`
	// +kubebuilder:validation:Optional
	// ADX tier, optional
	Tier string `json:"tier,omitempty"`
	// +kubebuilder:validation:Optional
	// Azure MSI client-id, optional
	ManagedIdentityClientId string `json:"managedIdentityClientId,omitempty"`
}

type ADXClusterDatabaseSpec struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=^[a-zA-Z0-9_]+$
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:MinLength=1
	// ADX valid database name, required
	DatabaseName string `json:"databaseName"`
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=30
	// default 30, optional
	RetentionInDays int `json:"retentionInDays,omitempty"`
	// +kubebuilder:validation:Optional
	// ADX retention policy, optional
	RetentionPolicy string `json:"retentionPolicy,omitempty"`
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=Metrics;Logs;Traces
	// TelemetryType: Required
	TelemtryType DatabaseTelemetryType `json:"telemetryType"`
}

type DatabaseTelemetryType string

const (
	DatabaseTelemetryMetrics DatabaseTelemetryType = "Metrics"
	DatabaseTelemetryLogs    DatabaseTelemetryType = "Logs"
	DatabaseTelemetryTraces  DatabaseTelemetryType = "Traces"

	ADXClusterConditionOwner = "adxcluster.adx-mon.azure.com"
)

// ADXClusterStatus defines the observed state of ADXCluster
type ADXClusterStatus struct {
	// +kubebuilder:validation:Optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ADXCluster is the Schema for the adxclusters API
type ADXCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ADXClusterSpec   `json:"spec,omitempty"`
	Status ADXClusterStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ADXClusterList contains a list of ADXCluster
type ADXClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ADXCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ADXCluster{}, &ADXClusterList{})
}
