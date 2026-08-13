package operator

import (
	context "context"
	"errors"
	"testing"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type failRoleCreateClient struct {
	client.Client
}

func (c *failRoleCreateClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if _, ok := obj.(*rbacv1.Role); ok {
		return errors.New("role creation denied")
	}
	return c.Client.Create(ctx, obj, opts...)
}

func TestIngestorReconciler_IsReady(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))

	ingestor := &adxmonv1.Ingestor{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "adx-mon.azure.com/v1",
			Kind:       "Ingestor",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ingestor",
			Namespace: "default",
		},
		Spec: adxmonv1.IngestorSpec{
			Replicas:           2,
			ADXClusterSelector: &metav1.LabelSelector{},
		},
	}

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ingestor",
			Namespace: "default",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: to.Ptr(int32(2)),
		},
		Status: appsv1.StatefulSetStatus{
			ReadyReplicas: 2,
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&adxmonv1.Ingestor{}).
		Build()
	require.NoError(t, client.Create(context.Background(), ingestor))
	require.NoError(t, client.Create(context.Background(), sts))
	r := &IngestorReconciler{Client: client, Scheme: scheme}

	// Ready case
	result, err := r.IsReady(context.Background(), ingestor)
	require.NoError(t, err)
	require.True(t, result.IsZero())

	// Not ready case
	sts.Spec.Replicas = to.Ptr(int32(3))
	require.NoError(t, client.Update(context.Background(), sts))

	result, err = r.IsReady(context.Background(), ingestor)
	require.NoError(t, err)
	require.False(t, result.IsZero())
}

func TestIngestorReconciler_ReconcileComponent(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))

	ingestor := &adxmonv1.Ingestor{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "adx-mon.azure.com/v1",
			Kind:       "Ingestor",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ingestor",
			Namespace: "default",
		},
		Spec: adxmonv1.IngestorSpec{
			Replicas:           2,
			Image:              "test-image:v1",
			ADXClusterSelector: &metav1.LabelSelector{},
		},
	}
	require.NoError(t, ingestor.Spec.StoreAppliedProvisioningState())

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ingestor",
			Namespace: "default",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: to.Ptr(int32(2)),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "ingestor",
						Image: "test-image:v1",
						Args:  []string{"--foo=bar"},
					}},
				},
			},
		},
		Status: appsv1.StatefulSetStatus{
			ReadyReplicas: 2,
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&adxmonv1.Ingestor{}).
		Build()
	require.NoError(t, client.Create(context.Background(), ingestor))
	require.NoError(t, client.Create(context.Background(), sts))

	r := &IngestorReconciler{Client: client, Scheme: scheme}

	// No update needed
	result, err := r.ReconcileComponent(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-ingestor",
			Namespace: "default",
		},
	})
	require.NoError(t, err)
	require.True(t, result.IsZero())

	// Update image to trigger update path
	sts.Spec.Template.Spec.Containers[0].Image = "old-image:v1"
	require.NoError(t, client.Update(context.Background(), sts))

	result, err = r.ReconcileComponent(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-ingestor",
			Namespace: "default",
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsZero())
}

