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

func TestMergeAddMetadataLabels(t *testing.T) {
	t.Run("nil configs returns empty config", func(t *testing.T) {
		cfg := MergeAddMetadataLabels()
		require.NotNil(t, cfg)
		require.Empty(t, cfg.KubernetesNodeLabels)
		require.Empty(t, cfg.KubernetesNodeAnnotations)

		cfg = MergeAddMetadataLabels(nil, nil)
		require.NotNil(t, cfg)
		require.Empty(t, cfg.KubernetesNodeLabels)
		require.Empty(t, cfg.KubernetesNodeAnnotations)
	})

	t.Run("merges maps and preserves precedence", func(t *testing.T) {
		labelsFirst := map[string]string{
			"first":  "label_a",
			"shared": "label_b",
		}
		annotationsFirst := map[string]string{
			"first-ann": "ann_a",
		}
		labelsSecond := map[string]string{
			"shared": "label_c",
			"second": "label_d",
		}
		annotationsSecond := map[string]string{
			"second-ann": "ann_b",
		}

		cfg := MergeAddMetadataLabels(
			&AddMetadataLabels{
				KubernetesNode: &AddMetadataKubernetesNode{
					Labels:      labelsFirst,
					Annotations: annotationsFirst,
				},
			},
			&AddMetadataLabels{
				KubernetesNode: &AddMetadataKubernetesNode{
					Labels:      labelsSecond,
					Annotations: annotationsSecond,
				},
			},
		)

		require.NotNil(t, cfg)
		require.Equal(t, map[string]string{
			"first":  "label_a",
			"shared": "label_c",
			"second": "label_d",
		}, cfg.KubernetesNodeLabels)
		require.Equal(t, map[string]string{
			"first-ann":  "ann_a",
			"second-ann": "ann_b",
		}, cfg.KubernetesNodeAnnotations)

		// Mutating the original maps must not affect the merged output.
		labelsFirst["first"] = "mutated"
		annotationsSecond["second-ann"] = "mutated"
		require.Equal(t, "label_a", cfg.KubernetesNodeLabels["first"])
		require.Equal(t, "ann_b", cfg.KubernetesNodeAnnotations["second-ann"])
	})

	t.Run("skips nil kubernetes node configs", func(t *testing.T) {
		cfg := MergeAddMetadataLabels(&AddMetadataLabels{})
		require.NotNil(t, cfg)
		require.Empty(t, cfg.KubernetesNodeLabels)
		require.Empty(t, cfg.KubernetesNodeAnnotations)
	})
}
