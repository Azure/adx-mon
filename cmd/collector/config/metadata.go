package config

import (
	"errors"
	"maps"

	"github.com/Azure/adx-mon/collector/metadata"
)

// MetadataWatch defines a set of watchers for dynamic metadata to add to logs and metrics.
type MetadataWatch struct {
	KubernetesNode *MetadataWatchKubernetesNode `toml:"kubernetes-node,omitempty" comment:"Defines a watcher for Kubernetes node metadata (labels, annotations), consumed by add-metadata-labels"`
}

// MetadataWatchKubernetesNode defines a metadata watcher for Kubernetes nodes.
type MetadataWatchKubernetesNode struct {
}

func (m *MetadataWatch) Validate() error {
	if m == nil {
		return nil
	}

	// Ok to be empty for now. Nothing to validate.
	return nil
}

// AddMetadataLabels defines the set of metadata to add to metrics and logs.
type AddMetadataLabels struct {
	KubernetesNode *AddMetadataKubernetesNode `toml:"kubernetes-node,omitempty" comment:"Configures the node labels and annotations to add as labels"`
}

// AddMetadataKubernetesNode defines a set of Kubernetes node metadata to add to metrics and logs.
type AddMetadataKubernetesNode struct {
	Labels      map[string]string `toml:"labels,omitempty" comment:"Mapping of node label keys to destination label key names"`
	Annotations map[string]string `toml:"annotations,omitempty" comment:"Mapping of node annotation keys to destination label key names"`
}

func (a *AddMetadataLabels) Validate(config *Config) error {
	if a == nil {
		return nil
	}

	var useKubernetesNode bool
	node := a.KubernetesNode
	if node != nil {
		useKubernetesNode = true
	}

	if useKubernetesNode {
		if config == nil || config.MetadataWatch == nil || config.MetadataWatch.KubernetesNode == nil {
			return errors.New("metadata-watch.kubernetes-node must be configured when add-metadata-labels.kubernetes-node is used")
		}

		for key, value := range node.Labels {
			if key == "" {
				return errors.New("add-metadata-labels.kubernetes-node.labels key must be set")
			}
			if value == "" {
				return errors.New("add-metadata-labels.kubernetes-node.labels value must be set")
			}
		}

		for key, value := range node.Annotations {
			if key == "" {
				return errors.New("add-metadata-labels.kubernetes-node.annotations key must be set")
			}
			if value == "" {
				return errors.New("add-metadata-labels.kubernetes-node.annotations value must be set")
			}
		}
	}

	return nil
}

// MergeAddMetadataLabels collapses one or more AddMetadataLabels configs into a single DynamicLabelerConfig.
// Later configurations in the slice take precedence when keys overlap.
func MergeAddMetadataLabels(configs ...*AddMetadataLabels) *metadata.DynamicLabelerConfig {
	merged := &metadata.DynamicLabelerConfig{
		KubernetesNodeLabels:      make(map[string]string),
		KubernetesNodeAnnotations: make(map[string]string),
	}

	for _, cfg := range configs {
		if cfg == nil {
			continue
		}

		node := cfg.KubernetesNode
		if node == nil {
			continue
		}

		maps.Copy(merged.KubernetesNodeLabels, node.Labels)
		maps.Copy(merged.KubernetesNodeAnnotations, node.Annotations)
	}

	return merged
}