func TestIngestorReconciler_CreateIngestor(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))
	require.NoError(t, clientgoscheme.AddToScheme(scheme))

	cluster := &adxmonv1.ADXCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "adx-mon.azure.com/v1",
			Kind:       "ADXCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
			Labels: map[string]string{
				"app": "test-cluster",
			},
		},
		Spec: adxmonv1.ADXClusterSpec{
			ClusterName: "test-cluster",
			Endpoint:    "https://bring-your-own-adx-cluster",
			Databases: []adxmonv1.ADXClusterDatabaseSpec{
				{
					DatabaseName:  "Metrics",
					TelemetryType: adxmonv1.DatabaseTelemetryMetrics,
				},
				{
					DatabaseName:  "Logs",
					TelemetryType: adxmonv1.DatabaseTelemetryLogs,
				},
			},
		},
		Status: adxmonv1.ADXClusterStatus{
			Conditions: []metav1.Condition{
				{
					Type:               adxmonv1.ADXClusterConditionOwner,
					Status:             metav1.ConditionTrue,
					ObservedGeneration: 1,
					LastTransitionTime: metav1.Now(),
					Reason:             "Ready",
					Message:            "The ADX cluster is ready",
				},
			},
		},
	}

	// Minimal Ingestor CRD
	ingestor := &adxmonv1.Ingestor{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "adx-mon.azure.com/v1",
			Kind:       "Ingestor",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ingestor",
			Namespace: "default",
		},
		Spec: adxmonv1.IngestorSpec{
			ADXClusterSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "test-cluster",
				},
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&adxmonv1.Ingestor{}).
		Build()

	reconciler := &IngestorReconciler{Client: client, Scheme: scheme, waitForReadyReason: "WaitForReady"}

	// Create the Ingestor resource in the fake client
	require.NoError(t, client.Create(context.Background(), cluster))
	require.NoError(t, client.Create(context.Background(), ingestor))

	result, err := reconciler.CreateIngestor(context.Background(), ingestor)
	// Should not error, should requeue
	require.NoError(t, err)
	require.NotNil(t, result)

	// Check that a status condition was set
	updated := &adxmonv1.Ingestor{}
	require.NoError(t, client.Get(context.Background(), types.NamespacedName{
		Name:      "test-ingestor",
		Namespace: "default",
	}, updated))
	found := false
	for _, cond := range updated.Status.Conditions {
		if cond.Type == adxmonv1.IngestorConditionOwner {
			found = true
			break
		}
	}
	require.True(t, found, "Expected status condition to be set")
}

func TestIngestorReconciler_handleADXClusterSelectorChange(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))

	// Initial Ingestor Spec (will be stored in annotation)
	initialSpec := adxmonv1.IngestorSpec{
		Replicas: 1,
		Image:    "initial-image:v1",
		ADXClusterSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"env": "prod"},
		},
	}
	ingestor := &adxmonv1.Ingestor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ingestor",
			Namespace: "default",
		},
		Spec: initialSpec,
	}
	// Store the initial spec in the annotation
	require.NoError(t, ingestor.Spec.StoreAppliedProvisioningState())

	// Initial StatefulSet
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ingestor",
			Namespace: "default",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: to.Ptr(int32(1)),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "ingestor",
						Image: "initial-image:v1",
						Args: []string{
							"--metrics-kusto-endpoints=OldMetricsDB=https://oldcluster.kusto.windows.net",
							"--logs-kusto-endpoints=OldLogsDB=https://oldcluster.kusto.windows.net",
							"--other-arg=value",
						},
					}},
				},
			},
		},
	}

	// ADX Cluster matching the *new* selector
	newCluster := &adxmonv1.ADXCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "new-cluster",
			Namespace: "default",
			Labels:    map[string]string{"env": "staging"}, // Matches the new selector
		},
		Spec: adxmonv1.ADXClusterSpec{
			Endpoint: "https://newcluster.kusto.windows.net",
			Databases: []adxmonv1.ADXClusterDatabaseSpec{
				{DatabaseName: "NewMetricsDB", TelemetryType: adxmonv1.DatabaseTelemetryMetrics},
				{DatabaseName: "NewLogsDB", TelemetryType: adxmonv1.DatabaseTelemetryLogs},
			},
		},
		Status: adxmonv1.ADXClusterStatus{
			Conditions: []metav1.Condition{
				{Type: adxmonv1.ADXClusterConditionOwner, Status: metav1.ConditionTrue}, // Mark as ready
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ingestor, sts, newCluster). // Add initial objects
		WithStatusSubresource(&adxmonv1.Ingestor{}, &adxmonv1.ADXCluster{}).
		Build()

	r := &IngestorReconciler{Client: client, Scheme: scheme}

	// --- Test Case 1: Selector Changed ---

	// Load the stored (initial) spec from the annotations
	storedSpec, err := ingestor.Spec.LoadAppliedProvisioningState()
	require.NoError(t, err)
	require.NotNil(t, storedSpec)

	// Update the ingestor spec with the new selector
	updatedIngestor := ingestor.DeepCopy()
	updatedIngestor.Spec.ADXClusterSelector = &metav1.LabelSelector{
		MatchLabels: map[string]string{"env": "staging"}, // New selector
	}

	// Call the function
	changed, err := r.handleADXClusterSelectorChange(context.Background(), sts, updatedIngestor, storedSpec)
	require.NoError(t, err)
	require.True(t, changed, "Expected change to be detected")

	// Verify the args were updated
	expectedArgs := []string{
		"--other-arg=value", // Other args should remain
		"--metrics-kusto-endpoints=NewMetricsDB=https://newcluster.kusto.windows.net",
		"--logs-kusto-endpoints=NewLogsDB=https://newcluster.kusto.windows.net",
	}
	require.ElementsMatch(t, expectedArgs, sts.Spec.Template.Spec.Containers[0].Args, "Args mismatch after selector change")

	// --- Test Case 2: Selector Not Changed ---

	// Reset STS args for the next test
	sts.Spec.Template.Spec.Containers[0].Args = []string{
		"--metrics-kusto-endpoints=NewMetricsDB=https://newcluster.kusto.windows.net",
		"--logs-kusto-endpoints=NewLogsDB=https://newcluster.kusto.windows.net",
		"--other-arg=value",
	}
	// Store the *new* spec in the annotation now
	require.NoError(t, updatedIngestor.Spec.StoreAppliedProvisioningState())
	// Update the ingestor in the fake client to reflect the stored annotation
	require.NoError(t, client.Update(context.Background(), updatedIngestor))

	// Load the currently stored spec (which matches the current spec)
	storedSpecNow, err := updatedIngestor.Spec.LoadAppliedProvisioningState()
	require.NoError(t, err)
	require.NotNil(t, storedSpecNow)

	// Call the function again, ingestor spec and stored spec match
	changed, err = r.handleADXClusterSelectorChange(context.Background(), sts, updatedIngestor, storedSpecNow)
	require.NoError(t, err)
	require.False(t, changed, "Expected no change when selector is the same")

	// Verify args did not change
	require.ElementsMatch(t, expectedArgs, sts.Spec.Template.Spec.Containers[0].Args, "Args should not change when selector is the same")

	// --- Test Case 3: Stored Spec is Nil ---
	changed, err = r.handleADXClusterSelectorChange(context.Background(), sts, updatedIngestor, nil)
	require.NoError(t, err)
	require.False(t, changed, "Expected no change when stored spec is nil")
	require.ElementsMatch(t, expectedArgs, sts.Spec.Template.Spec.Containers[0].Args, "Args should not change when stored spec is nil")
}

