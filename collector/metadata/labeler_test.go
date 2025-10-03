package metadata

import (
	"testing"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/stretchr/testify/require"
)

func TestFromConfigClonesMaps(t *testing.T) {
	labels := map[string]string{
		"src": "dest",
	}
	annotations := map[string]string{
		"ann": "dest_ann",
	}

	labeler := FromConfig(&KubeNode{}, &DynamicLabelerConfig{
		KubernetesNodeLabels:      labels,
		KubernetesNodeAnnotations: annotations,
	})

	require.NotNil(t, labeler)
	require.Equal(t, "dest", labeler.kubeNodeLabels["src"])
	require.Equal(t, "dest_ann", labeler.kubeNodeAnnotations["ann"])

	labels["src"] = "mutated"
	annotations["ann"] = "mutated"

	require.Equal(t, "dest", labeler.kubeNodeLabels["src"])
	require.Equal(t, "dest_ann", labeler.kubeNodeAnnotations["ann"])
}

func TestDynamicLabelerWalkAndAppend(t *testing.T) {
	kubeNode := &KubeNode{
		labels: map[string]string{
			"src1": "value1",
		},
		annotations: map[string]string{
			"ann1": "value2",
		},
	}

	cfg := &DynamicLabelerConfig{
		KubernetesNodeLabels: map[string]string{
			"src1":    "dest1",
			"missing": "dest_missing",
		},
		KubernetesNodeAnnotations: map[string]string{
			"ann1":        "dest_ann1",
			"missing-ann": "dest_ann_missing",
		},
	}

	labeler := FromConfig(kubeNode, cfg)

	got := make(map[string]string)
	labeler.WalkLabels(func(key, value []byte) {
		got[string(key)] = string(value)
	})

	require.Equal(t, map[string]string{
		"dest1":            "value1",
		"dest_missing":     "",
		"dest_ann1":        "value2",
		"dest_ann_missing": "",
	}, got)

	var names []string
	for _, name := range labeler.AppendLabelNamesBytes(nil) {
		names = append(names, string(name))
	}

	require.ElementsMatch(t, []string{"dest1", "dest_missing", "dest_ann1", "dest_ann_missing"}, names)
}

func TestDynamicLabelerSetResourceValues(t *testing.T) {
	kubeNode := &KubeNode{
		labels: map[string]string{
			"src1": "value1",
		},
		annotations: map[string]string{
			"ann1": "value2",
		},
	}

	cfg := &DynamicLabelerConfig{
		KubernetesNodeLabels: map[string]string{
			"src1":    "dest1",
			"missing": "dest_missing",
		},
		KubernetesNodeAnnotations: map[string]string{
			"ann1":        "dest_ann1",
			"missing-ann": "dest_ann_missing",
		},
	}

	labeler := FromConfig(kubeNode, cfg)

	log := types.NewLog()
	labeler.SetResourceValues(log)

	require.Equal(t, 4, log.ResourceLen())

	val, ok := log.GetResourceValue("dest1")
	require.True(t, ok)
	require.Equal(t, "value1", val)

	val, ok = log.GetResourceValue("dest_missing")
	require.True(t, ok)
	require.Equal(t, "", val)

	val, ok = log.GetResourceValue("dest_ann1")
	require.True(t, ok)
	require.Equal(t, "value2", val)

	val, ok = log.GetResourceValue("dest_ann_missing")
	require.True(t, ok)
	require.Equal(t, "", val)
}

func TestDynamicLabelerSetResourceValuesNoNode(t *testing.T) {
	labeler := FromConfig(nil, &DynamicLabelerConfig{
		KubernetesNodeLabels: map[string]string{
			"src": "dest",
		},
	})

	log := types.NewLog()
	labeler.SetResourceValues(log)

	require.Equal(t, 0, log.ResourceLen())
}
