package operator

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestBuildServiceAccount(t *testing.T) {
	cfg := &IngestorConfig{
		Name:      "test-ingestor",
		Namespace: "adx-mon",
	}

	sa := BuildServiceAccount(cfg)

	require.Equal(t, "v1", sa.APIVersion)
	require.Equal(t, "ServiceAccount", sa.Kind)
	require.Equal(t, "test-ingestor", sa.Name)
	require.Equal(t, "adx-mon", sa.Namespace)
	require.Equal(t, "test-ingestor", sa.Labels[LabelName])
	require.Equal(t, componentIngestor, sa.Labels[LabelComponent])
	require.Equal(t, ManagedByValue, sa.Labels[LabelManagedBy])
	require.NotNil(t, sa.AutomountServiceAccountToken)
	require.False(t, *sa.AutomountServiceAccountToken)
}

func TestBuildService(t *testing.T) {
	cfg := &IngestorConfig{
		Name:      "test-ingestor",
		Namespace: "adx-mon",
	}

	svc := BuildService(cfg)

	require.Equal(t, "v1", svc.APIVersion)
	require.Equal(t, "Service", svc.Kind)
	require.Equal(t, "test-ingestor", svc.Name)
	require.Equal(t, "adx-mon", svc.Namespace)
	require.Equal(t, corev1.ServiceTypeClusterIP, svc.Spec.Type)
	require.Len(t, svc.Spec.Ports, 1)
	require.Equal(t, int32(ServicePortHTTPS), svc.Spec.Ports[0].Port)
	require.Equal(t, IngestorPortHTTPS, svc.Spec.Ports[0].TargetPort.IntValue())
	require.Equal(t, "test-ingestor", svc.Spec.Selector[LabelName])
	require.Equal(t, componentIngestor, svc.Spec.Selector[LabelComponent])
}

func TestBuildStatefulSet(t *testing.T) {
	cfg := &IngestorConfig{
		Name:           "test-ingestor",
		Namespace:      "adx-mon",
		Image:          "ghcr.io/azure/adx-mon/ingestor:v1.0.0",
		Replicas:       3,
		Region:         "westus2",
		LogDestination: "CustomLogs:Ingestor",
		ClusterLabels: map[string]string{
			"region":  "westus2",
			"cluster": "test",
		},
		MetricsClusters: []string{"Metrics=https://cluster1.kusto.windows.net"},
		LogsClusters:    []string{"Logs=https://cluster2.kusto.windows.net"},
	}

	sts := BuildStatefulSet(cfg)

	// Check TypeMeta
	require.Equal(t, "apps/v1", sts.APIVersion)
	require.Equal(t, "StatefulSet", sts.Kind)

	// Check ObjectMeta
	require.Equal(t, "test-ingestor", sts.Name)
	require.Equal(t, "adx-mon", sts.Namespace)
	require.Equal(t, "test-ingestor", sts.Labels[LabelName])
	require.Equal(t, componentIngestor, sts.Labels[LabelComponent])

	// Check Spec
	require.Equal(t, "test-ingestor", sts.Spec.ServiceName)
	require.NotNil(t, sts.Spec.Replicas)
	require.Equal(t, int32(3), *sts.Spec.Replicas)
	require.NotNil(t, sts.Spec.Selector)
	require.Equal(t, "test-ingestor", sts.Spec.Selector.MatchLabels[LabelName])

	// Check Pod Template
	podSpec := sts.Spec.Template.Spec
	require.Len(t, podSpec.Containers, 1)
	require.Equal(t, componentIngestor, podSpec.Containers[0].Name)
	require.Equal(t, cfg.Image, podSpec.Containers[0].Image)
	require.Equal(t, []string{"/ingestor"}, podSpec.Containers[0].Command)

	// Check annotations
	annotations := sts.Spec.Template.ObjectMeta.Annotations
	require.Equal(t, "true", annotations["adx-mon/scrape"])
	require.Equal(t, "9091", annotations["adx-mon/port"])
	require.Equal(t, "/metrics", annotations["adx-mon/path"])
	require.Equal(t, "CustomLogs:Ingestor", annotations["adx-mon/log-destination"])

	// Check security context
	container := podSpec.Containers[0]
	require.NotNil(t, container.SecurityContext)
	require.False(t, *container.SecurityContext.AllowPrivilegeEscalation)
	require.False(t, *container.SecurityContext.Privileged)
	require.True(t, *container.SecurityContext.ReadOnlyRootFilesystem)

	// Check tolerations include defaults
	require.GreaterOrEqual(t, len(podSpec.Tolerations), 3)
	foundCritical := false
	for _, tol := range podSpec.Tolerations {
		if tol.Key == "CriticalAddonsOnly" {
			foundCritical = true
			break
		}
	}
	require.True(t, foundCritical, "Should include CriticalAddonsOnly toleration")

	// Check anti-affinity
	require.NotNil(t, podSpec.Affinity)
	require.NotNil(t, podSpec.Affinity.PodAntiAffinity)
	require.Len(t, podSpec.Affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution, 1)
}