func TestIngestorReconciler_SecurityControlsValidation(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))
	require.NoError(t, clientgoscheme.AddToScheme(scheme))

	ingestor := &adxmonv1.Ingestor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ingestor",
			Namespace: "default",
		},
		Spec: adxmonv1.IngestorSpec{
			Image: "test-image:v1",
		},
	}

	cluster := &adxmonv1.ADXCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
			Labels: map[string]string{
				"app": "adx-mon",
			},
		},
		Spec: adxmonv1.ADXClusterSpec{
			Endpoint: "https://test.kusto.windows.net",
			Databases: []adxmonv1.ADXClusterDatabaseSpec{
				{DatabaseName: "MetricsDB", TelemetryType: adxmonv1.DatabaseTelemetryMetrics},
				{DatabaseName: "LogsDB", TelemetryType: adxmonv1.DatabaseTelemetryLogs},
			},
		},
		Status: adxmonv1.ADXClusterStatus{
			Conditions: []metav1.Condition{
				{Type: adxmonv1.ADXClusterConditionOwner, Status: metav1.ConditionTrue},
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&adxmonv1.Ingestor{}).
		Build()

	reconciler := &IngestorReconciler{Client: client, Scheme: scheme, waitForReadyReason: "WaitForReady"}

	require.NoError(t, client.Create(context.Background(), cluster))
	require.NoError(t, client.Create(context.Background(), ingestor))

	result, err := reconciler.CreateIngestor(context.Background(), ingestor)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify that a statefulset was created
	sts := &appsv1.StatefulSet{}
	require.NoError(t, client.Get(context.Background(), types.NamespacedName{
		Name:      "ingestor",
		Namespace: "default",
	}, sts))

	// Validate pod security context (c0055 - Linux hardening)
	require.NotNil(t, sts.Spec.Template.Spec.SecurityContext, "Pod security context should be set")
	// Note: runAsNonRoot, runAsUser, runAsGroup, and fsGroup are omitted for ingestor as it needs root access to write to /mnt/ingestor

	// Validate container security context
	require.Len(t, sts.Spec.Template.Spec.Containers, 1, "Should have exactly one container")
	container := sts.Spec.Template.Spec.Containers[0]
	require.NotNil(t, container.SecurityContext, "Container security context should be set")

	// c0016 - Allow privilege escalation should be false
	require.NotNil(t, container.SecurityContext.AllowPrivilegeEscalation, "allowPrivilegeEscalation should be set")
	require.False(t, *container.SecurityContext.AllowPrivilegeEscalation, "allowPrivilegeEscalation should be false")

	// c0013 - Privileged containers should be false
	require.NotNil(t, container.SecurityContext.Privileged, "privileged should be set")
	require.False(t, *container.SecurityContext.Privileged, "privileged should be false")

	// c0017 - Immutable container filesystem
	require.NotNil(t, container.SecurityContext.ReadOnlyRootFilesystem, "readOnlyRootFilesystem should be set")
	require.True(t, *container.SecurityContext.ReadOnlyRootFilesystem, "readOnlyRootFilesystem should be true")

	// c0055 - Linux hardening (capabilities)
	require.NotNil(t, container.SecurityContext.Capabilities, "capabilities should be set")
	require.NotNil(t, container.SecurityContext.Capabilities.Drop, "capabilities.drop should be set")
	require.Contains(t, container.SecurityContext.Capabilities.Drop, corev1.Capability("ALL"), "ALL capabilities should be dropped")

	// c0034 - Service account token mounting
	require.NotNil(t, sts.Spec.Template.Spec.AutomountServiceAccountToken, "automountServiceAccountToken should be explicitly set")
	require.False(t, *sts.Spec.Template.Spec.AutomountServiceAccountToken, "automatic service account token mounting should be disabled")

	var tokenVolume *corev1.Volume
	for i := range sts.Spec.Template.Spec.Volumes {
		if sts.Spec.Template.Spec.Volumes[i].Name == "kube-api-access" {
			tokenVolume = &sts.Spec.Template.Spec.Volumes[i]
			break
		}
	}
	require.NotNil(t, tokenVolume, "projected Kubernetes API credentials should be configured")
	require.NotNil(t, tokenVolume.Projected)
	require.Len(t, tokenVolume.Projected.Sources, 3)
	tokenProjection := tokenVolume.Projected.Sources[0].ServiceAccountToken
	require.NotNil(t, tokenProjection)
	require.Equal(t, int64(3600), *tokenProjection.ExpirationSeconds)
	require.Equal(t, "token", tokenProjection.Path)
	require.Empty(t, tokenProjection.Audience, "the API server should select its configured audience")
	require.Equal(t, "kube-root-ca.crt", sts.Spec.Template.Spec.Volumes[0].Projected.Sources[1].ConfigMap.Name)
	require.Equal(t, "metadata.namespace", sts.Spec.Template.Spec.Volumes[0].Projected.Sources[2].DownwardAPI.Items[0].FieldRef.FieldPath)

	require.Contains(t, container.VolumeMounts, corev1.VolumeMount{
		Name:      "kube-api-access",
		MountPath: "/var/run/secrets/kubernetes.io/serviceaccount",
		ReadOnly:  true,
	})

	// Verify that service account has automountServiceAccountToken set to false
	sa := &corev1.ServiceAccount{}
	require.NoError(t, client.Get(context.Background(), types.NamespacedName{
		Name:      "ingestor",
		Namespace: "default",
	}, sa))
	require.NotNil(t, sa.AutomountServiceAccountToken, "ServiceAccount automountServiceAccountToken should be explicitly set")
	require.False(t, *sa.AutomountServiceAccountToken, "ServiceAccount automountServiceAccountToken should be false")

	role := &rbacv1.Role{}
	require.NoError(t, client.Get(context.Background(), types.NamespacedName{Name: ingestorPodRoleName, Namespace: "default"}, role))
	require.Equal(t, []rbacv1.PolicyRule{{
		APIGroups: []string{""},
		Resources: []string{"pods"},
		Verbs:     []string{"get", "list", "watch", "patch"},
	}}, role.Rules)

	roleBinding := &rbacv1.RoleBinding{}
	require.NoError(t, client.Get(context.Background(), types.NamespacedName{Name: ingestorPodRoleName, Namespace: "default"}, roleBinding))
	require.Equal(t, "Role", roleBinding.RoleRef.Kind)
	require.Equal(t, ingestorPodRoleName, roleBinding.RoleRef.Name)

	clusterRole := &rbacv1.ClusterRole{}
	require.NoError(t, client.Get(context.Background(), types.NamespacedName{Name: "default:ingestor"}, clusterRole))
	for _, rule := range clusterRole.Rules {
		require.NotContains(t, rule.Resources, "namespaces")
		require.NotContains(t, rule.Resources, "pods")
	}
	require.Contains(t, clusterRole.Rules, rbacv1.PolicyRule{
		APIGroups: []string{"adx-mon.azure.com"},
		Resources: []string{"functions/status", "managementcommands/status", "summaryrules/status"},
		Verbs:     []string{"update"},
	})
}

