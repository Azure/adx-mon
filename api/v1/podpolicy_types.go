package v1

import (
	corev1 "k8s.io/api/core/v1"
)

// PodPolicy defines workload-level pod scheduling and image pull configuration.
type PodPolicy struct {
	// ImagePullSecrets is an optional list of references to secrets in the same
	// namespace to use for pulling images.
	//+kubebuilder:validation:Optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// NodeSelector is an optional map of node selector labels used for pod scheduling.
	//+kubebuilder:validation:Optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Tolerations is an optional list of tolerations used for pod scheduling.
	//+kubebuilder:validation:Optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
}
