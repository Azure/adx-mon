package v1

import (
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AlerterSpec defines the desired state of Alerter
type AlerterSpec struct {
	//+kubebuilder:validation:Optional
	// Image is the container image to use for the alerter component. Optional; if omitted, a default image will be used.
	Image string `json:"image,omitempty"`

	//+kubebuilder:validation:Required
	//+kubebuilder:validation:Format=uri
	// NotificationEndpoint is the URI where alert notifications will be sent. This field is required.
	NotificationEndpoint string `json:"notificationEndpoint"`

	//+kubebuilder:validation:Required
	// ADXClusterSelector is a label selector used to select the ADXCluster CRDs this alerter should target. This field is required.
	ADXClusterSelector *metav1.LabelSelector `json:"adxClusterSelector"`

	// AppliedProvisionState is a JSON-serialized snapshot of the CRD
	// as last applied by the operator. This is set by the operator and is read-only for users.
	AppliedProvisionState string `json:"appliedProvisionState,omitempty"`

	// CriteriaExpression is an optional CEL (Common Expression Language) expression evaluated against
	// operator cluster labels (region, environment, cloud, and any --cluster-labels key/value pairs).
	// All labels are exposed as string variables that can be referenced directly. Example:
	//
	//   criteriaExpression: "region == 'eastus' && environment == 'prod'"
	//
	// Semantics:
	//   * Empty / omitted expression => the Alerter always reconciles.
	//   * When specified, the expression must evaluate to true for reconciliation; false skips quietly.
	//   * CEL parse, type-check, or evaluation errors surface via status and block reconciliation until
	//     corrected.
	CriteriaExpression string `json:"criteriaExpression,omitempty"`
}

func (s *AlerterSpec) StoreAppliedProvisioningState() error {
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

func (s *AlerterSpec) LoadAppliedProvisioningState() (*AlerterSpec, error) {
	// Unmarshal the JSON string back into the struct
	if s.AppliedProvisionState == "" {
		return nil, nil
	}
	var stored AlerterSpec
	err := json.Unmarshal([]byte(s.AppliedProvisionState), &stored)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal provision state: %w", err)
	}
	return &stored, nil
}

const AlerterConditionOwner = "alerter.adx-mon.azure.com"

// AlerterStatus defines the observed state of Alerter
type AlerterStatus struct {
	//+kubebuilder:validation:Optional
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