func TestBuildStatefulSetWithImagePullSecrets(t *testing.T) {
	cfg := &IngestorConfig{
		Name:             "test-ingestor",
		Namespace:        "adx-mon",
		Image:            "ghcr.io/azure/adx-mon/ingestor:latest",
		Replicas:         1,
		ImagePullSecrets: []corev1.LocalObjectReference{{Name: "my-registry-secret"}, {Name: "another-secret"}},
	}

	sts := BuildStatefulSet(cfg)

	require.Len(t, sts.Spec.Template.Spec.ImagePullSecrets, 2)
	require.Equal(t, "my-registry-secret", sts.Spec.Template.Spec.ImagePullSecrets[0].Name)
	require.Equal(t, "another-secret", sts.Spec.Template.Spec.ImagePullSecrets[1].Name)
}

func TestBuildStatefulSetWithNodeSelector(t *testing.T) {
	cfg := &IngestorConfig{
		Name:      "test-ingestor",
		Namespace: "adx-mon",
		Image:     "ghcr.io/azure/adx-mon/ingestor:latest",
		Replicas:  1,
		NodeSelector: map[string]string{
			"agentpool": "infra",
			"zone":      "us-east-1a",
		},
	}

	sts := BuildStatefulSet(cfg)

	require.Len(t, sts.Spec.Template.Spec.NodeSelector, 2)
	require.Equal(t, "infra", sts.Spec.Template.Spec.NodeSelector["agentpool"])
	require.Equal(t, "us-east-1a", sts.Spec.Template.Spec.NodeSelector["zone"])
}

func TestBuildStatefulSetWithExtraTolerations(t *testing.T) {
	cfg := &IngestorConfig{
		Name:      "test-ingestor",
		Namespace: "adx-mon",
		Image:     "ghcr.io/azure/adx-mon/ingestor:latest",
		Replicas:  1,
		Tolerations: []corev1.Toleration{
			{
				Key:      "agentpool",
				Operator: corev1.TolerationOpEqual,
				Value:    "infra",
				Effect:   corev1.TaintEffectNoSchedule,
			},
		},
	}

	sts := BuildStatefulSet(cfg)

	// Should have default tolerations (3) + extra (1)
	require.Len(t, sts.Spec.Template.Spec.Tolerations, 4)

	foundCustom := false
	for _, tol := range sts.Spec.Template.Spec.Tolerations {
		if tol.Key == "agentpool" && tol.Value == "infra" {
			foundCustom = true
			break
		}
	}
	require.True(t, foundCustom, "Custom toleration should be present")
}

func TestBuildStatefulSetWithTLSSecret(t *testing.T) {
	cfg := &IngestorConfig{
		Name:          "test-ingestor",
		Namespace:     "adx-mon",
		Image:         "ghcr.io/azure/adx-mon/ingestor:latest",
		Replicas:      1,
		TLSSecretName: "ingestor-tls",
	}

	sts := BuildStatefulSet(cfg)

	// Check that TLS volume is added
	volumes := sts.Spec.Template.Spec.Volumes
	foundTLSVolume := false
	for _, vol := range volumes {
		if vol.Name == "ingestor-certs" {
			foundTLSVolume = true
			require.NotNil(t, vol.Secret)
			require.Equal(t, "ingestor-tls", vol.Secret.SecretName)
			break
		}
	}
	require.True(t, foundTLSVolume, "TLS volume should be present")

	// Check that TLS volume mount is added
	volumeMounts := sts.Spec.Template.Spec.Containers[0].VolumeMounts
	foundTLSMount := false
	for _, mount := range volumeMounts {
		if mount.Name == "ingestor-certs" {
			foundTLSMount = true
			require.Equal(t, IngestorTLSCertsPath, mount.MountPath)
			require.True(t, mount.ReadOnly)
			break
		}
	}
	require.True(t, foundTLSMount, "TLS volume mount should be present")

	// Check args include TLS paths
	args := sts.Spec.Template.Spec.Containers[0].Args
	foundCACert := false
	foundKey := false
	for _, arg := range args {
		if arg == "--ca-cert="+IngestorTLSCertsPath+"/tls.crt" {
			foundCACert = true
		}
		if arg == "--key="+IngestorTLSCertsPath+"/tls.key" {
			foundKey = true
		}
	}
	require.True(t, foundCACert, "Args should include --ca-cert")
	require.True(t, foundKey, "Args should include --key")
}