func TestIngestorReconciler_MigratesExistingSecurityControls(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))
	require.NoError(t, clientgoscheme.AddToScheme(scheme))

	const namespace = "existing-namespace"
	ingestor := &adxmonv1.Ingestor{
		ObjectMeta: metav1.ObjectMeta{Name: "ingestor", Namespace: namespace},
		Spec: adxmonv1.IngestorSpec{
			Image:              "custom-image:v1",
			Replicas:           3,
			ADXClusterSelector: &metav1.LabelSelector{},
			CriteriaExpression: "false",
		},
		Status: adxmonv1.IngestorStatus{Conditions: []metav1.Condition{{
			Type:               adxmonv1.IngestorConditionOwner,
			Status:             metav1.ConditionTrue,
			Reason:             "Ready",
			ObservedGeneration: 1,
		}}},
	}

	automount := true
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "ingestor",
			Namespace:   namespace,
			Labels:      map[string]string{"preserve": "label"},
			Annotations: map[string]string{"preserve": "annotation"},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: to.Ptr(int32(3)),
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				AutomountServiceAccountToken: &automount,
				NodeSelector:                 map[string]string{"preserve": "selector"},
				Containers: []corev1.Container{{
					Name:  "ingestor",
					Image: "custom-image:v1",
					Args:  []string{"--preserve=argument"},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "data", MountPath: "/data"},
						{Name: "legacy-token", MountPath: "/var/run/secrets/kubernetes.io/serviceaccount"},
					},
				}},
				Volumes: []corev1.Volume{
					{Name: "data", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
					{Name: "kube-api-access", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
				},
			}},
		},
	}

	clusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: namespace + ":ingestor"},
		Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{""},
			Resources: []string{"namespaces", "pods"},
			Verbs:     []string{"get", "list", "watch", "update"},
		}},
	}
	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: namespace + ":ingestor"},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: namespace + ":ingestor"},
		Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "ingestor", Namespace: namespace}},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&adxmonv1.Ingestor{}).
		WithObjects(ingestor, sts, clusterRole, clusterRoleBinding).
		Build()
	reconciler := &IngestorReconciler{Client: client, Scheme: scheme, waitForReadyReason: "WaitForReady"}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "ingestor", Namespace: namespace}}

	result, err := reconciler.Reconcile(context.Background(), req)
	require.NoError(t, err)
	require.True(t, result.IsZero(), "criteria evaluation should still skip normal reconciliation")
	updatedIngestor := &adxmonv1.Ingestor{}
	require.NoError(t, client.Get(context.Background(), req.NamespacedName, updatedIngestor))
	require.Contains(t, updatedIngestor.Finalizers, ingestorSecurityFinalizer)

	updatedRole := &rbacv1.ClusterRole{}
	require.NoError(t, client.Get(context.Background(), types.NamespacedName{Name: namespace + ":ingestor"}, updatedRole))
	for _, rule := range updatedRole.Rules {
		require.NotContains(t, rule.Resources, "namespaces")
		require.NotContains(t, rule.Resources, "pods")
	}
	require.Contains(t, updatedRole.Rules, rbacv1.PolicyRule{
		APIGroups: []string{"adx-mon.azure.com"},
		Resources: []string{"functions"},
		Verbs:     []string{"get", "list", "update"},
	})

	localRole := &rbacv1.Role{}
	require.NoError(t, client.Get(context.Background(), types.NamespacedName{Name: ingestorPodRoleName, Namespace: namespace}, localRole))
	require.Equal(t, []string{"get", "list", "watch", "patch"}, localRole.Rules[0].Verbs)
	localBinding := &rbacv1.RoleBinding{}
	require.NoError(t, client.Get(context.Background(), types.NamespacedName{Name: ingestorPodRoleName, Namespace: namespace}, localBinding))
	require.Equal(t, "Role", localBinding.RoleRef.Kind)

	updatedSTS := &appsv1.StatefulSet{}
	require.NoError(t, client.Get(context.Background(), types.NamespacedName{Name: "ingestor", Namespace: namespace}, updatedSTS))
	require.False(t, *updatedSTS.Spec.Template.Spec.AutomountServiceAccountToken)
	require.Equal(t, int32(3), *updatedSTS.Spec.Replicas)
	require.Equal(t, map[string]string{"preserve": "label"}, updatedSTS.Labels)
	require.Equal(t, map[string]string{"preserve": "annotation"}, updatedSTS.Annotations)
	require.Equal(t, map[string]string{"preserve": "selector"}, updatedSTS.Spec.Template.Spec.NodeSelector)
	require.Equal(t, "custom-image:v1", updatedSTS.Spec.Template.Spec.Containers[0].Image)
	require.Equal(t, []string{"--preserve=argument"}, updatedSTS.Spec.Template.Spec.Containers[0].Args)
	require.Contains(t, updatedSTS.Spec.Template.Spec.Volumes, corev1.Volume{Name: "data", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}})
	require.Contains(t, updatedSTS.Spec.Template.Spec.Volumes, ingestorKubeAPIAccessVolume())
	require.Contains(t, updatedSTS.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{Name: "data", MountPath: "/data"})
	require.Contains(t, updatedSTS.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{Name: "kube-api-access", MountPath: "/var/run/secrets/kubernetes.io/serviceaccount", ReadOnly: true})
	require.NotContains(t, updatedSTS.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{Name: "legacy-token", MountPath: "/var/run/secrets/kubernetes.io/serviceaccount"})

	resourceVersion := updatedSTS.ResourceVersion
	_, err = reconciler.Reconcile(context.Background(), req)
	require.NoError(t, err)
	require.NoError(t, client.Get(context.Background(), types.NamespacedName{Name: "ingestor", Namespace: namespace}, updatedSTS))
	require.Equal(t, resourceVersion, updatedSTS.ResourceVersion, "an idempotent migration should not update the StatefulSet")
}

