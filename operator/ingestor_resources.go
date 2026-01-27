package operator

import (
	"fmt"
	"os"
	"slices"
	"strings"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// Component name for ingestor resources.
const componentIngestor = "ingestor"

// Ingestor container and resource constants.
// These can be made configurable in the future via CRD fields or operator flags.
const (
	// Container resource requests
	IngestorCPURequest    = "8"
	IngestorMemoryRequest = "3Gi"

	// Network ports
	IngestorPortHTTPS   = 9090
	IngestorPortMetrics = 9091
	ServicePortHTTPS    = 443

	// Volume paths
	IngestorDataPath         = "/mnt/data"
	IngestorHostDataPath     = "/mnt/ingestor"
	IngestorCACertsPath      = "/etc/ssl/certs"
	IngestorPKICertsPath     = "/etc/pki/ca-trust/extracted"
	IngestorTLSCertsPath     = "/etc/ssl/ingestor"
	IngestorHostCACertsPath  = "/etc/ssl/certs"
	IngestorHostPKICertsPath = "/etc/pki/ca-trust/extracted"

	// Ingestor command-line argument defaults
	IngestorStorageDir      = "/mnt/data"
	IngestorMaxSegmentAge   = "5s"
	IngestorMaxDiskUsage    = "68719476736" // 64GB
	IngestorMaxTransferSize = "10485760"    // 10MB
	IngestorMaxConnections  = "1000"
)

// IngestorConfig contains all the configuration needed to build ingestor resources.
// This replaces the ingestorTemplateData struct for type-safe resource construction.
type IngestorConfig struct {
	// Basic identification
	Name      string
	Namespace string

	// Container configuration
	Image    string
	Replicas int32

	// ADX cluster endpoints
	MetricsClusters []string
	LogsClusters    []string
	LogDestination  string

	// Azure identity
	AzureClientID string
	AzureResource string // Federated cluster endpoint

	// Cluster labels for --cluster-labels args
	ClusterLabels map[string]string
	Region        string

	// TLS configuration (mutually exclusive)
	TLSSecretName string
	TLSHostPath   string

	// Scheduling configuration (inherited from operator)
	ImagePullSecrets []corev1.LocalObjectReference
	NodeSelector     map[string]string
	Tolerations      []corev1.Toleration
}

// BuildServiceAccount creates a ServiceAccount for the ingestor.
func BuildServiceAccount(cfg *IngestorConfig) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ServiceAccount",
		},
		ObjectMeta:                   objectMeta(cfg.Name, cfg.Namespace, componentIngestor),
		AutomountServiceAccountToken: ptr(false),
	}
}

// BuildService creates a Service for the ingestor.
func BuildService(cfg *IngestorConfig) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: objectMeta(cfg.Name, cfg.Namespace, componentIngestor),
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: selectorLabels(cfg.Name, componentIngestor),
			Ports: []corev1.ServicePort{
				{
					Port:       ServicePortHTTPS,
					TargetPort: intstr.FromInt(IngestorPortHTTPS),
				},
			},
		},
	}
}

// BuildStatefulSet creates a StatefulSet for the ingestor.
func BuildStatefulSet(cfg *IngestorConfig) *appsv1.StatefulSet {
	// Build container args
	args := buildIngestorArgs(cfg)

	// Build container env vars
	env := buildIngestorEnv(cfg)

	// Build volumes and volume mounts
	volumes, volumeMounts := buildIngestorVolumes(cfg)

	// Build tolerations (defaults + extras from operator)
	tolerations := mergeTolerations(defaultTolerations(), cfg.Tolerations)

	// Build pod spec
	podSpec := corev1.PodSpec{
		AutomountServiceAccountToken: ptr(true),
		SecurityContext:              &corev1.PodSecurityContext{},
		ServiceAccountName:           cfg.Name,
		Containers: []corev1.Container{
			{
				Name:  componentIngestor,
				Image: cfg.Image,
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: ptr(false),
					Privileged:               ptr(false),
					ReadOnlyRootFilesystem:   ptr(true),
					Capabilities: &corev1.Capabilities{
						Drop: []corev1.Capability{"ALL"},
					},
				},
				Ports: []corev1.ContainerPort{
					{ContainerPort: IngestorPortHTTPS, Name: componentIngestor},
					{ContainerPort: IngestorPortMetrics, Name: "metrics"},
				},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse(IngestorCPURequest),
						corev1.ResourceMemory: resource.MustParse(IngestorMemoryRequest),
					},
				},
				Env:          env,
				Command:      []string{"/ingestor"},
				Args:         args,
				VolumeMounts: volumeMounts,
				ReadinessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Path:   "/readyz",
							Port:   intstr.FromInt(IngestorPortHTTPS),
							Scheme: corev1.URISchemeHTTPS,
						},
					},
					InitialDelaySeconds: 10,
					PeriodSeconds:       5,
				},
			},
		},
		Affinity: &corev1.Affinity{
			PodAntiAffinity: &corev1.PodAntiAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      LabelName,
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{cfg.Name},
								},
								{
									Key:      LabelComponent,
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{componentIngestor},
								},
							},
						},
						TopologyKey: "kubernetes.io/hostname",
					},
				},
			},
		},
		Volumes:     volumes,
		Tolerations: tolerations,
	}

	// Add imagePullSecrets if configured
	podSpec.ImagePullSecrets = normalizeLocalObjectReferences(cfg.ImagePullSecrets)

	// Add nodeSelector if configured
	if len(cfg.NodeSelector) > 0 {
		podSpec.NodeSelector = nodeSelectorFromMap(cfg.NodeSelector)
	}

	annotations := map[string]string{
		"adx-mon/scrape":      "true",
		"adx-mon/port":        fmt.Sprintf("%d", IngestorPortMetrics),
		"adx-mon/path":        "/metrics",
		"adx-mon/log-parsers": "json",
	}
	if cfg.LogDestination != "" {
		annotations["adx-mon/log-destination"] = cfg.LogDestination
	}

	return &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "StatefulSet",
		},
		ObjectMeta: objectMeta(cfg.Name, cfg.Namespace, componentIngestor),
		Spec: appsv1.StatefulSetSpec{
			ServiceName: cfg.Name,
			Replicas:    ptr(cfg.Replicas),
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: selectorLabels(cfg.Name, componentIngestor),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      resourceLabels(cfg.Name, componentIngestor),
					Annotations: annotations,
				},
				Spec: podSpec,
			},
		},
	}
}

