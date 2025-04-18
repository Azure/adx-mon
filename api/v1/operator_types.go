package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// OperatorSpec defines the desired state of Operator
type OperatorSpec struct {
	// ADX configuration (optional for collector-only clusters)
	ADX *ADXConfig `json:"adx,omitempty"`

	// Ingestor configuration (optional for collector-only clusters)
	Ingestor *IngestorConfig `json:"ingestor,omitempty"`

	// Collector configuration
	Collector *CollectorConfig `json:"collector,omitempty"`

	// Alerter configuration (optional)
	Alerter *AlerterConfig `json:"alerter,omitempty"`
}

// ADXConfig holds configuration for one or more ADX clusters.
type ADXConfig struct {
	Clusters []ADXClusterSpec `json:"clusters"`
}

// ADXClusterSpec describes a single ADX cluster.
type ADXClusterSpec struct {
	// Name of the ADX cluster.
	// +kubebuilder:validation:Pattern=^[a-zA-Z0-9_-]+$
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Endpoint is the ADX cluster endpoint.
	// +kubebuilder:validation:Required
	Endpoint string `json:"endpoint"`

	// Connection information for the ADX cluster.
	// +kubebuilder:validation:Optional
	Connection *ADXConnectionSpec `json:"connection,omitempty"`

	// Databases in this cluster.
	// +kubebuilder:validation:Required
	Databases []ADXDatabaseSpec `json:"databases"`
}

// ADXConnectionSpec describes how to connect to an ADX cluster.
type ADXConnectionSpec struct {
	// Type of authentication. E.g., "MSI", "token", "key"
	// +kubebuilder:validation:Enum=MSI;token;key
	Type string `json:"type"`

	// ClientId for MSI authentication (optional)
	ClientId string `json:"clientId,omitempty"`

	// TokenSecretRef for token/key authentication (optional)
	TokenSecretRef *SecretKeyRef `json:"tokenSecretRef,omitempty"`
}

// SecretKeyRef references a key in a Kubernetes Secret.
type SecretKeyRef struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

type DatabaseTelemetryType string

const (
	DatabaseTelemetryMetrics DatabaseTelemetryType = "Metrics"
	DatabaseTelemetryLogs    DatabaseTelemetryType = "Logs"
	DatabaseTelemetryTraces  DatabaseTelemetryType = "Traces"
)

// ADXDatabaseSpec describes a database in an ADX cluster.
type ADXDatabaseSpec struct {
	// Name is the name of the ADX database.
	// +kubebuilder:validation:Pattern=^[a-zA-Z0-9_]+$
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// TelemetryType is the type of telemetry to collect.
	// +kubebuilder:validation:Enum=Metrics;Logs;Traces
	// +kubebuilder:validation:Required
	TelemetryType DatabaseTelemetryType `json:"telemetryType"`
}

// IngestorConfig configures the ingestor component.
type IngestorConfig struct {
	// Image for the ingestor container.
	Image string `json:"image,omitempty"`

	// Number of ingestor replicas.
	Replicas *int32 `json:"replicas,omitempty"`
}

// CollectorConfig configures the collector component.
type CollectorConfig struct {
	// Image for the collector container.
	Image string `json:"image,omitempty"`

	// If this is a collector-only cluster, specify ingestor connection:
	IngestorEndpoint string `json:"ingestorEndpoint,omitempty"`

	IngestorAuth *IngestorAuthSpec `json:"ingestorAuth,omitempty"`
}

// IngestorAuthSpec describes how to authenticate to the ingestor.
type IngestorAuthSpec struct {
	// Type of authentication. E.g., "token"
	Type string `json:"type"`

	// TokenSecretRef for token authentication.
	TokenSecretRef *SecretKeyRef `json:"tokenSecretRef,omitempty"`
}

// AlerterConfig configures the alerter component.
type AlerterConfig struct {
	// Image for the alerter container.
	Image string `json:"image,omitempty"`
}

const (
	OperatorCommandConditionOwner  = "operator.adx-mon.azure.com"
	ADXClusterConditionOwner       = "adxcluster.adx-mon.azure.com"
	IngestorClusterConditionOwner  = "ingestorcluster.adx-mon.azure.com"
	CollectorClusterConditionOwner = "collectorcluster.adx-mon.azure.com"
	AlerterClusterConditionOwner   = "alertercluster.adx-mon.azure.com"
)

// OperatorStatus defines the observed state of Operator
type OperatorStatus struct {
	// Conditions is a list of conditions that apply to the Function
	// +kubebuilder:validation:Optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Operator is the Schema for the operators API
type Operator struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OperatorSpec   `json:"spec,omitempty"`
	Status OperatorStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// OperatorList contains a list of Operator
type OperatorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Operator `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Operator{}, &OperatorList{})
}
