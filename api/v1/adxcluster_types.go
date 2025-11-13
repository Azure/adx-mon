package v1

import (
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
	// Endpoint is the URI of an existing ADX cluster. When set, the controller treats the cluster as bring-your-own (no Azure provisioning is attempted). Example: "https://mycluster.kusto.windows.net"
	Endpoint string `json:"endpoint,omitempty"`

	//+kubebuilder:validation:Optional
	// Databases is an array of ADXClusterDatabaseSpec objects. Each object defines a database to be created in the ADX cluster. If not specified, no databases will be created.
	Databases []ADXClusterDatabaseSpec `json:"databases,omitempty"`

	//+kubebuilder:validation:Optional
	// Provision contains Azure provisioning details for the ADX cluster. Reconciliation of Azure resources only occurs when this section is provided. If omitted, the operator skips provisioning and treats the cluster as bring-your-own.
	Provision *ADXClusterProvisionSpec `json:"provision,omitempty"`

	//+kubebuilder:validation:Optional
	//+kubebuilder:validation:Enum=Partition;Federated
	// Role specifies the cluster's role: "Partition" (default, for data-holding clusters) or "Federated" (for the central federating cluster).
	Role *ClusterRole `json:"role,omitempty"`

	//+kubebuilder:validation:Optional
	// Supports cluster partitioning. Only relevant if Role is set.
	Federation *ADXClusterFederationSpec `json:"federation,omitempty"`

	// CriteriaExpression is an optional CEL (Common Expression Language) expression evaluated against
	// operator cluster labels (region, environment, cloud, and any --cluster-labels key/value pairs).
	// Every label is exposed as a string variable that can be referenced directly, for example:
	//
	//   criteriaExpression: "region in ['eastus','westus'] && environment == 'prod'"
	//
	// Semantics:
	//   * Empty / omitted expression => the ADXCluster always reconciles.
	//   * When specified, the expression must evaluate to true for reconciliation; false skips quietly.
	//   * CEL parse, type-check, or evaluation errors are surfaced via status and block reconciliation
	//     until corrected.
	CriteriaExpression string `json:"criteriaExpression,omitempty"`
}

type ClusterRole string

const (
	ClusterRolePartition ClusterRole = "Partition"
	ClusterRoleFederated ClusterRole = "Federated"
)

type ADXClusterFederationSpec struct {
	// role: Partition

	//+kubebuilder:validation:Optional
	// If role is "Partition", specifies the Federated cluster(s) details for heartbeating.
	FederationTargets []ADXClusterFederatedClusterSpec `json:"federatedTargets,omitempty"`

	//+kubebuilder:validation:Optional
	// If role is "Partition", specifies the ADX cluster's partition details.  Open-ended map/object for partitioning metadata (geo, location, tenant, etc.).
	Partitioning *map[string]string `json:"partitioning,omitempty"`

	// role: Federated

	//+kubebuilder:validation:Optional
	// If role is "Federated", specifies the ADX cluster's heartbeat database.
	//+kubebuilder:validation:Pattern=^[a-zA-Z0-9_]+$
	HeartbeatDatabase *string `json:"heartbeatDatabase,omitempty"`

	//+kubebuilder:validation:Optional
	// If role is "Federated", specifies the ADX cluster's heartbeat table.
	//+kubebuilder:validation:Pattern=^[a-zA-Z0-9_]+$
	HeartbeatTable *string `json:"heartbeatTable,omitempty"`

	//+kubebuilder:validation:Optional
	//+kubebuilder:default="1h"
	//+kubebuilder:validation:Pattern=^(\d+h)?(\d+m)?(\d+s)?$
	// If role is "Federated", specifies the ADX cluster's heartbeat table TTL.
	HeartbeatTTL *string `json:"heartbeatTTL,omitempty"`
}

type ADXClusterFederatedClusterSpec struct {
	//+kubebuilder:validation:Required
	// Endpoint is the URI of the federated ADX cluster. Example: "https://mycluster.kusto.windows.net"
	Endpoint string `json:"endpoint"`

	//+kubebuilder:validation:Required
	//+kubebuilder:validation:Pattern=^[a-zA-Z0-9_]+$
	HeartbeatDatabase string `json:"heartbeatDatabase"`

	//+kubebuilder:validation:Required
	//+kubebuilder:validation:Pattern=^[a-zA-Z0-9_]+$
	HeartbeatTable string `json:"heartbeatTable"`

	//+kubebuilder:validation:Required
	// Used for writing logs to the federated cluster.
	ManagedIdentityClientId string `json:"managedIdentityClientId"`
}

type ADXClusterProvisionSpec struct {
	//+kubebuilder:validation:Optional
	// SubscriptionId is the Azure subscription ID to use for provisioning. When the operator manages provisioning this value must be supplied explicitly; it is no longer auto-detected or defaulted by the controller.
	SubscriptionId string `json:"subscriptionId,omitempty"`
	//+kubebuilder:validation:Optional
	// ResourceGroup is the Azure resource group for the ADX cluster. This must be provided when provisioning is enabled; the operator no longer mutates the spec to fill this value.
	ResourceGroup string `json:"resourceGroup,omitempty"`
	//+kubebuilder:validation:Optional
	// Location is the Azure region for the ADX cluster (e.g., "eastus2"). Required when the operator provisions the cluster and must be set explicitly by the user.
	Location string `json:"location,omitempty"`
	//+kubebuilder:validation:Optional
	// SkuName is the Azure SKU for the ADX cluster (e.g., "Standard_L8as_v3"). Required when provisioning is enabled; defaults are not applied automatically by the controller.
	SkuName string `json:"skuName,omitempty"`
	//+kubebuilder:validation:Optional
	// Tier is the Azure ADX tier (e.g., "Standard"). Required when provisioning is enabled; the operator no longer assigns a default.
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
}

type AppliedProvisionState struct {
	SkuName                string   `json:"skuName,omitempty"`
	Tier                   string   `json:"tier,omitempty"`
	UserAssignedIdentities []string `json:"userAssignedIdentities,omitempty"`
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
	//+kubebuilder:validation:Optional
	// Endpoint reflects the observed Kusto endpoint when the controller manages provisioning, or mirrors spec.endpoint when running in BYO mode.
	Endpoint string `json:"endpoint,omitempty"`
	//+kubebuilder:validation:Optional
	// AppliedProvisionState captures the last Azure provisioning state reconciled by the controller.
	AppliedProvisionState *AppliedProvisionState `json:"appliedProvisionState,omitempty"`
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