// buildIngestorArgs constructs the command-line arguments for the ingestor container.
func buildIngestorArgs(cfg *IngestorConfig) []string {
	args := []string{
		"--storage-dir=" + IngestorStorageDir,
		"--max-segment-age=" + IngestorMaxSegmentAge,
		"--max-disk-usage=" + IngestorMaxDiskUsage,
		"--max-transfer-size=" + IngestorMaxTransferSize,
		"--insecure-skip-verify",
		"--max-connections=" + IngestorMaxConnections,
	}

	// TLS configuration
	if cfg.TLSSecretName != "" || cfg.TLSHostPath != "" {
		args = append(args, "--ca-cert="+IngestorTLSCertsPath+"/tls.crt")
		args = append(args, "--key="+IngestorTLSCertsPath+"/tls.key")
	}

	// Region
	if cfg.Region != "" {
		args = append(args, "--region="+cfg.Region)
	}

	// Cluster labels
	for k, v := range cfg.ClusterLabels {
		args = append(args, fmt.Sprintf("--cluster-labels=%s=%s", k, v))
	}

	// Metrics kusto endpoints
	for _, endpoint := range cfg.MetricsClusters {
		args = append(args, "--metrics-kusto-endpoints="+endpoint)
	}

	// Logs kusto endpoints
	for _, endpoint := range cfg.LogsClusters {
		args = append(args, "--logs-kusto-endpoints="+endpoint)
	}

	return args
}

// buildIngestorEnv constructs the environment variables for the ingestor container.
func buildIngestorEnv(cfg *IngestorConfig) []corev1.EnvVar {
	env := []corev1.EnvVar{
		{Name: "LOG_LEVEL", Value: "INFO"},
		{Name: "GODEBUG", Value: "http2client=0"},
	}

	if cfg.AzureResource != "" {
		env = append(env, corev1.EnvVar{Name: "AZURE_RESOURCE", Value: cfg.AzureResource})
	}

	if cfg.AzureClientID != "" {
		env = append(env, corev1.EnvVar{Name: "AZURE_CLIENT_ID", Value: cfg.AzureClientID})
	}

	return env
}

// buildIngestorVolumes constructs the volumes and volume mounts for the ingestor container.
func buildIngestorVolumes(cfg *IngestorConfig) ([]corev1.Volume, []corev1.VolumeMount) {
	volumes := []corev1.Volume{
		{
			Name: "ca-certs",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: IngestorHostCACertsPath,
					Type: hostPathTypePtr(corev1.HostPathDirectory),
				},
			},
		},
		{
			Name: "etc-pki-ca-certs",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: IngestorHostPKICertsPath,
					Type: hostPathTypePtr(corev1.HostPathDirectoryOrCreate),
				},
			},
		},
		{
			Name: "metrics",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: IngestorHostDataPath,
				},
			},
		},
	}

	volumeMounts := []corev1.VolumeMount{
		{Name: "metrics", MountPath: IngestorDataPath},
		{Name: "etc-pki-ca-certs", MountPath: IngestorPKICertsPath, ReadOnly: true},
		{Name: "ca-certs", MountPath: IngestorCACertsPath, ReadOnly: true},
	}

	// Add TLS volume and mount if configured
	if cfg.TLSSecretName != "" {
		volumes = append(volumes, corev1.Volume{
			Name: "ingestor-certs",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: cfg.TLSSecretName,
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "ingestor-certs",
			MountPath: IngestorTLSCertsPath,
			ReadOnly:  true,
		})
	} else if cfg.TLSHostPath != "" {
		volumes = append(volumes, corev1.Volume{
			Name: "ingestor-certs",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: cfg.TLSHostPath,
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "ingestor-certs",
			MountPath: IngestorTLSCertsPath,
			ReadOnly:  true,
		})
	}

	return volumes, volumeMounts
}

