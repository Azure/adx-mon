package v1

import (
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ADXClusterSpec defines the desired state of ADXCluster
type ADXClusterSpec struct {
	//+kubebuilder:validation:Required
	//+kubebuilder:validation:Pattern=^[a-zA-Z0-9-]+$
	//+kubebuilder:validation:MaxLength=100
	// ClusterName is the unique, valid name for the ADX cluster. Must match ^[a-zA-Z0-9-]+$ and be at most 100 characters. Used for resource identification and naming in Azure.
	ClusterName string `json:"clusterName"`

	//+kubebuilder:validation:Format=uri
	// Endpoint is the URI of an existing ADX cluster. If set, the operator will use this cluster instead of provisioning a new one. Example: "https://mycluster.kusto.windows.net"
	Endpoint string `json:"endpoint,omitempty"`

	//+kubebuilder:validation:Optional
	// Databases is an array of ADXClusterDatabaseSpec objects. Each object defines a database to be created in the ADX cluster. If not specified, no databases will be created.
	Databases []ADXClusterDatabaseSpec `json:"databases,omitempty"`

	//+kubebuilder:validation:Optional
	// Provision contains optional Azure provisioning details for the ADX cluster. If omitted, the operator will attempt zero-config provisioning using Azure IMDS.
	Provision *ADXClusterProvisionSpec `json:"provision,omitempty"`
}

type ADXClusterProvisionSpec struct {
	//+kubebuilder:validation:Optional
	// SubscriptionId is the Azure subscription ID to use for provisioning. Optional. If omitted, will be auto-detected in zero-config mode.
	SubscriptionId string `json:"subscriptionId,omitempty"`
	//+kubebuilder:validation:Optional
	// ResourceGroup is the Azure resource group for the ADX cluster. Optional. If omitted, will be auto-created or detected.
	ResourceGroup string `json:"resourceGroup,omitempty"`
	//+kubebuilder:validation:Optional
	// Location is the Azure region for the ADX cluster (e.g., "eastus2"). Optional. If omitted, will be auto-detected.
	Location string `json:"location,omitempty"`
	//+kubebuilder:validation:Optional
	// SkuName is the Azure SKU for the ADX cluster (e.g., "Standard_L8as_v3"). Optional. The operator will select a default if not specified.
	SkuName string `json:"skuName,omitempty"`
	//+kubebuilder:validation:Optional
	// Tier is the Azure ADX tier (e.g., "Standard"). Optional. Defaults to "Standard" if not specified.
	Tier string `json:"tier,omitempty"`
	//+kubebuilder:validation:Optional
	// UserAssignedIdentities is a list of MSIs that can be attached to the cluster. They should be resource-ids of the form
	// /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ManagedIdentity/userAssignedIdentities/{identityName}
	UserAssignedIdentities []string `json:"userAssignedIdentities,omitempty"`
	//+kubebuilder:validation:Optional
	////+kubebuilder:default=false
	// AutoScale indicates whether to enable auto-scaling for the ADX cluster. Optional. Defaults to false if not specified.
	AutoScale bool `json:"autoScale,omitempty"`
	//+kubebuilder:validation:Optional
	//+kubebuilder:default=10
	// AutoScaleMax is the maximum number of nodes for auto-scaling. Optional. Defaults to 10 if not specified.
	AutoScaleMax int `json:"autoScaleMax,omitempty"`
	//+kubebuilder:validation:Optional
	//+kubebuilder:default=2
	// AutoScaleMin is the minimum number of nodes for auto-scaling. Optional. Defaults to 2 if not specified.
	AutoScaleMin int `json:"autoScaleMin,omitempty"`
	//+kubebuilder:validation:Optional
	// AppliedProvisionState is a JSON-serialized snapshot of the SkuName, Tier, and UserAssignedIdentities
	// as last applied by the operator. This is set by the operator and is read-only for users.
	AppliedProvisionState string `json:"appliedProvisionState,omitempty"`
}

type AppliedProvisionState struct {
	SkuName                string   `json:"skuName,omitempty"`
	Tier                   string   `json:"tier,omitempty"`
	UserAssignedIdentities []string `json:"userAssignedIdentities,omitempty"`
}

func (s *ADXClusterProvisionSpec) StoreAppliedProvisioningState() error {
	// Store the current provisioning state as a JSON string
	provisionState, err := json.Marshal(AppliedProvisionState{
		SkuName:                s.SkuName,
		Tier:                   s.Tier,
		UserAssignedIdentities: s.UserAssignedIdentities,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal provision state: %w", err)
	}
	s.AppliedProvisionState = string(provisionState)
	return nil
}

func (s *ADXClusterProvisionSpec) LoadAppliedProvisioningState() (*AppliedProvisionState, error) {
	// Unmarshal the JSON string back into the struct
	if s.AppliedProvisionState == "" {
		return nil, nil
	}
	var stored AppliedProvisionState
	err := json.Unmarshal([]byte(s.AppliedProvisionState), &stored)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal provision state: %w", err)
	}
	return &stored, nil
}

type ADXClusterDatabaseSpec struct {
	//+kubebuilder:validation:Required
	//+kubebuilder:validation:Pattern=^[a-zA-Z0-9_]+$
	//+kubebuilder:validation:MaxLength=64
	//+kubebuilder:validation:MinLength=1
	// ADX valid database name, required
	DatabaseName string `json:"databaseName"`
	//+kubebuilder:validation:Optional
	//+kubebuilder:default=30
	// default 30, optional
	RetentionInDays int `json:"retentionInDays,omitempty"`
	//+kubebuilder:validation:Optional
	// ADX retention policy, optional
	RetentionPolicy string `json:"retentionPolicy,omitempty"`
	//+kubebuilder:validation:Required
	//+kubebuilder:validation:Enum=Metrics;Logs;Traces
	// TelemetryType: Required
	TelemetryType DatabaseTelemetryType `json:"telemetryType"`
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
	//+kubebuilder:validation:Optional
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
