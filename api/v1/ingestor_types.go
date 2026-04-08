package v1

import (
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TLSConfig defines TLS certificate configuration for the ingestor server.
// Only one of SecretRef or HostPath should be specified.
type TLSConfig struct {
	//+kubebuilder:validation:Optional
	// SecretRef references a Kubernetes Secret containing tls.crt and tls.key.
	// The secret must exist in the same namespace as the Ingestor.
	SecretRef *SecretReference `json:"secretRef,omitempty"`

	//+kubebuilder:validation:Optional
	// HostPath specifies a directory path on the host containing TLS certificates.
	// The directory should contain tls.crt and tls.key files.
	// This is useful for environments where certificates are pre-provisioned on nodes.
	HostPath string `json:"hostPath,omitempty"`
}

// SecretReference references a Kubernetes Secret by name.
type SecretReference struct {
	// Name is the name of the secret in the same namespace as the Ingestor.
	Name string `json:"name"`
}

// IngestorSpec defines the desired state of Ingestor
type IngestorSpec struct {
	//+kubebuilder:validation:Optional
	// Image is the container image to use for the ingestor component. Optional; if omitted, a default image will be used.
	Image string `json:"image,omitempty"`

	//+kubebuilder:validation:Optional
	//+kubebuilder:default=1
	// Replicas is the number of ingestor replicas to run. Optional; defaults to 1 if omitted.
	Replicas int32 `json:"replicas,omitempty"`

	//+kubebuilder:validation:Optional
	// Endpoint is the endpoint to use for the ingestor. If running in a cluster, this should be the service name; otherwise, the operator will generate an endpoint. Optional.
	Endpoint string `json:"endpoint,omitempty"`

	//+kubebuilder:validation:Optional
	//+kubebuilder:default=false
	// ExposeExternally indicates if the ingestor should be exposed externally as reflected in the Endpoint. Optional; defaults to false.
	ExposeExternally bool `json:"exposeExternally,omitempty"`

	//+kubebuilder:validation:Optional
	//+kubebuilder:default="Logs:Ingestor"
	// LogDestination configures the default log destination for ingestor pod logs in the form "Database:Table".
	// Optional; defaults to "Logs:Ingestor" if omitted.
	LogDestination string `json:"logDestination,omitempty"`

	//+kubebuilder:validation:Optional
	// TLS configures TLS certificates for the ingestor server.
	// If not specified, the ingestor will generate self-signed certificates.
	TLS *TLSConfig `json:"tls,omitempty"`

	//+kubebuilder:validation:Optional
	// PodPolicy configures pod scheduling and image pull behavior for the ingestor workload.
	PodPolicy *PodPolicy `json:"podPolicy,omitempty"`

	//+kubebuilder:validation:Required
	// ADXClusterSelector is a label selector used to select the ADXCluster CRDs this ingestor should target. This field is required.
	ADXClusterSelector *metav1.LabelSelector `json:"adxClusterSelector"`

	// AppliedProvisionState is a JSON-serialized snapshot of the CRD
	// as last applied by the operator. This is set by the operator and is read-only for users.
	AppliedProvisionState string `json:"appliedProvisionState,omitempty"`

	// CriteriaExpression is an optional CEL (Common Expression Language) expression evaluated against
	// operator cluster labels (region, environment, cloud, and any --cluster-labels key/value pairs).
	// All labels are exposed as string variables. Example:
	//
	//   criteriaExpression: "environment == 'prod' && region == 'eastus'"
	//
	// Semantics:
	//   * Empty / omitted expression => the Ingestor always reconciles.
	//   * When specified, the expression must evaluate to true for reconciliation; false skips quietly.
	//   * CEL parse, type-check, or evaluation errors surface via status and block reconciliation until
	//     corrected.
	CriteriaExpression string `json:"criteriaExpression,omitempty"`
}

func (s *IngestorSpec) StoreAppliedProvisioningState() error {
	// Store the current provisioning state as a JSON string.
	// Create a copy with AppliedProvisionState cleared to avoid recursive escaping -
	// otherwise each reconciliation would nest the previous JSON inside itself,
	// causing exponential growth in escape characters.
	toMarshal := *s
	toMarshal.AppliedProvisionState = ""
	provisionState, err := json.Marshal(toMarshal)
	if err != nil {
		return fmt.Errorf("failed to marshal provision state: %w", err)
	}
	s.AppliedProvisionState = string(provisionState)
	return nil
}

func (s *IngestorSpec) LoadAppliedProvisioningState() (*IngestorSpec, error) {
	// Unmarshal the JSON string back into the struct
	if s.AppliedProvisionState == "" {
		return nil, nil
	}
	var stored IngestorSpec
	err := json.Unmarshal([]byte(s.AppliedProvisionState), &stored)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal provision state: %w", err)
	}
	return &stored, nil
}

const IngestorConditionOwner = "ingestor.adx-mon.azure.com"

// IngestorStatus defines the observed state of Ingestor
type IngestorStatus struct {
	//+kubebuilder:validation:Optional
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
