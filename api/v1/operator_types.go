package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// OperatorSpec defines the desired state of Operator
type OperatorSpec struct {
	// Azure authentication configuration (optional)
	// +optional
	AzureAuth *AzureAuthConfig `json:"azureAuth,omitempty"`

	// ADX configuration (optional for collector-only clusters)
	// +optional
	ADX *ADXConfig `json:"adx,omitempty"`

	// Ingestor configuration (optional for collector-only clusters)
	// +optional
	Ingestor *IngestorConfig `json:"ingestor,omitempty"`

	// Collector configuration
	// +optional
	Collector *CollectorConfig `json:"collector,omitempty"`

	// Alerter configuration (optional)
	// +optional
	Alerter *AlerterConfig `json:"alerter,omitempty"`
}

// OperatorStatus defines the observed state of Operator
type OperatorStatus struct {
	// Conditions is a list of conditions that apply to the Function
	// +kubebuilder:validation:Optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// AzureAuthType defines the authentication type to use
type AzureAuthType string

const (
	// AzureAuthTypeDefault uses DefaultAzureCredential which tries all auth methods
	AzureAuthTypeDefault AzureAuthType = "Default"
	// AzureAuthTypeManagedIdentity uses specific managed identity
	AzureAuthTypeManagedIdentity AzureAuthType = "ManagedIdentity"
	// AzureAuthTypeServicePrincipal uses service principal credentials
	AzureAuthTypeServicePrincipal AzureAuthType = "ServicePrincipal"
)

// AzureAuthConfig defines the Azure authentication configuration
type AzureAuthConfig struct {
	// Type of authentication to use
	// +kubebuilder:validation:Enum=Default;ManagedIdentity;ServicePrincipal
	// +kubebuilder:default=Default
	Type AzureAuthType `json:"type"`

	// ManagedIdentityClientID specifies which managed identity to use. Only used when Type is ManagedIdentity.
	// If not specified, the system-assigned managed identity is used.
	// +optional
	ManagedIdentityClientID string `json:"managedIdentityClientId,omitempty"`

	// ServicePrincipal holds service principal credentials. Only used when Type is ServicePrincipal.
	// +optional
	ServicePrincipal *AzureServicePrincipalAuth `json:"servicePrincipal,omitempty"`
}

// AzureServicePrincipalAuth holds service principal authentication details
type AzureServicePrincipalAuth struct {
	// TenantID is the Azure AD tenant ID
	TenantID string `json:"tenantId"`

	// ClientID references a secret containing the client ID
	ClientID SecretKeyRef `json:"clientId"`

	// ClientSecret references a secret containing the client secret
	ClientSecret SecretKeyRef `json:"clientSecret"`
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

	// Databases in this cluster.
	// +kubebuilder:validation:Required
	Databases []ADXDatabaseSpec `json:"databases"`

	// Provision contains configuration for provisioning the Kusto cluster (optional)
	// +kubebuilder:validation:Optional
	Provision *ADXClusterProvisionSpec `json:"provision,omitempty"`
}

// ADXClusterProvisionSpec describes how to provision a Kusto cluster.
type ADXClusterProvisionSpec struct {
	// Azure Subscription ID
	SubscriptionID string `json:"subscriptionId"`
	// Azure Resource Group
	ResourceGroup string `json:"resourceGroup"`
	// Azure Region
	Region string `json:"region"`
	// SKU name (e.g. Standard_L8as_v3)
	SKU string `json:"sku"`
	// Tier (e.g. Standard)
	Tier string `json:"tier"`
	// Managed Identity Client ID for admin assignment (optional)
	ManagedIdentityClientID string `json:"managedIdentityClientId,omitempty"`
	// Managed Identity Principal ID (object ID) for admin assignment (derived from client ID)
	ManagedIdentityPrincipalID string `json:"managedIdentityPrincipalId,omitempty"`
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
	InitConditionOwner             = "init.adx-mon.azure.com"
	ADXClusterConditionOwner       = "adxcluster.adx-mon.azure.com"
	IngestorClusterConditionOwner  = "ingestorcluster.adx-mon.azure.com"
	CollectorClusterConditionOwner = "collectorcluster.adx-mon.azure.com"
	AlerterClusterConditionOwner   = "alertercluster.adx-mon.azure.com"
)

// OperatorServiceReason is the reason for a service's current condition (shared by all managed services)
type OperatorServiceReason string

const (
	OperatorServiceReasonNotInstalled OperatorServiceReason = "NotInstalled"
	OperatorServiceReasonInstalling   OperatorServiceReason = "Installing"
	OperatorServiceReasonInstalled    OperatorServiceReason = "Installed"
	OperatorServiceReasonDrifted      OperatorServiceReason = "Drifted"
	OperatorServiceTerminalError      OperatorServiceReason = "TerminalError"
	OperatorServiceReasonUnknown      OperatorServiceReason = "Unknown"
	OperatorServiceReasonNotReady     OperatorServiceReason = "NotReady"
)

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
