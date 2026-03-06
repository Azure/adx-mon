package operator

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestLabels(t *testing.T) {
	l := resourceLabels("my-ingestor", "ingestor")

	require.Equal(t, "my-ingestor", l[LabelName])
	require.Equal(t, "ingestor", l[LabelComponent])
	require.Equal(t, ManagedByValue, l[LabelManagedBy])
	require.Len(t, l, 3)
}

func TestSelectorLabels(t *testing.T) {
	l := selectorLabels("my-ingestor", "ingestor")

	require.Equal(t, "my-ingestor", l[LabelName])
	require.Equal(t, "ingestor", l[LabelComponent])
	require.Len(t, l, 2)
	require.Empty(t, l[LabelManagedBy]) // Should not include managed-by
}

func TestMergeLabels(t *testing.T) {
	base := map[string]string{"a": "1", "b": "2"}
	override := map[string]string{"b": "3", "c": "4"}

	merged := mergeLabels(base, override)

	require.Equal(t, "1", merged["a"])
	require.Equal(t, "3", merged["b"]) // Override wins
	require.Equal(t, "4", merged["c"])
	require.Len(t, merged, 3)
}

func TestPtr(t *testing.T) {
	intVal := 42
	intPtr := ptr(intVal)
	require.NotNil(t, intPtr)
	require.Equal(t, intVal, *intPtr)

	strVal := "hello"
	strPtr := ptr(strVal)
	require.NotNil(t, strPtr)
	require.Equal(t, strVal, *strPtr)

	boolVal := true
	boolPtr := ptr(boolVal)
	require.NotNil(t, boolPtr)
	require.Equal(t, boolVal, *boolPtr)
}

func TestObjectMeta(t *testing.T) {
	meta := objectMeta("my-resource", "my-namespace", "collector")

	require.Equal(t, "my-resource", meta.Name)
	require.Equal(t, "my-namespace", meta.Namespace)
	require.Equal(t, "my-resource", meta.Labels[LabelName])
	require.Equal(t, "collector", meta.Labels[LabelComponent])
	require.Equal(t, ManagedByValue, meta.Labels[LabelManagedBy])
}

func TestClusterObjectMeta(t *testing.T) {
	meta := clusterObjectMeta("my-resource", "my-namespace", "ingestor")

	require.Equal(t, "my-namespace:my-resource", meta.Name)
	require.Empty(t, meta.Namespace) // Cluster-scoped, no namespace
	require.Equal(t, "my-resource", meta.Labels[LabelName])
	require.Equal(t, "ingestor", meta.Labels[LabelComponent])
}

func TestDefaultTolerations(t *testing.T) {
	tolerations := defaultTolerations()

	require.Len(t, tolerations, 3)

	// CriticalAddonsOnly toleration
	require.Equal(t, "CriticalAddonsOnly", tolerations[0].Key)
	require.Equal(t, corev1.TolerationOpExists, tolerations[0].Operator)

	// not-ready toleration
	require.Equal(t, "node.kubernetes.io/not-ready", tolerations[1].Key)
	require.Equal(t, corev1.TolerationOpExists, tolerations[1].Operator)
	require.Equal(t, corev1.TaintEffectNoExecute, tolerations[1].Effect)
	require.NotNil(t, tolerations[1].TolerationSeconds)
	require.Equal(t, int64(300), *tolerations[1].TolerationSeconds)

	// unreachable toleration
	require.Equal(t, "node.kubernetes.io/unreachable", tolerations[2].Key)
	require.Equal(t, corev1.TolerationOpExists, tolerations[2].Operator)
	require.Equal(t, corev1.TaintEffectNoExecute, tolerations[2].Effect)
	require.NotNil(t, tolerations[2].TolerationSeconds)
	require.Equal(t, int64(300), *tolerations[2].TolerationSeconds)
}

func TestMergeTolerations(t *testing.T) {
	defaults := defaultTolerations()

	extras := []corev1.Toleration{
		{
			Key:      "custom-taint",
			Operator: corev1.TolerationOpEqual,
			Value:    "true",
			Effect:   corev1.TaintEffectNoSchedule,
		},
		// This is a duplicate of a default toleration and should be skipped
		{
			Key:      "CriticalAddonsOnly",
			Operator: corev1.TolerationOpExists,
		},
	}

	merged := mergeTolerations(defaults, extras)

	// Should have defaults (3) + 1 custom, duplicate should be removed
	require.Len(t, merged, 4)

	// Custom toleration should be present
	found := false
	for _, tol := range merged {
		if tol.Key == "custom-taint" {
			found = true
			require.Equal(t, corev1.TolerationOpEqual, tol.Operator)
			require.Equal(t, "true", tol.Value)
			require.Equal(t, corev1.TaintEffectNoSchedule, tol.Effect)
			break
		}
	}
	require.True(t, found, "Custom toleration should be in merged list")
}

func TestMergeTolerationsEmpty(t *testing.T) {
	defaults := defaultTolerations()

	// Empty extras should return just defaults
	merged := mergeTolerations(defaults, nil)
	require.Equal(t, defaults, merged)

	// Empty defaults should return just extras
	extras := []corev1.Toleration{
		{Key: "custom", Operator: corev1.TolerationOpExists},
	}
	merged = mergeTolerations(nil, extras)
	require.Len(t, merged, 1)
	require.Equal(t, "custom", merged[0].Key)
}

func TestImagePullSecretsFromNames(t *testing.T) {
	names := []string{"secret1", "secret2", "secret3"}
	refs := imagePullSecretsFromNames(names)

	require.Len(t, refs, 3)
	require.Equal(t, "secret1", refs[0].Name)
	require.Equal(t, "secret2", refs[1].Name)
	require.Equal(t, "secret3", refs[2].Name)
}

func TestImagePullSecretsFromNamesEmpty(t *testing.T) {
	refs := imagePullSecretsFromNames(nil)
	require.Nil(t, refs)

	refs = imagePullSecretsFromNames([]string{})
	require.Nil(t, refs)
}

func TestNodeSelectorFromMap(t *testing.T) {
	input := map[string]string{"node-type": "infra", "zone": "us-east-1a"}
	result := nodeSelectorFromMap(input)

	require.Len(t, result, 2)
	require.Equal(t, "infra", result["node-type"])
	require.Equal(t, "us-east-1a", result["zone"])

	// Verify it's a copy, not the same map
	input["new-key"] = "new-value"
	require.Empty(t, result["new-key"])
}

func TestNodeSelectorFromMapEmpty(t *testing.T) {
	require.Nil(t, nodeSelectorFromMap(nil))
	require.Nil(t, nodeSelectorFromMap(map[string]string{}))
}
