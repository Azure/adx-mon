package operator

import (
	"slices"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	corev1 "k8s.io/api/core/v1"
)

// kvPair is used for deterministic template rendering of maps.
type kvPair struct {
	Key   string
	Value string
}

// tmplToleration is a template-friendly representation of corev1.Toleration.
// It mirrors the structure used in YAML templates and supports optional tolerationSeconds.
type tmplToleration struct {
	Key                  string
	Operator             string
	Value                string
	Effect               string
	TolerationSeconds    int64
	HasTolerationSeconds bool
}

func normalizeLocalObjectReferences(refs []corev1.LocalObjectReference) []corev1.LocalObjectReference {
	if len(refs) == 0 {
		return nil
	}
	out := make([]corev1.LocalObjectReference, len(refs))
	copy(out, refs)
	return out
}

func normalizeStringMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func normalizeTolerations(tols []corev1.Toleration) []corev1.Toleration {
	if len(tols) == 0 {
		return nil
	}
	out := make([]corev1.Toleration, len(tols))
	copy(out, tols)
	return out
}

func imagePullSecretsToNames(secrets []corev1.LocalObjectReference) []string {
	if len(secrets) == 0 {
		return nil
	}
	names := make([]string, 0, len(secrets))
	for _, s := range secrets {
		if s.Name != "" {
			names = append(names, s.Name)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// ingestorPodPolicy extracts scheduling configuration for the ingestor workload.
// Ingestor always includes default tolerations (see defaultTolerations), and these are treated as extras.
func ingestorPodPolicy(policy *adxmonv1.PodPolicy) (secrets []corev1.LocalObjectReference, nodeSelector map[string]string, tolerations []corev1.Toleration) {
	if policy == nil {
		return nil, nil, nil
	}
	// Nil vs empty matters on decode, but for the workload spec, nil and empty are equivalent.
	secrets = normalizeLocalObjectReferences(policy.ImagePullSecrets)
	nodeSelector = normalizeStringMap(policy.NodeSelector)

	// For ingestor, tolerations are additive on top of the defaults.
	if policy.Tolerations != nil {
		tolerations = normalizeTolerations(policy.Tolerations)
	}
	return secrets, nodeSelector, tolerations
}