func TestBuildStatefulSetWithTLSHostPath(t *testing.T) {
	cfg := &IngestorConfig{
		Name:        "test-ingestor",
		Namespace:   "adx-mon",
		Image:       "ghcr.io/azure/adx-mon/ingestor:latest",
		Replicas:    1,
		TLSHostPath: "/etc/pki/tls/certs",
	}

	sts := BuildStatefulSet(cfg)

	// Check that TLS volume is a hostPath
	volumes := sts.Spec.Template.Spec.Volumes
	foundTLSVolume := false
	for _, vol := range volumes {
		if vol.Name == "ingestor-certs" {
			foundTLSVolume = true
			require.NotNil(t, vol.HostPath)
			require.Equal(t, "/etc/pki/tls/certs", vol.HostPath.Path)
			break
		}
	}
	require.True(t, foundTLSVolume, "TLS hostPath volume should be present")
}

func TestBuildIngestorArgs(t *testing.T) {
	cfg := &IngestorConfig{
		Region: "westus2",
		ClusterLabels: map[string]string{
			"region":  "westus2",
			"cluster": "prod",
		},
		MetricsClusters: []string{"Metrics=https://metrics.kusto.net"},
		LogsClusters:    []string{"Logs=https://logs.kusto.net"},
	}

	args := buildIngestorArgs(cfg)

	// Check required args are present
	require.Contains(t, args, "--storage-dir="+IngestorStorageDir)
	require.Contains(t, args, "--max-segment-age="+IngestorMaxSegmentAge)
	require.Contains(t, args, "--max-disk-usage="+IngestorMaxDiskUsage)
	require.Contains(t, args, "--insecure-skip-verify")

	// Check region arg
	require.Contains(t, args, "--region=westus2")

	// Check cluster labels args (sorted order)
	require.Contains(t, args, "--cluster-labels=cluster=prod")
	require.Contains(t, args, "--cluster-labels=region=westus2")

	// Check endpoint args
	require.Contains(t, args, "--metrics-kusto-endpoints=Metrics=https://metrics.kusto.net")
	require.Contains(t, args, "--logs-kusto-endpoints=Logs=https://logs.kusto.net")
}

func TestBuildIngestorEnv(t *testing.T) {
	cfg := &IngestorConfig{
		AzureClientID: "my-client-id",
		AzureResource: "https://federated.kusto.net",
	}

	env := buildIngestorEnv(cfg)

	// Check required env vars
	envMap := make(map[string]string)
	for _, e := range env {
		envMap[e.Name] = e.Value
	}

	require.Equal(t, "INFO", envMap["LOG_LEVEL"])
	require.Equal(t, "http2client=0", envMap["GODEBUG"])
	require.Equal(t, "my-client-id", envMap["AZURE_CLIENT_ID"])
	require.Equal(t, "https://federated.kusto.net", envMap["AZURE_RESOURCE"])
}

func TestBuildIngestorEnvMinimal(t *testing.T) {
	cfg := &IngestorConfig{}

	env := buildIngestorEnv(cfg)

	// Only LOG_LEVEL and GODEBUG should be present
	require.Len(t, env, 2)
	envMap := make(map[string]string)
	for _, e := range env {
		envMap[e.Name] = e.Value
	}
	require.Equal(t, "INFO", envMap["LOG_LEVEL"])
	require.Equal(t, "http2client=0", envMap["GODEBUG"])
}

func TestFilterNonDefaultTolerations(t *testing.T) {
	tolerations := []corev1.Toleration{
		// Default toleration - should be filtered out
		{Key: "CriticalAddonsOnly", Operator: corev1.TolerationOpExists},
		// Custom toleration - should be kept
		{Key: "custom-taint", Operator: corev1.TolerationOpEqual, Value: "true", Effect: corev1.TaintEffectNoSchedule},
		// Another default - should be filtered out
		{Key: "node.kubernetes.io/not-ready", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoExecute, TolerationSeconds: ptr(int64(300))},
	}

	filtered := filterNonDefaultTolerations(tolerations)

	require.Len(t, filtered, 1)
	require.Equal(t, "custom-taint", filtered[0].Key)
}

func TestTolerationsEqual(t *testing.T) {
	tolsA := []corev1.Toleration{
		{Key: "key1", Operator: corev1.TolerationOpEqual, Value: "val1", Effect: corev1.TaintEffectNoSchedule},
		{Key: "key2", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoExecute},
	}

	tolsB := []corev1.Toleration{
		{Key: "key2", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoExecute},
		{Key: "key1", Operator: corev1.TolerationOpEqual, Value: "val1", Effect: corev1.TaintEffectNoSchedule},
	}

	// Same tolerations in different order should be equal
	require.True(t, TolerationsEqual(tolsA, tolsB))

	// Different tolerations should not be equal
	tolsC := []corev1.Toleration{
		{Key: "key1", Operator: corev1.TolerationOpEqual, Value: "val1", Effect: corev1.TaintEffectNoSchedule},
	}
	require.False(t, TolerationsEqual(tolsA, tolsC))

	// Empty slices should be equal
	require.True(t, TolerationsEqual(nil, nil))
	require.True(t, TolerationsEqual([]corev1.Toleration{}, []corev1.Toleration{}))
}
