package operator

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Common label keys following Kubernetes recommended labels.
// See: https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/
const (
	LabelName      = "app.kubernetes.io/name"
	LabelComponent = "app.kubernetes.io/component"
	LabelManagedBy = "app.kubernetes.io/managed-by"

	// ManagedByValue is the standard value for the managed-by label.
	ManagedByValue = "adx-mon-operator"
)

// resourceLabels creates a standard label set for adx-mon resources.
func resourceLabels(name, component string) map[string]string {
	return map[string]string{
		LabelName:      name,
		LabelComponent: component,
		LabelManagedBy: ManagedByValue,
	}
}

// selectorLabels creates a minimal label set for selectors.
// These should be immutable for the lifetime of the resource.
func selectorLabels(name, component string) map[string]string {
	return map[string]string{
		LabelName:      name,
		LabelComponent: component,
	}
}

// mergeLabels merges multiple label maps, with later maps taking precedence.
func mergeLabels(labelMaps ...map[string]string) map[string]string {
	result := make(map[string]string)
	for _, m := range labelMaps {
		for k, v := range m {
			result[k] = v
		}
	}
	return result
}

// ptr returns a pointer to the given value. Useful for inline pointer creation.
func ptr[T any](v T) *T {
	return &v
}

// objectMeta creates a standard ObjectMeta for namespace-scoped resources.
func objectMeta(name, namespace, component string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:      name,
		Namespace: namespace,
		Labels:    resourceLabels(name, component),
	}
}

// clusterObjectMeta creates a standard ObjectMeta for cluster-scoped resources.
// The name follows the pattern "namespace:name" to avoid conflicts.
func clusterObjectMeta(name, namespace, component string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:   namespace + ":" + name,
		Labels: resourceLabels(name, component),
	}
}

// defaultTolerations returns the standard tolerations for adx-mon workloads.
func defaultTolerations() []corev1.Toleration {
	return []corev1.Toleration{
		{
			Key:      "CriticalAddonsOnly",
			Operator: corev1.TolerationOpExists,
		},
		{
			Key:               "node.kubernetes.io/not-ready",
			Operator:          corev1.TolerationOpExists,
			Effect:            corev1.TaintEffectNoExecute,
			TolerationSeconds: ptr(int64(300)),
		},
		{
			Key:               "node.kubernetes.io/unreachable",
			Operator:          corev1.TolerationOpExists,
			Effect:            corev1.TaintEffectNoExecute,
			TolerationSeconds: ptr(int64(300)),
		},
	}
}

// mergeTolerations combines default tolerations with extra tolerations.
// Duplicates are removed based on key+effect+operator.
func mergeTolerations(defaults, extras []corev1.Toleration) []corev1.Toleration {
	result := make([]corev1.Toleration, 0, len(defaults)+len(extras))
	result = append(result, defaults...)

	// Track existing tolerations to avoid duplicates
	seen := make(map[string]struct{}, len(defaults))
	for _, t := range defaults {
		seen[builderTolerationKey(t)] = struct{}{}
	}

	for _, t := range extras {
		key := builderTolerationKey(t)
		if _, exists := seen[key]; !exists {
			result = append(result, t)
			seen[key] = struct{}{}
		}
	}

	return result
}

// builderTolerationKey creates a unique key for a toleration for deduplication.
func builderTolerationKey(t corev1.Toleration) string {
	return t.Key + "|" + string(t.Effect) + "|" + string(t.Operator) + "|" + t.Value
}

// imagePullSecretsFromNames converts a slice of secret names to LocalObjectReferences.
func imagePullSecretsFromNames(names []string) []corev1.LocalObjectReference {
	if len(names) == 0 {
		return nil
	}
	refs := make([]corev1.LocalObjectReference, len(names))
	for i, name := range names {
		refs[i] = corev1.LocalObjectReference{Name: name}
	}
	return refs
}

// nodeSelectorFromMap converts a map to a properly typed nodeSelector.
// Returns nil if the map is empty.
func nodeSelectorFromMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	result := make(map[string]string, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}
