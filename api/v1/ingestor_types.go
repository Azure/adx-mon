package v1

import (
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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

	//+kubebuilder:validation:Optional
	// Autoscaler configures the optional autoscaling control loop for the ingestor StatefulSet.
	Autoscaler *IngestorAutoscalerSpec `json:"autoscaler,omitempty"`
}

func (s *IngestorSpec) StoreAppliedProvisioningState() error {
	// Store the current provisioning state as a JSON string
	provisionState, err := json.Marshal(s)
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

	//+kubebuilder:validation:Optional
	Autoscaler *IngestorAutoscalerStatus `json:"autoscaler,omitempty"`
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

// +kubebuilder:validation:XValidation:rule="self.maxReplicas == 0 || self.maxReplicas >= self.minReplicas",message="maxReplicas must be greater than or equal to minReplicas"
// IngestorAutoscalerSpec captures configuration for the autoscaler control loop.
type IngestorAutoscalerSpec struct {
	//+kubebuilder:default=false
	Enabled bool `json:"enabled"`

	//+kubebuilder:validation:Minimum=0
	//+kubebuilder:default=1
	MinReplicas int32 `json:"minReplicas,omitempty"`

	//+kubebuilder:validation:Minimum=0
	//+kubebuilder:default=1
	MaxReplicas int32 `json:"maxReplicas,omitempty"`

	//+kubebuilder:validation:Minimum=0
	//+kubebuilder:validation:Maximum=100
	//+kubebuilder:default=70
	// ScaleUpCPUThreshold is the percentage (0-100) of average CPU utilization that triggers a scale-up evaluation.
	ScaleUpCPUThreshold int32 `json:"scaleUpCPUThreshold,omitempty"`

	//+kubebuilder:validation:Minimum=0
	//+kubebuilder:validation:Maximum=100
	//+kubebuilder:default=40
	// ScaleDownCPUThreshold is the percentage (0-100) of average CPU utilization at or below which scale-down is considered.
	ScaleDownCPUThreshold int32 `json:"scaleDownCPUThreshold,omitempty"`

	//+kubebuilder:default="5m"
	ScaleInterval metav1.Duration `json:"scaleInterval,omitempty"`

	//+kubebuilder:default="15m"
	CPUWindow metav1.Duration `json:"cpuWindow,omitempty"`

	//+kubebuilder:validation:Minimum=0
	//+kubebuilder:validation:Maximum=100
	//+kubebuilder:default=25
	// ScaleUpBasePercent is the baseline percentage (0-100) of the current replica count to add during a scale-up, prior to capping.
	ScaleUpBasePercent int32 `json:"scaleUpBasePercent,omitempty"`

	//+kubebuilder:validation:Minimum=0
	//+kubebuilder:default=5
	ScaleUpCapPerCycle int32 `json:"scaleUpCapPerCycle,omitempty"`

	//+kubebuilder:default=false
	DryRun bool `json:"dryRun,omitempty"`

	//+kubebuilder:default=true
	CollectMetrics bool `json:"collectMetrics,omitempty"`
}

// IngestorAutoscalerAction represents the last scaling decision performed.
type IngestorAutoscalerAction string

const (
	AutoscalerActionNone      IngestorAutoscalerAction = "None"
	AutoscalerActionScaleUp   IngestorAutoscalerAction = "ScaleUp"
	AutoscalerActionScaleDown IngestorAutoscalerAction = "ScaleDown"
	AutoscalerActionSkip      IngestorAutoscalerAction = "Skip"
)

// IngestorAutoscalerStatus exposes runtime data about the autoscaling loop.
type IngestorAutoscalerStatus struct {
	//+kubebuilder:validation:Optional
	LastAction IngestorAutoscalerAction `json:"lastAction,omitempty"`

	//+kubebuilder:validation:Optional
	LastActionTime *metav1.Time `json:"lastActionTime,omitempty"`

	//+kubebuilder:validation:Optional
	// LastObservedCPUPercentHundredths stores the last measured CPU utilization scaled by 100 (e.g. 7543 represents 75.43%).
	LastObservedCPUPercentHundredths int32 `json:"lastObservedCpuPercentHundredths,omitempty"`

	//+kubebuilder:validation:Optional
	Reason string `json:"reason,omitempty"`

	//+kubebuilder:validation:Optional
	LastScaleTime *metav1.Time `json:"lastScaleTime,omitempty"`
}