// hostPathTypePtr returns a pointer to a HostPathType.
func hostPathTypePtr(t corev1.HostPathType) *corev1.HostPathType {
	return &t
}

// NewIngestorConfigFromReconciler creates an IngestorConfig from the reconciler state
// and the Ingestor CRD spec. This bridges the gap between the reconciler's data
// gathering (ADXCluster lookup, etc.) and the resource builders.
func NewIngestorConfigFromReconciler(
	ingestor *adxmonv1.Ingestor,
	metricsClusters, logsClusters []string,
	azureResource string,
	clusterLabels map[string]string,
	imagePullSecrets []corev1.LocalObjectReference,
	nodeSelector map[string]string,
	tolerations []corev1.Toleration,
) *IngestorConfig {
	// Extract TLS configuration
	var tlsSecretName, tlsHostPath string
	if ingestor.Spec.TLS != nil {
		if ingestor.Spec.TLS.SecretRef != nil {
			tlsSecretName = ingestor.Spec.TLS.SecretRef.Name
		}
		if ingestor.Spec.TLS.HostPath != "" {
			tlsHostPath = ingestor.Spec.TLS.HostPath
		}
	}

	// Get region from cluster labels
	region := ""
	if clusterLabels != nil {
		region = clusterLabels["region"]
	}

	// Filter tolerations to remove defaults (they're added by the builder)
	extraTolerations := filterNonDefaultTolerations(tolerations)

	return &IngestorConfig{
		Name:             ingestor.Name,
		Namespace:        ingestor.Namespace,
		Image:            ingestor.Spec.Image,
		Replicas:         ingestor.Spec.Replicas,
		MetricsClusters:  metricsClusters,
		LogsClusters:     logsClusters,
		LogDestination:   ingestor.Spec.LogDestination,
		AzureClientID:    os.Getenv("AZURE_CLIENT_ID"),
		AzureResource:    azureResource,
		ClusterLabels:    clusterLabels,
		Region:           region,
		TLSSecretName:    tlsSecretName,
		TLSHostPath:      tlsHostPath,
		ImagePullSecrets: imagePullSecrets,
		NodeSelector:     nodeSelector,
		Tolerations:      extraTolerations,
	}
}

// filterNonDefaultTolerations removes tolerations that match the defaults.
// This prevents duplicates when defaults are merged in BuildStatefulSet.
func filterNonDefaultTolerations(tolerations []corev1.Toleration) []corev1.Toleration {
	if len(tolerations) == 0 {
		return nil
	}

	result := make([]corev1.Toleration, 0, len(tolerations))
	for _, t := range tolerations {
		if !isDefaultToleration(t) {
			result = append(result, t)
		}
	}
	return result
}

// isDefaultToleration checks if a toleration matches one of the default tolerations.
func isDefaultToleration(t corev1.Toleration) bool {
	switch {
	case t.Key == "CriticalAddonsOnly" && t.Operator == corev1.TolerationOpExists:
		return true
	case t.Key == "node.kubernetes.io/not-ready" &&
		t.Operator == corev1.TolerationOpExists &&
		t.Effect == corev1.TaintEffectNoExecute &&
		t.TolerationSeconds != nil &&
		*t.TolerationSeconds == 300:
		return true
	case t.Key == "node.kubernetes.io/unreachable" &&
		t.Operator == corev1.TolerationOpExists &&
		t.Effect == corev1.TaintEffectNoExecute &&
		t.TolerationSeconds != nil &&
		*t.TolerationSeconds == 300:
		return true
	default:
		return false
	}
}

// TolerationsEqual compares two toleration slices for equality.
// Used for change detection during reconciliation.
func TolerationsEqual(a, b []corev1.Toleration) bool {
	if len(a) != len(b) {
		return false
	}
	// Create sorted copies for comparison
	aCopy := make([]string, len(a))
	bCopy := make([]string, len(b))
	for i, t := range a {
		aCopy[i] = tolerationString(t)
	}
	for i, t := range b {
		bCopy[i] = tolerationString(t)
	}
	slices.Sort(aCopy)
	slices.Sort(bCopy)
	for i := range aCopy {
		if aCopy[i] != bCopy[i] {
			return false
		}
	}
	return true
}

func tolerationString(t corev1.Toleration) string {
	seconds := ""
	if t.TolerationSeconds != nil {
		seconds = fmt.Sprintf("%d", *t.TolerationSeconds)
	}
	return strings.Join([]string{t.Key, string(t.Operator), t.Value, string(t.Effect), seconds}, "|")
}
