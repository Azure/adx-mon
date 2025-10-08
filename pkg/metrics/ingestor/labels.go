package ingestor

import "k8s.io/apimachinery/pkg/labels"

const (
	// AppLabelKey is the standard Kubernetes label key applied to ingestor pods.
	AppLabelKey = "app"
	// AppLabelValue is the default label value assigned to ingestor pods.
	AppLabelValue = "ingestor"
)

// DefaultLabelSelector returns a fresh selector matching the default ingestor pod label set.
func DefaultLabelSelector() labels.Selector {
	return labels.SelectorFromSet(labels.Set{AppLabelKey: AppLabelValue})
}
