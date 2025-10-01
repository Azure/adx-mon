package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAddMetadataLabelsValidate(t *testing.T) {
	newWatcherConfig := func() *Config {
		return &Config{
			MetadataWatch: &MetadataWatch{KubernetesNode: &MetadataWatchKubernetesNode{}},
		}
	}

	tests := []struct {
		name    string
		labels  *AddMetadataLabels
		cfg     *Config
		wantErr string
	}{
		{
			name:   "nil receiver",
			labels: nil,
			cfg:    &Config{},
		},
		{
			name:   "no kubernetes node configured",
			labels: &AddMetadataLabels{},
			cfg:    &Config{},
		},
		{
			name: "metadata watch required",
			labels: &AddMetadataLabels{
				KubernetesNode: &AddMetadataKubernetesNode{},
			},
			cfg:     &Config{},
			wantErr: "metadata-watch.kubernetes-node must be configured when add-metadata-labels.kubernetes-node is used",
		},
		{
			name: "invalid label key",
			labels: &AddMetadataLabels{
				KubernetesNode: &AddMetadataKubernetesNode{
					Labels: map[string]string{
						"": "dest",
					},
				},
			},
			cfg:     newWatcherConfig(),
			wantErr: "add-metadata-labels.kubernetes-node.labels key must be set",
		},
		{
			name: "invalid label value",
			labels: &AddMetadataLabels{
				KubernetesNode: &AddMetadataKubernetesNode{
					Labels: map[string]string{
						"source": "",
					},
				},
			},
			cfg:     newWatcherConfig(),
			wantErr: "add-metadata-labels.kubernetes-node.labels value must be set",
		},
		{
			name: "invalid annotation key",
			labels: &AddMetadataLabels{
				KubernetesNode: &AddMetadataKubernetesNode{
					Annotations: map[string]string{
						"": "dest",
					},
				},
			},
			cfg:     newWatcherConfig(),
			wantErr: "add-metadata-labels.kubernetes-node.annotations key must be set",
		},
		{
			name: "invalid annotation value",
			labels: &AddMetadataLabels{
				KubernetesNode: &AddMetadataKubernetesNode{
					Annotations: map[string]string{
						"source": "",
					},
				},
			},
			cfg:     newWatcherConfig(),
			wantErr: "add-metadata-labels.kubernetes-node.annotations value must be set",
		},
		{
			name: "success",
			labels: &AddMetadataLabels{
				KubernetesNode: &AddMetadataKubernetesNode{
					Labels: map[string]string{
						"kubernetes.io/role": "node_role",
					},
					Annotations: map[string]string{
						"cluster-autoscaler.kubernetes.io/safe-to-evict": "safe_to_evict",
					},
				},
			},
			cfg: newWatcherConfig(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.labels.Validate(tc.cfg)
			if tc.wantErr != "" {
				require.EqualError(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}