func TestIngestorReconciler_RecreatesRoleBindingWhenRoleRefDiffers(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))
	require.NoError(t, clientgoscheme.AddToScheme(scheme))

	const namespace = "binding-conflict"
	ingestor := &adxmonv1.Ingestor{ObjectMeta: metav1.ObjectMeta{Name: "ingestor", Namespace: namespace}}
	clusterRole := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: namespace + ":ingestor"}}
	conflicting := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: ingestorPodRoleName, Namespace: namespace},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "wrong-role"},
	}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ingestor, clusterRole, conflicting).Build()
	reconciler := &IngestorReconciler{Client: client, Scheme: scheme}

	require.NoError(t, reconciler.reconcileIngestorSecurity(context.Background(), ingestor))
	updated := &rbacv1.RoleBinding{}
	require.NoError(t, client.Get(context.Background(), types.NamespacedName{Name: ingestorPodRoleName, Namespace: namespace}, updated))
	require.Equal(t, "Role", updated.RoleRef.Kind)
	require.Equal(t, ingestorPodRoleName, updated.RoleRef.Name)
	require.Equal(t, []rbacv1.Subject{{Kind: "ServiceAccount", Name: "ingestor", Namespace: namespace}}, updated.Subjects)
}

func TestIngestorReconciler_CleansUpManagedClusterRBAC(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))
	require.NoError(t, clientgoscheme.AddToScheme(scheme))

	const namespace = "cleanup"
	name := namespace + ":ingestor"
	ingestor := &adxmonv1.Ingestor{ObjectMeta: metav1.ObjectMeta{Name: "ingestor", Namespace: namespace, Finalizers: []string{ingestorSecurityFinalizer}}}
	role := desiredIngestorClusterRole(namespace)
	binding := desiredIngestorClusterRoleBinding(namespace)
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ingestor, role, binding).Build()
	reconciler := &IngestorReconciler{Client: client, Scheme: scheme}

	require.NoError(t, reconciler.cleanupIngestorSecurity(context.Background(), ingestor))
	require.True(t, apierrors.IsNotFound(client.Get(context.Background(), types.NamespacedName{Name: name}, &rbacv1.ClusterRole{})))
	require.True(t, apierrors.IsNotFound(client.Get(context.Background(), types.NamespacedName{Name: name}, &rbacv1.ClusterRoleBinding{})))
	updated := &adxmonv1.Ingestor{}
	require.NoError(t, client.Get(context.Background(), types.NamespacedName{Name: "ingestor", Namespace: namespace}, updated))
	require.NotContains(t, updated.Finalizers, ingestorSecurityFinalizer)
}

