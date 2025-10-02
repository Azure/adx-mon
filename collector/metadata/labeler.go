package metadata

import (
	"maps"
)

// DynamicLabeler is a labeler that adds dynamic labels from a collection of metadata sources.
type DynamicLabeler struct {
	// kubernetes node related metadata
	kubeNode *KubeNode
	// maps kubernetes node label keys to destination label key names
	kubeNodeLabels map[string]string
	// maps kubernetes node annotation keys to destination label key names
	kubeNodeAnnotations map[string]string
}

// DynamicLabelerConfig contains the label mapping configuration used to build a DynamicLabeler.
type DynamicLabelerConfig struct {
	KubernetesNodeLabels      map[string]string
	KubernetesNodeAnnotations map[string]string
}

// FromConfig creates a DynamicLabeler from the provided configuration and metadata sources.
// The provided configuration may be nil.
func FromConfig(kubeNode *KubeNode, cfg *DynamicLabelerConfig) *DynamicLabeler {
	var kubeNodeLabels map[string]string
	var kubeNodeAnnotations map[string]string

	if cfg != nil {
		if len(cfg.KubernetesNodeLabels) > 0 {
			kubeNodeLabels = maps.Clone(cfg.KubernetesNodeLabels)
		}
		if len(cfg.KubernetesNodeAnnotations) > 0 {
			kubeNodeAnnotations = maps.Clone(cfg.KubernetesNodeAnnotations)
		}
	}

	return &DynamicLabeler{
		kubeNode:            kubeNode,
		kubeNodeLabels:      kubeNodeLabels,
		kubeNodeAnnotations: kubeNodeAnnotations,
	}
}

// AppendLabelNamesBytes appends the dynamic label names this labeler sets to the provided slice of labels.
func (d *DynamicLabeler) AppendLabelNamesBytes(names [][]byte) [][]byte {
	if d.kubeNode != nil {
		for _, destKey := range d.kubeNodeLabels {
			names = append(names, []byte(destKey))
		}
		for _, destKey := range d.kubeNodeAnnotations {
			names = append(names, []byte(destKey))
		}
	}

	return names
}

// WalkLabels calls the provided function for each dynamic label this labeler would add.
// If the label source does not have a value for the label, the value will be an empty string.
func (d *DynamicLabeler) WalkLabels(callback func(key, value string)) {
	if d.kubeNode != nil {
		for srcKey, destKey := range d.kubeNodeLabels {
			val, _ := d.kubeNode.Label(srcKey)
			callback(destKey, val)
		}
		for srcKey, destKey := range d.kubeNodeAnnotations {
			val, _ := d.kubeNode.Annotation(srcKey)
			callback(destKey, val)
		}
	}
}