func TestIngestorReconciler_DoesNotProvisionSecurityResourcesWhenCriteriaIsFalse(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))
	require.NoError(t, clientgoscheme.AddToScheme(scheme))

	ingestor := &adxmonv1.Ingestor{
		ObjectMeta: metav1.ObjectMeta{Name: "ingestor", Namespace: "excluded"},
		Spec: adxmonv1.IngestorSpec{
			ADXClusterSelector: &metav1.LabelSelector{},
			CriteriaExpression: "false",
		},
	}
	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&adxmonv1.Ingestor{}).
		WithObjects(ingestor).
		Build()
	reconciler := &IngestorReconciler{Client: client, Scheme: scheme}

	_, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "ingestor", Namespace: "excluded"}})
	require.NoError(t, err)

	require.True(t, apierrors.IsNotFound(client.Get(context.Background(), types.NamespacedName{Name: "excluded:ingestor"}, &rbacv1.ClusterRole{})))
	require.True(t, apierrors.IsNotFound(client.Get(context.Background(), types.NamespacedName{Name: ingestorPodRoleName, Namespace: "excluded"}, &rbacv1.Role{})))
}

func TestIngestorReconciler_RevokesClusterRoleBeforeAdditiveFailure(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))
	require.NoError(t, clientgoscheme.AddToScheme(scheme))

	const namespace = "fail-closed"
	ingestor := &adxmonv1.Ingestor{ObjectMeta: metav1.ObjectMeta{Name: "ingestor", Namespace: namespace}}
	vulnerableRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: namespace + ":ingestor"},
		Rules:      []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"namespaces", "pods"}, Verbs: []string{"update"}}},
	}
	baseClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ingestor, vulnerableRole).Build()
	reconciler := &IngestorReconciler{Client: &failRoleCreateClient{Client: baseClient}, Scheme: scheme}

	err := reconciler.reconcileIngestorSecurity(context.Background(), ingestor)
	require.ErrorContains(t, err, "role creation denied")
	updated := &rbacv1.ClusterRole{}
	require.NoError(t, baseClient.Get(context.Background(), types.NamespacedName{Name: namespace + ":ingestor"}, updated))
	for _, rule := range updated.Rules {
		require.NotContains(t, rule.Resources, "namespaces")
		require.NotContains(t, rule.Resources, "pods")
	}
}

func TestIngestorReconciler_OnDeleteStatefulSetRequiresManualRollout(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	automount := true
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "ingestor", Namespace: "on-delete"},
		Spec: appsv1.StatefulSetSpec{
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{Type: appsv1.OnDeleteStatefulSetStrategyType},
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				AutomountServiceAccountToken: &automount,
				Containers:                   []corev1.Container{{Name: "ingestor"}},
			}},
		},
	}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(sts).Build()
	reconciler := &IngestorReconciler{Client: client, Scheme: scheme}

	err := reconciler.reconcileIngestorTokenProjection(context.Background(), "on-delete")
	require.ErrorContains(t, err, "delete existing ingestor pods")
	updated := &appsv1.StatefulSet{}
	require.NoError(t, client.Get(context.Background(), types.NamespacedName{Name: "ingestor", Namespace: "on-delete"}, updated))
	require.False(t, *updated.Spec.Template.Spec.AutomountServiceAccountToken)
	require.Contains(t, updated.Spec.Template.Spec.Volumes, ingestorKubeAPIAccessVolume())

	err = reconciler.reconcileIngestorTokenProjection(context.Background(), "on-delete")
	require.ErrorContains(t, err, "delete existing ingestor pods")
}
