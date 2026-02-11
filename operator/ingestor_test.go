package operator

import (
	context "context"
	"testing"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

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
			ReadyReplicas:   2,
			UpdatedReplicas: 2,
			CurrentRevision: "rev1",
			UpdateRevision:  "rev1",
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&adxmonv1.Ingestor{}).
		WithStatusSubresource(&appsv1.StatefulSet{}).
		Build()
	require.NoError(t, client.Create(context.Background(), ingestor))
	require.NoError(t, client.Create(context.Background(), sts))
	r := &IngestorReconciler{Client: client, Scheme: scheme}

	// Ready case
	result, err := r.IsReady(context.Background(), ingestor)
	require.NoError(t, err)
	require.True(t, result.IsZero())

	// Rollout pending: ready replicas match, but updated replicas lag.
	require.NoError(t, client.Get(context.Background(), types.NamespacedName{Name: "test-ingestor", Namespace: "default"}, sts))
	sts.Status.UpdatedReplicas = 1
	require.NoError(t, client.Status().Update(context.Background(), sts))
	result, err = r.IsReady(context.Background(), ingestor)
	require.NoError(t, err)
	require.False(t, result.IsZero())

	// Rollout pending: revisions differ.
	require.NoError(t, client.Get(context.Background(), types.NamespacedName{Name: "test-ingestor", Namespace: "default"}, sts))
	sts.Status.UpdatedReplicas = 2
	sts.Status.UpdateRevision = "rev2"
	require.NoError(t, client.Status().Update(context.Background(), sts))
	result, err = r.IsReady(context.Background(), ingestor)
	require.NoError(t, err)
	require.False(t, result.IsZero())

	// Not ready case
	require.NoError(t, client.Get(context.Background(), types.NamespacedName{Name: "test-ingestor", Namespace: "default"}, sts))
	sts.Status.UpdateRevision = "rev1"
	sts.Status.CurrentRevision = "rev1"
	require.NoError(t, client.Status().Update(context.Background(), sts))
	sts.Spec.Replicas = to.Ptr(int32(3))
	require.NoError(t, client.Update(context.Background(), sts))

	result, err = r.IsReady(context.Background(), ingestor)
	require.NoError(t, err)
	require.False(t, result.IsZero())
}

func TestArgsEqual(t *testing.T) {
	tests := []struct {
		name     string
		a        []string
		b        []string
		expected bool
	}{
		{
			name:     "identical slices",
			a:        []string{"--foo=bar", "--baz=qux"},
			b:        []string{"--foo=bar", "--baz=qux"},
			expected: true,
		},
		{
			name:     "same elements different order",
			a:        []string{"--foo=bar", "--baz=qux"},
			b:        []string{"--baz=qux", "--foo=bar"},
			expected: true,
		},
		{
			name:     "different lengths",
			a:        []string{"--foo=bar"},
			b:        []string{"--foo=bar", "--baz=qux"},
			expected: false,
		},
		{
			name:     "different values",
			a:        []string{"--foo=bar", "--baz=qux"},
			b:        []string{"--foo=bar", "--baz=different"},
			expected: false,
		},
		{
			name:     "duplicate in a only",
			a:        []string{"--foo=bar", "--foo=bar"},
			b:        []string{"--foo=bar", "--baz=qux"},
			expected: false,
		},
		{
			name:     "same duplicates",
			a:        []string{"--foo=bar", "--foo=bar"},
			b:        []string{"--foo=bar", "--foo=bar"},
			expected: true,
		},
		{
			name:     "empty slices",
			a:        []string{},
			b:        []string{},
			expected: true,
		},
		{
			name:     "nil slices",
			a:        nil,
			b:        nil,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := argsEqual(tt.a, tt.b)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestStatefulSetNeedsUpdate_ArgsOrderIndependent(t *testing.T) {
	existing := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			Replicas: ptr(int32(1)),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Image: "test:v1",
							Args:  []string{"--a=1", "--b=2", "--c=3"},
						},
					},
				},
			},
		},
	}

	// Same args, different order - should NOT trigger update
	desired := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			Replicas: ptr(int32(1)),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Image: "test:v1",
							Args:  []string{"--c=3", "--a=1", "--b=2"},
						},
					},
				},
			},
		},
	}

	require.False(t, statefulSetNeedsUpdate(existing, desired),
		"Reordered args should not trigger StatefulSet update")

	// Different args - should trigger update
	desired.Spec.Template.Spec.Containers[0].Args = []string{"--a=1", "--b=2", "--d=4"}
	require.True(t, statefulSetNeedsUpdate(existing, desired),
		"Different args should trigger StatefulSet update")
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
			ADXClusterSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "adx-mon",
				},
			},
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
		Name:      "test-ingestor",
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
	require.True(t, *sts.Spec.Template.Spec.AutomountServiceAccountToken, "automountServiceAccountToken should be true in statefulset")

	// Verify that service account has automountServiceAccountToken set to false
	sa := &corev1.ServiceAccount{}
	require.NoError(t, client.Get(context.Background(), types.NamespacedName{
		Name:      "test-ingestor",
		Namespace: "default",
	}, sa))
	require.NotNil(t, sa.AutomountServiceAccountToken, "ServiceAccount automountServiceAccountToken should be explicitly set")
	require.False(t, *sa.AutomountServiceAccountToken, "ServiceAccount automountServiceAccountToken should be false")
}

// TestIngestorReconciler_UpdateExistingResources verifies that existing resources with
// proper ownership are updated when the Ingestor CRD changes (e.g., image updates).
// This test was added to catch a bug where existing StatefulSets were not updated.
func TestIngestorReconciler_UpdateExistingResources(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	ingestor := &adxmonv1.Ingestor{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "adx-mon.azure.com/v1",
			Kind:       "Ingestor",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ingestor",
			Namespace: "default",
			UID:       "test-uid-12345",
		},
		Spec: adxmonv1.IngestorSpec{
			Image:    "new-image:v2",
			Replicas: 1,
			ADXClusterSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "adx-mon"},
			},
		},
	}

	cluster := &adxmonv1.ADXCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
			Labels:    map[string]string{"app": "adx-mon"},
		},
		Spec: adxmonv1.ADXClusterSpec{
			Endpoint: "https://test.kusto.windows.net",
			Databases: []adxmonv1.ADXClusterDatabaseSpec{
				{DatabaseName: "MetricsDB", TelemetryType: adxmonv1.DatabaseTelemetryMetrics},
			},
		},
		Status: adxmonv1.ADXClusterStatus{
			Conditions: []metav1.Condition{
				{Type: adxmonv1.ADXClusterConditionOwner, Status: metav1.ConditionTrue},
			},
		},
	}

	// Create existing StatefulSet with OLD image and proper owner reference
	existingSts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ingestor",
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/name":       "test-ingestor",
				"app.kubernetes.io/component":  "ingestor",
				"app.kubernetes.io/managed-by": "adx-mon-operator",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "adx-mon.azure.com/v1",
					Kind:               "Ingestor",
					Name:               "test-ingestor",
					UID:                "test-uid-12345",
					Controller:         func() *bool { b := true; return &b }(),
					BlockOwnerDeletion: func() *bool { b := true; return &b }(),
				},
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: func() *int32 { r := int32(1); return &r }(),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":      "test-ingestor",
					"app.kubernetes.io/component": "ingestor",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/name":      "test-ingestor",
						"app.kubernetes.io/component": "ingestor",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "ingestor",
							Image: "old-image:v1", // OLD image that should be updated
						},
					},
				},
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster, ingestor, existingSts).
		WithStatusSubresource(&adxmonv1.Ingestor{}).
		Build()

	reconciler := &IngestorReconciler{Client: client, Scheme: scheme, waitForReadyReason: ReasonWaitForReady}

	// Run CreateIngestor which should update the existing StatefulSet
	result, err := reconciler.CreateIngestor(context.Background(), ingestor)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify that the StatefulSet was updated with the new image
	updatedSts := &appsv1.StatefulSet{}
	require.NoError(t, client.Get(context.Background(), types.NamespacedName{
		Name:      "test-ingestor",
		Namespace: "default",
	}, updatedSts))

	require.Len(t, updatedSts.Spec.Template.Spec.Containers, 1, "Should have exactly one container")
	require.Equal(t, "new-image:v2", updatedSts.Spec.Template.Spec.Containers[0].Image,
		"StatefulSet image should be updated to the new image from Ingestor spec")
}

func TestIngestorReconciler_UpdateExistingStatefulSetLabels(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	ingestor := &adxmonv1.Ingestor{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "adx-mon.azure.com/v1",
			Kind:       "Ingestor",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ingestor",
			Namespace: "default",
			UID:       "test-uid-12345",
		},
		Spec: adxmonv1.IngestorSpec{
			Image:    "ingestor:v1",
			Replicas: 1,
			ADXClusterSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "adx-mon"},
			},
		},
	}

	cluster := &adxmonv1.ADXCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
			Labels:    map[string]string{"app": "adx-mon"},
		},
		Spec: adxmonv1.ADXClusterSpec{
			Endpoint: "https://test.kusto.windows.net",
			Databases: []adxmonv1.ADXClusterDatabaseSpec{
				{DatabaseName: "MetricsDB", TelemetryType: adxmonv1.DatabaseTelemetryMetrics},
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
		WithObjects(cluster, ingestor).
		WithStatusSubresource(&adxmonv1.Ingestor{}).
		Build()

	reconciler := &IngestorReconciler{Client: client, Scheme: scheme, waitForReadyReason: ReasonWaitForReady}

	ready, cfg, err := reconciler.buildIngestorConfig(context.Background(), ingestor)
	require.NoError(t, err)
	require.True(t, ready)

	existingSts := BuildStatefulSet(cfg).DeepCopy()
	existingSts.Spec.Selector = &metav1.LabelSelector{
		MatchLabels: map[string]string{"app": "ingestor"},
	}
	existingSts.Spec.Template.Labels = map[string]string{"app": "ingestor"}
	existingSts.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion:         "adx-mon.azure.com/v1",
			Kind:               "Ingestor",
			Name:               "test-ingestor",
			UID:                "test-uid-12345",
			Controller:         func() *bool { b := true; return &b }(),
			BlockOwnerDeletion: func() *bool { b := true; return &b }(),
		},
	}
	require.NoError(t, client.Create(context.Background(), existingSts))

	result, err := reconciler.CreateIngestor(context.Background(), ingestor)
	require.NoError(t, err)
	require.NotNil(t, result)

	updatedSts := &appsv1.StatefulSet{}
	require.NoError(t, client.Get(context.Background(), types.NamespacedName{
		Name:      "test-ingestor",
		Namespace: "default",
	}, updatedSts))

	require.Equal(t, "test-ingestor", updatedSts.Spec.Template.Labels[LabelName])
	require.Equal(t, componentIngestor, updatedSts.Spec.Template.Labels[LabelComponent])
	require.Equal(t, "ingestor", updatedSts.Spec.Template.Labels["app"])
}

// TestIngestorReconciler_SkipsUnownedResources verifies that the reconciler does not
// update resources that are not owned by the Ingestor, preventing conflicts.
func TestIngestorReconciler_SkipsUnownedResources(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	ingestor := &adxmonv1.Ingestor{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "adx-mon.azure.com/v1",
			Kind:       "Ingestor",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ingestor",
			Namespace: "default",
			UID:       "test-uid-12345",
		},
		Spec: adxmonv1.IngestorSpec{
			Image:    "new-image:v2",
			Replicas: 1,
			ADXClusterSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "adx-mon"},
			},
		},
	}

	cluster := &adxmonv1.ADXCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
			Labels:    map[string]string{"app": "adx-mon"},
		},
		Spec: adxmonv1.ADXClusterSpec{
			Endpoint: "https://test.kusto.windows.net",
			Databases: []adxmonv1.ADXClusterDatabaseSpec{
				{DatabaseName: "MetricsDB", TelemetryType: adxmonv1.DatabaseTelemetryMetrics},
			},
		},
		Status: adxmonv1.ADXClusterStatus{
			Conditions: []metav1.Condition{
				{Type: adxmonv1.ADXClusterConditionOwner, Status: metav1.ConditionTrue},
			},
		},
	}

	// Create existing StatefulSet WITHOUT owner reference (simulating external creation)
	unownedSts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ingestor",
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/name": "test-ingestor",
			},
			// No OwnerReferences - this resource is not owned by the Ingestor
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: func() *int32 { r := int32(1); return &r }(),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name": "test-ingestor",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/name": "test-ingestor",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "ingestor",
							Image: "external-image:v1", // Should NOT be updated
						},
					},
				},
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster, ingestor, unownedSts).
		WithStatusSubresource(&adxmonv1.Ingestor{}).
		Build()

	reconciler := &IngestorReconciler{Client: client, Scheme: scheme, waitForReadyReason: ReasonWaitForReady}

	// Run CreateIngestor - should skip the unowned StatefulSet
	result, err := reconciler.CreateIngestor(context.Background(), ingestor)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify that the StatefulSet was NOT updated (still has old image)
	unchangedSts := &appsv1.StatefulSet{}
	require.NoError(t, client.Get(context.Background(), types.NamespacedName{
		Name:      "test-ingestor",
		Namespace: "default",
	}, unchangedSts))

	require.Len(t, unchangedSts.Spec.Template.Spec.Containers, 1, "Should have exactly one container")
	require.Equal(t, "external-image:v1", unchangedSts.Spec.Template.Spec.Containers[0].Image,
		"Unowned StatefulSet should NOT be updated - image should remain unchanged")
}

// TestIngestorReconciler_ImagePullSecrets verifies that imagePullSecrets configured
// on the reconciler are propagated to created StatefulSets.
func TestIngestorReconciler_ImagePullSecrets(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	ingestor := &adxmonv1.Ingestor{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "adx-mon.azure.com/v1",
			Kind:       "Ingestor",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ingestor",
			Namespace: "default",
			UID:       "test-uid-12345",
		},
		Spec: adxmonv1.IngestorSpec{
			Image:    "my-registry.io/ingestor:v1",
			Replicas: 1,
			PodPolicy: &adxmonv1.PodPolicy{
				ImagePullSecrets: []corev1.LocalObjectReference{
					{Name: "acr-pull-secret"},
					{Name: "another-registry-secret"},
				},
			},
			ADXClusterSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "adx-mon"},
			},
		},
	}

	cluster := &adxmonv1.ADXCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
			Labels:    map[string]string{"app": "adx-mon"},
		},
		Spec: adxmonv1.ADXClusterSpec{
			Endpoint: "https://test.kusto.windows.net",
			Databases: []adxmonv1.ADXClusterDatabaseSpec{
				{DatabaseName: "MetricsDB", TelemetryType: adxmonv1.DatabaseTelemetryMetrics},
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
		WithObjects(cluster, ingestor).
		WithStatusSubresource(&adxmonv1.Ingestor{}).
		Build()

	// Reconciler should apply imagePullSecrets from the CRD's podPolicy.
	reconciler := &IngestorReconciler{
		Client:             client,
		Scheme:             scheme,
		waitForReadyReason: ReasonWaitForReady,
	}

	// Run CreateIngestor
	result, err := reconciler.CreateIngestor(context.Background(), ingestor)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify that the StatefulSet was created with imagePullSecrets
	sts := &appsv1.StatefulSet{}
	require.NoError(t, client.Get(context.Background(), types.NamespacedName{
		Name:      "test-ingestor",
		Namespace: "default",
	}, sts))

	require.Len(t, sts.Spec.Template.Spec.ImagePullSecrets, 2, "Should have two imagePullSecrets")
	require.Equal(t, "acr-pull-secret", sts.Spec.Template.Spec.ImagePullSecrets[0].Name)
	require.Equal(t, "another-registry-secret", sts.Spec.Template.Spec.ImagePullSecrets[1].Name)
}

func TestIngestorReconciler_TemplateData_UsesPodPolicySchedulingConstraints(t *testing.T) {
	SetClusterLabels(map[string]string{"region": "eastus"})
	t.Cleanup(func() { SetClusterLabels(nil) })

	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	ingestor := &adxmonv1.Ingestor{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "adx-mon.azure.com/v1",
			Kind:       "Ingestor",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ingestor",
			Namespace: "default",
			UID:       "test-uid-12345",
		},
		Spec: adxmonv1.IngestorSpec{
			Image:    "my-registry.io/ingestor:v1",
			Replicas: 1,
			PodPolicy: &adxmonv1.PodPolicy{
				NodeSelector: map[string]string{
					"agentpool": "infra",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "agentpool",
						Operator: corev1.TolerationOpEqual,
						Value:    "infra",
						Effect:   corev1.TaintEffectNoSchedule,
					},
				},
			},
			ADXClusterSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "adx-mon"},
			},
		},
	}

	cluster := &adxmonv1.ADXCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
			Labels:    map[string]string{"app": "adx-mon"},
		},
		Spec: adxmonv1.ADXClusterSpec{
			Endpoint: "https://test.kusto.windows.net",
			Databases: []adxmonv1.ADXClusterDatabaseSpec{
				{DatabaseName: "MetricsDB", TelemetryType: adxmonv1.DatabaseTelemetryMetrics},
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
		WithObjects(cluster, ingestor).
		Build()

	reconciler := &IngestorReconciler{Client: client, Scheme: scheme}

	ready, data, err := reconciler.templateData(context.Background(), ingestor)
	require.NoError(t, err)
	require.True(t, ready)
	require.Equal(t, []clusterLabel{{Key: "agentpool", Value: "infra"}}, data.NodeSelector)
	require.Equal(t, []toleration{{Key: "agentpool", Operator: "Equal", Value: "infra", Effect: "NoSchedule"}}, data.ExtraTolerations)
}

// TestIngestorReconciler_NoImagePullSecrets verifies that when no imagePullSecrets
// are configured, the StatefulSet is created without the imagePullSecrets field.
func TestIngestorReconciler_NoImagePullSecrets(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	ingestor := &adxmonv1.Ingestor{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "adx-mon.azure.com/v1",
			Kind:       "Ingestor",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ingestor",
			Namespace: "default",
			UID:       "test-uid-12345",
		},
		Spec: adxmonv1.IngestorSpec{
			Image:    "ghcr.io/azure/adx-mon/ingestor:latest",
			Replicas: 1,
			ADXClusterSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "adx-mon"},
			},
		},
	}

	cluster := &adxmonv1.ADXCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
			Labels:    map[string]string{"app": "adx-mon"},
		},
		Spec: adxmonv1.ADXClusterSpec{
			Endpoint: "https://test.kusto.windows.net",
			Databases: []adxmonv1.ADXClusterDatabaseSpec{
				{DatabaseName: "MetricsDB", TelemetryType: adxmonv1.DatabaseTelemetryMetrics},
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
		WithObjects(cluster, ingestor).
		WithStatusSubresource(&adxmonv1.Ingestor{}).
		Build()

	// No podPolicy configured => no imagePullSecrets.
	reconciler := &IngestorReconciler{
		Client:             client,
		Scheme:             scheme,
		waitForReadyReason: ReasonWaitForReady,
	}

	// Run CreateIngestor
	result, err := reconciler.CreateIngestor(context.Background(), ingestor)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify that the StatefulSet was created without imagePullSecrets
	sts := &appsv1.StatefulSet{}
	require.NoError(t, client.Get(context.Background(), types.NamespacedName{
		Name:      "test-ingestor",
		Namespace: "default",
	}, sts))

	require.Empty(t, sts.Spec.Template.Spec.ImagePullSecrets, "Should have no imagePullSecrets when not configured")
}

// TestIngestorReconciler_StateMachine verifies that the reconciliation state machine
// handles all condition states correctly and doesn't create infinite loops.
// This test exists because a bug where ConditionUnknown matched too broadly caused
// rapid reconciliation loops in production.
func TestIngestorReconciler_StateMachine(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, clientgoscheme.AddToScheme(scheme))

	// Define the expected behavior for each state
	// This serves as documentation and compile-time verification of the state machine
	type stateTestCase struct {
		name          string
		reason        string
		status        metav1.ConditionStatus
		generation    int64
		observedGen   int64
		expectAction  string // "create", "ready", "noop"
		expectRequeue bool
	}

	testCases := []stateTestCase{
		// No condition - first time reconciliation
		{
			name:          "no_condition_first_reconcile",
			reason:        "",
			status:        "",
			generation:    1,
			observedGen:   0,
			expectAction:  "create",
			expectRequeue: true,
		},
		// WaitForReady - ensure manifests are up-to-date, then check if StatefulSet is ready.
		// Since test setup doesn't include all managed resources, ensureManifests will
		// create them and return with requeue.
		{
			name:          "wait_for_ready",
			reason:        ReasonWaitForReady,
			status:        metav1.ConditionTrue,
			generation:    1,
			observedGen:   1,
			expectAction:  "ensure_and_ready",
			expectRequeue: true, // requeue after ensuring manifests or if not ready yet
		},
		// NotReady - ADXCluster not ready, should retry CreateIngestor
		{
			name:          "not_ready_retry",
			reason:        ReasonNotReady,
			status:        metav1.ConditionUnknown,
			generation:    1,
			observedGen:   1,
			expectAction:  "create",
			expectRequeue: true,
		},
		// Ready - ensure manifests are up-to-date (drift correction).
		// Since test setup doesn't include all managed resources, ensureManifests will
		// create them and trigger a transition to WaitForReady.
		{
			name:          "ready_drift_correction",
			reason:        ReasonReady,
			status:        metav1.ConditionTrue,
			generation:    1,
			observedGen:   1,
			expectAction:  "ensure",
			expectRequeue: true, // requeue after detecting drift and creating resources
		},
		// Generation changed - re-render manifests
		{
			name:          "generation_changed",
			reason:        ReasonReady,
			status:        metav1.ConditionTrue,
			generation:    2,
			observedGen:   1,
			expectAction:  "create",
			expectRequeue: true,
		},
		// CriteriaExpressionError - recover by re-creating (CEL now passes or is unset)
		{
			name:          "criteria_error_recovery",
			reason:        ReasonCriteriaExpressionError,
			status:        metav1.ConditionFalse,
			generation:    1,
			observedGen:   1,
			expectAction:  "create",
			expectRequeue: true,
		},
		// CriteriaExpressionFalse - recover by re-creating (CEL now passes or is unset)
		{
			name:          "criteria_false_recovery",
			reason:        ReasonCriteriaExpressionFalse,
			status:        metav1.ConditionFalse,
			generation:    1,
			observedGen:   1,
			expectAction:  "create",
			expectRequeue: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a cluster so CreateIngestor can proceed
			cluster := &adxmonv1.ADXCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
					Labels:    map[string]string{"app": "test"},
				},
				Spec: adxmonv1.ADXClusterSpec{
					ClusterName: "test-cluster",
					Endpoint:    "https://test.kusto.windows.net",
					Databases: []adxmonv1.ADXClusterDatabaseSpec{
						{DatabaseName: "Metrics", TelemetryType: adxmonv1.DatabaseTelemetryMetrics},
					},
				},
				Status: adxmonv1.ADXClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:   adxmonv1.ADXClusterConditionOwner,
							Status: metav1.ConditionTrue,
							Reason: "Ready",
						},
					},
				},
			}

			ingestor := &adxmonv1.Ingestor{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-ingestor",
					Namespace:  "default",
					Generation: tc.generation,
				},
				Spec: adxmonv1.IngestorSpec{
					Replicas: 1,
					Image:    "test:latest",
					ADXClusterSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
				},
			}

			// Set condition if this test case has one
			if tc.reason != "" {
				ingestor.Status.Conditions = []metav1.Condition{
					{
						Type:               adxmonv1.IngestorConditionOwner,
						Status:             tc.status,
						Reason:             tc.reason,
						ObservedGeneration: tc.observedGen,
						LastTransitionTime: metav1.Now(),
					},
				}
			}

			// Create StatefulSet for "ready" action tests
			sts := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ingestor",
					Namespace: "default",
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: func() *int32 { r := int32(1); return &r }(),
				},
				Status: appsv1.StatefulSetStatus{
					ReadyReplicas: 0, // Not ready yet
				},
			}

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(cluster, ingestor, sts).
				WithStatusSubresource(&adxmonv1.Ingestor{}, &adxmonv1.ADXCluster{}).
				Build()

			reconciler := &IngestorReconciler{
				Client:             client,
				Scheme:             scheme,
				waitForReadyReason: ReasonWaitForReady,
			}

			result, err := reconciler.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-ingestor",
					Namespace: "default",
				},
			})

			// Verify no error (we're testing state transitions, not error cases)
			require.NoError(t, err, "Reconcile should not return error for state: %s", tc.name)

			// Verify requeue behavior
			if tc.expectRequeue {
				require.True(t, result.RequeueAfter > 0 || result.Requeue,
					"Expected requeue for state: %s", tc.name)
			} else {
				require.True(t, result.IsZero(),
					"Expected no requeue for state: %s, got RequeueAfter=%v", tc.name, result.RequeueAfter)
			}
		})
	}
}

// TestIngestorReconciler_NoSelfTriggeringLoop verifies that reconciliation with
// ADXCluster not ready doesn't cause a rapid loop. This was a production bug where
// status updates triggered immediate re-reconciliation.
func TestIngestorReconciler_NoSelfTriggeringLoop(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, clientgoscheme.AddToScheme(scheme))

	// ADXCluster that is NOT ready
	cluster := &adxmonv1.ADXCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
			Labels:    map[string]string{"app": "test"},
		},
		Spec: adxmonv1.ADXClusterSpec{
			ClusterName: "test-cluster",
			Endpoint:    "https://test.kusto.windows.net",
			Databases: []adxmonv1.ADXClusterDatabaseSpec{
				{DatabaseName: "Metrics", TelemetryType: adxmonv1.DatabaseTelemetryMetrics},
			},
		},
		Status: adxmonv1.ADXClusterStatus{
			Conditions: []metav1.Condition{
				{
					Type:   adxmonv1.ADXClusterConditionOwner,
					Status: metav1.ConditionFalse, // NOT ready
					Reason: "Provisioning",
				},
			},
		},
	}

	ingestor := &adxmonv1.Ingestor{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-ingestor",
			Namespace:  "default",
			Generation: 1,
		},
		Spec: adxmonv1.IngestorSpec{
			Replicas: 1,
			Image:    "test:latest",
			ADXClusterSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster, ingestor).
		WithStatusSubresource(&adxmonv1.Ingestor{}, &adxmonv1.ADXCluster{}).
		Build()

	reconciler := &IngestorReconciler{
		Client:             client,
		Scheme:             scheme,
		waitForReadyReason: ReasonWaitForReady,
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-ingestor",
			Namespace: "default",
		},
	}

	// First reconcile - should install CRDs and find cluster not ready
	result1, err := reconciler.Reconcile(context.Background(), req)
	require.NoError(t, err)
	require.True(t, result1.RequeueAfter > 0, "Should requeue after first reconcile")

	// Get the updated ingestor to see its state
	updated := &adxmonv1.Ingestor{}
	require.NoError(t, client.Get(context.Background(), req.NamespacedName, updated))

	// Second reconcile with the same state - should NOT cause rapid loop
	// It should either:
	// 1. Return quickly with a requeue (waiting for cluster)
	// 2. Not trigger CreateIngestor again unnecessarily
	result2, err := reconciler.Reconcile(context.Background(), req)
	require.NoError(t, err)

	// The key assertion: we should get a reasonable requeue interval, not immediate retry
	// If the bug existed, this would either error or return a very short interval
	if result2.RequeueAfter > 0 {
		require.True(t, result2.RequeueAfter >= defaultRequeueInterval,
			"Requeue interval should be at least %v, got %v (rapid loop detected)",
			defaultRequeueInterval, result2.RequeueAfter)
	}

	// Verify the condition reason is stable (not flipping between states)
	final := &adxmonv1.Ingestor{}
	require.NoError(t, client.Get(context.Background(), req.NamespacedName, final))

	var condition *metav1.Condition
	for i := range final.Status.Conditions {
		if final.Status.Conditions[i].Type == adxmonv1.IngestorConditionOwner {
			condition = &final.Status.Conditions[i]
			break
		}
	}
	require.NotNil(t, condition, "Should have a status condition")

	// The condition should be in a stable waiting state, not flipping
	validWaitingReasons := []string{ReasonNotReady, ReasonWaitForReady}
	found := false
	for _, r := range validWaitingReasons {
		if condition.Reason == r {
			found = true
			break
		}
	}
	require.True(t, found, "Condition reason should be a valid waiting state, got: %s", condition.Reason)
}

func TestIngestorReconciler_BuildIngestorConfig_DeterministicEndpointOrder(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, clientgoscheme.AddToScheme(scheme))

	ingestor := &adxmonv1.Ingestor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ingestor",
			Namespace: "default",
		},
		Spec: adxmonv1.IngestorSpec{
			Image:    "test:latest",
			Replicas: 1,
			ADXClusterSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "adx-mon"},
			},
		},
	}

	// Intentionally add clusters in reverse lexical order to ensure we sort deterministically.
	clusterB := &adxmonv1.ADXCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "b-cluster",
			Namespace: "default",
			Labels:    map[string]string{"app": "adx-mon"},
		},
		Spec: adxmonv1.ADXClusterSpec{
			Endpoint: "https://b.kusto.windows.net",
			Databases: []adxmonv1.ADXClusterDatabaseSpec{
				{DatabaseName: "Logs", TelemetryType: adxmonv1.DatabaseTelemetryLogs},
				{DatabaseName: "Metrics", TelemetryType: adxmonv1.DatabaseTelemetryMetrics},
			},
		},
		Status: adxmonv1.ADXClusterStatus{
			Conditions: []metav1.Condition{{Type: adxmonv1.ADXClusterConditionOwner, Status: metav1.ConditionTrue}},
		},
	}
	clusterA := &adxmonv1.ADXCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "a-cluster",
			Namespace: "default",
			Labels:    map[string]string{"app": "adx-mon"},
		},
		Spec: adxmonv1.ADXClusterSpec{
			Endpoint: "https://a.kusto.windows.net",
			Databases: []adxmonv1.ADXClusterDatabaseSpec{
				{DatabaseName: "Logs", TelemetryType: adxmonv1.DatabaseTelemetryLogs},
				{DatabaseName: "Metrics", TelemetryType: adxmonv1.DatabaseTelemetryMetrics},
			},
		},
		Status: adxmonv1.ADXClusterStatus{
			Conditions: []metav1.Condition{{Type: adxmonv1.ADXClusterConditionOwner, Status: metav1.ConditionTrue}},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(clusterB, clusterA, ingestor).
		Build()

	reconciler := &IngestorReconciler{Client: client, Scheme: scheme}

	ready, cfg, err := reconciler.buildIngestorConfig(context.Background(), ingestor)
	require.NoError(t, err)
	require.True(t, ready)
	require.NotNil(t, cfg)

	// Endpoints are returned in ADXCluster name order (sorted for iteration stability),
	// but the set comparison in statefulSetNeedsUpdate handles any remaining non-determinism.
	require.ElementsMatch(t, []string{"Metrics=https://a.kusto.windows.net", "Metrics=https://b.kusto.windows.net"}, cfg.MetricsClusters)
	require.ElementsMatch(t, []string{"Logs=https://a.kusto.windows.net", "Logs=https://b.kusto.windows.net"}, cfg.LogsClusters)
}

// TestIngestorReconciler_CriteriaExpression tests CEL expression evaluation and recovery paths.
// This covers the CriteriaExpressionError and CriteriaExpressionFalse conditions.
func TestIngestorReconciler_CriteriaExpression(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, clientgoscheme.AddToScheme(scheme))

	// Set up cluster labels for CEL evaluation
	SetClusterLabels(map[string]string{"region": "eastus", "env": "prod"})
	t.Cleanup(func() { SetClusterLabels(nil) })

	cluster := &adxmonv1.ADXCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
			Labels:    map[string]string{"app": "test"},
		},
		Spec: adxmonv1.ADXClusterSpec{
			ClusterName: "test-cluster",
			Endpoint:    "https://test.kusto.windows.net",
			Databases: []adxmonv1.ADXClusterDatabaseSpec{
				{DatabaseName: "Metrics", TelemetryType: adxmonv1.DatabaseTelemetryMetrics},
			},
		},
		Status: adxmonv1.ADXClusterStatus{
			Conditions: []metav1.Condition{
				{Type: adxmonv1.ADXClusterConditionOwner, Status: metav1.ConditionTrue},
			},
		},
	}

	t.Run("expression_evaluates_to_false", func(t *testing.T) {
		ingestor := &adxmonv1.Ingestor{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-ingestor",
				Namespace:  "default",
				Generation: 1,
			},
			Spec: adxmonv1.IngestorSpec{
				Replicas:           1,
				Image:              "test:latest",
				CriteriaExpression: `region == "westus"`, // Will evaluate to false
				ADXClusterSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "test"},
				},
			},
		}

		client := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cluster, ingestor).
			WithStatusSubresource(&adxmonv1.Ingestor{}).
			Build()

		reconciler := &IngestorReconciler{
			Client:             client,
			Scheme:             scheme,
			waitForReadyReason: ReasonWaitForReady,
		}

		result, err := reconciler.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "test-ingestor", Namespace: "default"},
		})

		require.NoError(t, err)
		require.True(t, result.IsZero(), "Should not requeue when expression is false")

		// Verify condition was set to CriteriaExpressionFalse
		updated := &adxmonv1.Ingestor{}
		require.NoError(t, client.Get(context.Background(), types.NamespacedName{Name: "test-ingestor", Namespace: "default"}, updated))

		condition := findCondition(updated.Status.Conditions, adxmonv1.IngestorConditionOwner)
		require.NotNil(t, condition)
		require.Equal(t, ReasonCriteriaExpressionFalse, condition.Reason)
		require.Equal(t, metav1.ConditionFalse, condition.Status)
	})

	t.Run("expression_has_syntax_error", func(t *testing.T) {
		ingestor := &adxmonv1.Ingestor{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-ingestor-err",
				Namespace:  "default",
				Generation: 1,
			},
			Spec: adxmonv1.IngestorSpec{
				Replicas:           1,
				Image:              "test:latest",
				CriteriaExpression: `region === "eastus"`, // Invalid CEL syntax (=== instead of ==)
				ADXClusterSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "test"},
				},
			},
		}

		client := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cluster, ingestor).
			WithStatusSubresource(&adxmonv1.Ingestor{}).
			Build()

		reconciler := &IngestorReconciler{
			Client:             client,
			Scheme:             scheme,
			waitForReadyReason: ReasonWaitForReady,
		}

		result, err := reconciler.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "test-ingestor-err", Namespace: "default"},
		})

		require.NoError(t, err, "Terminal errors should return nil, not error")
		require.True(t, result.IsZero(), "Should not requeue on terminal error")

		// Verify condition was set to CriteriaExpressionError
		updated := &adxmonv1.Ingestor{}
		require.NoError(t, client.Get(context.Background(), types.NamespacedName{Name: "test-ingestor-err", Namespace: "default"}, updated))

		condition := findCondition(updated.Status.Conditions, adxmonv1.IngestorConditionOwner)
		require.NotNil(t, condition)
		require.Equal(t, ReasonCriteriaExpressionError, condition.Reason)
		require.Equal(t, metav1.ConditionFalse, condition.Status)
	})

	t.Run("expression_evaluates_to_true", func(t *testing.T) {
		ingestor := &adxmonv1.Ingestor{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-ingestor-true",
				Namespace:  "default",
				Generation: 1,
			},
			Spec: adxmonv1.IngestorSpec{
				Replicas:           1,
				Image:              "test:latest",
				CriteriaExpression: `region == "eastus"`, // Will evaluate to true
				ADXClusterSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "test"},
				},
			},
		}

		client := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cluster, ingestor).
			WithStatusSubresource(&adxmonv1.Ingestor{}).
			Build()

		reconciler := &IngestorReconciler{
			Client:             client,
			Scheme:             scheme,
			waitForReadyReason: ReasonWaitForReady,
		}

		result, err := reconciler.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "test-ingestor-true", Namespace: "default"},
		})

		require.NoError(t, err)
		require.True(t, result.RequeueAfter > 0, "Should requeue after CreateIngestor")

		// Verify condition is WaitForReady (CreateIngestor was called)
		updated := &adxmonv1.Ingestor{}
		require.NoError(t, client.Get(context.Background(), types.NamespacedName{Name: "test-ingestor-true", Namespace: "default"}, updated))

		condition := findCondition(updated.Status.Conditions, adxmonv1.IngestorConditionOwner)
		require.NotNil(t, condition)
		require.Equal(t, ReasonWaitForReady, condition.Reason)
	})

	t.Run("recovery_from_expression_false", func(t *testing.T) {
		// Start with an ingestor that previously had CriteriaExpressionFalse
		// but now has an expression that evaluates to true
		ingestor := &adxmonv1.Ingestor{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-ingestor-recover",
				Namespace:  "default",
				Generation: 2, // Updated generation
			},
			Spec: adxmonv1.IngestorSpec{
				Replicas:           1,
				Image:              "test:latest",
				CriteriaExpression: `region == "eastus"`, // Now evaluates to true
				ADXClusterSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "test"},
				},
			},
			Status: adxmonv1.IngestorStatus{
				Conditions: []metav1.Condition{
					{
						Type:               adxmonv1.IngestorConditionOwner,
						Status:             metav1.ConditionFalse,
						Reason:             ReasonCriteriaExpressionFalse,
						ObservedGeneration: 1, // Old generation
						LastTransitionTime: metav1.Now(),
					},
				},
			},
		}

		client := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cluster, ingestor).
			WithStatusSubresource(&adxmonv1.Ingestor{}).
			Build()

		reconciler := &IngestorReconciler{
			Client:             client,
			Scheme:             scheme,
			waitForReadyReason: ReasonWaitForReady,
		}

		result, err := reconciler.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "test-ingestor-recover", Namespace: "default"},
		})

		require.NoError(t, err)
		require.True(t, result.RequeueAfter > 0, "Should requeue after recovery")

		// Verify condition transitioned to WaitForReady
		updated := &adxmonv1.Ingestor{}
		require.NoError(t, client.Get(context.Background(), types.NamespacedName{Name: "test-ingestor-recover", Namespace: "default"}, updated))

		condition := findCondition(updated.Status.Conditions, adxmonv1.IngestorConditionOwner)
		require.NotNil(t, condition)
		require.Equal(t, ReasonWaitForReady, condition.Reason)
	})
}

// TestIngestorReconciler_ADXClusterEdgeCases tests various ADXCluster configurations.
func TestIngestorReconciler_ADXClusterEdgeCases(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, clientgoscheme.AddToScheme(scheme))

	t.Run("mixed_ready_not_ready_clusters", func(t *testing.T) {
		ingestor := &adxmonv1.Ingestor{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ingestor",
				Namespace: "default",
			},
			Spec: adxmonv1.IngestorSpec{
				Replicas: 1,
				Image:    "test:latest",
				ADXClusterSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "adx-mon"},
				},
			},
		}

		readyCluster := &adxmonv1.ADXCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ready-cluster",
				Namespace: "default",
				Labels:    map[string]string{"app": "adx-mon"},
			},
			Spec: adxmonv1.ADXClusterSpec{
				Endpoint: "https://ready.kusto.windows.net",
				Databases: []adxmonv1.ADXClusterDatabaseSpec{
					{DatabaseName: "Metrics", TelemetryType: adxmonv1.DatabaseTelemetryMetrics},
				},
			},
			Status: adxmonv1.ADXClusterStatus{
				Conditions: []metav1.Condition{
					{Type: adxmonv1.ADXClusterConditionOwner, Status: metav1.ConditionTrue},
				},
			},
		}

		notReadyCluster := &adxmonv1.ADXCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "not-ready-cluster",
				Namespace: "default",
				Labels:    map[string]string{"app": "adx-mon"},
			},
			Spec: adxmonv1.ADXClusterSpec{
				Endpoint: "https://notready.kusto.windows.net",
				Databases: []adxmonv1.ADXClusterDatabaseSpec{
					{DatabaseName: "Metrics", TelemetryType: adxmonv1.DatabaseTelemetryMetrics},
				},
			},
			Status: adxmonv1.ADXClusterStatus{
				Conditions: []metav1.Condition{
					{Type: adxmonv1.ADXClusterConditionOwner, Status: metav1.ConditionFalse},
				},
			},
		}

		client := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(ingestor, readyCluster, notReadyCluster).
			Build()

		reconciler := &IngestorReconciler{Client: client, Scheme: scheme}

		// buildIngestorConfig should return not ready when any cluster is not ready
		ready, cfg, err := reconciler.buildIngestorConfig(context.Background(), ingestor)
		require.NoError(t, err)
		require.False(t, ready, "Should return not ready when any cluster is not ready")
		require.Nil(t, cfg)
	})

	t.Run("federated_cluster_selection_lexicographic", func(t *testing.T) {
		ingestor := &adxmonv1.Ingestor{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ingestor",
				Namespace: "default",
			},
			Spec: adxmonv1.IngestorSpec{
				Replicas: 1,
				Image:    "test:latest",
				ADXClusterSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "adx-mon"},
				},
			},
		}

		federated := adxmonv1.ClusterRoleFederated

		// Two federated clusters - should select lexicographically smallest endpoint
		clusterZ := &adxmonv1.ADXCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "z-cluster",
				Namespace: "default",
				Labels:    map[string]string{"app": "adx-mon"},
			},
			Spec: adxmonv1.ADXClusterSpec{
				Endpoint: "https://z-federated.kusto.windows.net",
				Role:     &federated,
				Databases: []adxmonv1.ADXClusterDatabaseSpec{
					{DatabaseName: "Metrics", TelemetryType: adxmonv1.DatabaseTelemetryMetrics},
				},
			},
			Status: adxmonv1.ADXClusterStatus{
				Conditions: []metav1.Condition{
					{Type: adxmonv1.ADXClusterConditionOwner, Status: metav1.ConditionTrue},
				},
			},
		}

		clusterA := &adxmonv1.ADXCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "a-cluster",
				Namespace: "default",
				Labels:    map[string]string{"app": "adx-mon"},
			},
			Spec: adxmonv1.ADXClusterSpec{
				Endpoint: "https://a-federated.kusto.windows.net",
				Role:     &federated,
				Databases: []adxmonv1.ADXClusterDatabaseSpec{
					{DatabaseName: "Metrics", TelemetryType: adxmonv1.DatabaseTelemetryMetrics},
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
			WithObjects(ingestor, clusterZ, clusterA).
			Build()

		reconciler := &IngestorReconciler{Client: client, Scheme: scheme}

		ready, cfg, err := reconciler.buildIngestorConfig(context.Background(), ingestor)
		require.NoError(t, err)
		require.True(t, ready)
		require.NotNil(t, cfg)

		// AzureResource should be the lexicographically smallest endpoint
		require.Equal(t, "https://a-federated.kusto.windows.net", cfg.AzureResource)
	})

	t.Run("nil_adxcluster_selector", func(t *testing.T) {
		ingestor := &adxmonv1.Ingestor{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ingestor",
				Namespace: "default",
			},
			Spec: adxmonv1.IngestorSpec{
				Replicas:           1,
				Image:              "test:latest",
				ADXClusterSelector: nil, // Nil selector
			},
		}

		client := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(ingestor).
			Build()

		reconciler := &IngestorReconciler{Client: client, Scheme: scheme}

		ready, cfg, err := reconciler.buildIngestorConfig(context.Background(), ingestor)
		require.Error(t, err)
		require.Contains(t, err.Error(), "ADXClusterSelector is required")
		require.False(t, ready)
		require.Nil(t, cfg)
	})
}

// TestIngestorReconciler_EnsureManifests tests the drift correction functionality.
func TestIngestorReconciler_EnsureManifests(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	t.Run("no_error_when_resources_exist", func(t *testing.T) {
		ingestor := &adxmonv1.Ingestor{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "adx-mon.azure.com/v1",
				Kind:       "Ingestor",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ingestor",
				Namespace: "default",
				UID:       "test-uid-12345",
			},
			Spec: adxmonv1.IngestorSpec{
				Image:    "test:v1",
				Replicas: 1,
				ADXClusterSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "adx-mon"},
				},
			},
		}

		cluster := &adxmonv1.ADXCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
				Labels:    map[string]string{"app": "adx-mon"},
			},
			Spec: adxmonv1.ADXClusterSpec{
				Endpoint: "https://test.kusto.windows.net",
				Databases: []adxmonv1.ADXClusterDatabaseSpec{
					{DatabaseName: "Metrics", TelemetryType: adxmonv1.DatabaseTelemetryMetrics},
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
			WithObjects(cluster, ingestor).
			WithStatusSubresource(&adxmonv1.Ingestor{}).
			Build()

		reconciler := &IngestorReconciler{Client: client, Scheme: scheme, waitForReadyReason: ReasonWaitForReady}

		// First create the resources
		_, err := reconciler.CreateIngestor(context.Background(), ingestor)
		require.NoError(t, err)

		// Call ensureManifests multiple times - should not error
		for i := 0; i < 3; i++ {
			result, _, err := reconciler.ensureManifests(context.Background(), ingestor)
			require.NoError(t, err, "ensureManifests should not error on iteration %d", i)
			// Result may or may not have requeue depending on drift detection, which is fine
			_ = result
		}
	})

	t.Run("adxcluster_becomes_not_ready", func(t *testing.T) {
		ingestor := &adxmonv1.Ingestor{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "adx-mon.azure.com/v1",
				Kind:       "Ingestor",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ingestor",
				Namespace: "default",
				UID:       "test-uid-12345",
			},
			Spec: adxmonv1.IngestorSpec{
				Image:    "test:v1",
				Replicas: 1,
				ADXClusterSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "adx-mon"},
				},
			},
		}

		// Cluster is NOT ready
		cluster := &adxmonv1.ADXCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
				Labels:    map[string]string{"app": "adx-mon"},
			},
			Spec: adxmonv1.ADXClusterSpec{
				Endpoint: "https://test.kusto.windows.net",
				Databases: []adxmonv1.ADXClusterDatabaseSpec{
					{DatabaseName: "Metrics", TelemetryType: adxmonv1.DatabaseTelemetryMetrics},
				},
			},
			Status: adxmonv1.ADXClusterStatus{
				Conditions: []metav1.Condition{
					{Type: adxmonv1.ADXClusterConditionOwner, Status: metav1.ConditionFalse},
				},
			},
		}

		client := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cluster, ingestor).
			WithStatusSubresource(&adxmonv1.Ingestor{}).
			Build()

		reconciler := &IngestorReconciler{Client: client, Scheme: scheme, waitForReadyReason: ReasonWaitForReady}

		// ensureManifests should return requeue when cluster is not ready
		result, updated, err := reconciler.ensureManifests(context.Background(), ingestor)
		require.NoError(t, err)
		require.False(t, updated, "Should not report updated when cluster not ready")
		require.True(t, result.RequeueAfter > 0, "Should requeue when cluster not ready")
	})
}

// TestIngestorReconciler_ApplyDefaults tests the defaulting logic.
func TestIngestorReconciler_ApplyDefaults(t *testing.T) {
	reconciler := &IngestorReconciler{}

	t.Run("applies_all_defaults", func(t *testing.T) {
		ingestor := &adxmonv1.Ingestor{
			Spec: adxmonv1.IngestorSpec{
				// All fields empty/zero
			},
		}

		reconciler.applyDefaults(ingestor)

		require.Equal(t, int32(1), ingestor.Spec.Replicas, "Default replicas should be 1")
		require.Equal(t, "ghcr.io/azure/adx-mon/ingestor:latest", ingestor.Spec.Image, "Default image should be set")
		require.Equal(t, "Logs:Ingestor", ingestor.Spec.LogDestination, "Default log destination should be set")
	})

	t.Run("preserves_existing_values", func(t *testing.T) {
		ingestor := &adxmonv1.Ingestor{
			Spec: adxmonv1.IngestorSpec{
				Replicas:       3,
				Image:          "custom:v1",
				LogDestination: "CustomLogs:MyIngestor",
			},
		}

		reconciler.applyDefaults(ingestor)

		require.Equal(t, int32(3), ingestor.Spec.Replicas, "Custom replicas should be preserved")
		require.Equal(t, "custom:v1", ingestor.Spec.Image, "Custom image should be preserved")
		require.Equal(t, "CustomLogs:MyIngestor", ingestor.Spec.LogDestination, "Custom log destination should be preserved")
	})
}

// TestIngestorReconciler_ConcurrentCRDInstallation tests the mutex protection for CRD installation.
func TestIngestorReconciler_ConcurrentCRDInstallation(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, clientgoscheme.AddToScheme(scheme))

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	reconciler := &IngestorReconciler{Client: client, Scheme: scheme}

	// Run multiple concurrent calls to ensureCRDsInstalled
	const numGoroutines = 10
	done := make(chan bool, numGoroutines)
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			err := reconciler.ensureCRDsInstalled(context.Background())
			if err != nil {
				errors <- err
			}
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}
	close(errors)

	// Check for any errors
	for err := range errors {
		t.Errorf("Unexpected error during concurrent CRD installation: %v", err)
	}

	// Verify that crdsInstalled flag is set
	require.True(t, reconciler.crdsInstalled, "CRDs should be marked as installed")
}

// TestIngestorReconciler_MapADXClusterToIngestors tests the ADXCluster->Ingestor mapping function.
func TestIngestorReconciler_MapADXClusterToIngestors(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))

	t.Run("matches_selector", func(t *testing.T) {
		cluster := &adxmonv1.ADXCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
				Labels:    map[string]string{"env": "prod", "region": "eastus"},
			},
		}

		ingestor := &adxmonv1.Ingestor{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "matching-ingestor",
				Namespace: "default",
			},
			Spec: adxmonv1.IngestorSpec{
				ADXClusterSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"env": "prod"},
				},
			},
		}

		client := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cluster, ingestor).
			Build()

		reconciler := &IngestorReconciler{Client: client, Scheme: scheme}

		requests := reconciler.mapADXClusterToIngestors(context.Background(), cluster)
		require.Len(t, requests, 1)
		require.Equal(t, "matching-ingestor", requests[0].Name)
		require.Equal(t, "default", requests[0].Namespace)
	})

	t.Run("does_not_match_different_labels", func(t *testing.T) {
		cluster := &adxmonv1.ADXCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
				Labels:    map[string]string{"env": "dev"},
			},
		}

		ingestor := &adxmonv1.Ingestor{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "non-matching-ingestor",
				Namespace: "default",
			},
			Spec: adxmonv1.IngestorSpec{
				ADXClusterSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"env": "prod"},
				},
			},
		}

		client := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cluster, ingestor).
			Build()

		reconciler := &IngestorReconciler{Client: client, Scheme: scheme}

		requests := reconciler.mapADXClusterToIngestors(context.Background(), cluster)
		require.Empty(t, requests)
	})

	t.Run("skips_deleted_ingestors", func(t *testing.T) {
		now := metav1.Now()
		cluster := &adxmonv1.ADXCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
				Labels:    map[string]string{"env": "prod"},
			},
		}

		deletingIngestor := &adxmonv1.Ingestor{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "deleting-ingestor",
				Namespace:         "default",
				DeletionTimestamp: &now,
				Finalizers:        []string{"test-finalizer"}, // Required for deletion timestamp to be set
			},
			Spec: adxmonv1.IngestorSpec{
				ADXClusterSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"env": "prod"},
				},
			},
		}

		client := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cluster, deletingIngestor).
			Build()

		reconciler := &IngestorReconciler{Client: client, Scheme: scheme}

		requests := reconciler.mapADXClusterToIngestors(context.Background(), cluster)
		require.Empty(t, requests, "Should skip Ingestors with deletion timestamp")
	})

	t.Run("skips_nil_selector", func(t *testing.T) {
		cluster := &adxmonv1.ADXCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
				Labels:    map[string]string{"env": "prod"},
			},
		}

		ingestorWithNilSelector := &adxmonv1.Ingestor{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "nil-selector-ingestor",
				Namespace: "default",
			},
			Spec: adxmonv1.IngestorSpec{
				ADXClusterSelector: nil, // Nil selector
			},
		}

		client := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cluster, ingestorWithNilSelector).
			Build()

		reconciler := &IngestorReconciler{Client: client, Scheme: scheme}

		requests := reconciler.mapADXClusterToIngestors(context.Background(), cluster)
		require.Empty(t, requests, "Should skip Ingestors with nil selector")
	})

	t.Run("namespace_scoped", func(t *testing.T) {
		cluster := &adxmonv1.ADXCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "namespace-a",
				Labels:    map[string]string{"env": "prod"},
			},
		}

		ingestorSameNS := &adxmonv1.Ingestor{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "same-ns-ingestor",
				Namespace: "namespace-a",
			},
			Spec: adxmonv1.IngestorSpec{
				ADXClusterSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"env": "prod"},
				},
			},
		}

		ingestorDifferentNS := &adxmonv1.Ingestor{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "different-ns-ingestor",
				Namespace: "namespace-b",
			},
			Spec: adxmonv1.IngestorSpec{
				ADXClusterSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"env": "prod"},
				},
			},
		}

		client := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cluster, ingestorSameNS, ingestorDifferentNS).
			Build()

		reconciler := &IngestorReconciler{Client: client, Scheme: scheme}

		requests := reconciler.mapADXClusterToIngestors(context.Background(), cluster)
		require.Len(t, requests, 1, "Should only match Ingestors in same namespace")
		require.Equal(t, "same-ns-ingestor", requests[0].Name)
		require.Equal(t, "namespace-a", requests[0].Namespace)
	})

	t.Run("multiple_matching_ingestors", func(t *testing.T) {
		cluster := &adxmonv1.ADXCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
				Labels:    map[string]string{"env": "prod", "tier": "premium"},
			},
		}

		ingestor1 := &adxmonv1.Ingestor{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ingestor-1",
				Namespace: "default",
			},
			Spec: adxmonv1.IngestorSpec{
				ADXClusterSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"env": "prod"},
				},
			},
		}

		ingestor2 := &adxmonv1.Ingestor{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ingestor-2",
				Namespace: "default",
			},
			Spec: adxmonv1.IngestorSpec{
				ADXClusterSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"tier": "premium"},
				},
			},
		}

		client := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cluster, ingestor1, ingestor2).
			Build()

		reconciler := &IngestorReconciler{Client: client, Scheme: scheme}

		requests := reconciler.mapADXClusterToIngestors(context.Background(), cluster)
		require.Len(t, requests, 2, "Should match both Ingestors")

		names := []string{requests[0].Name, requests[1].Name}
		require.Contains(t, names, "ingestor-1")
		require.Contains(t, names, "ingestor-2")
	})
}

// TestIngestorReconciler_TerminalVsTransientErrors documents and verifies the error handling pattern.
// Terminal errors (invalid config) return nil to prevent retry loops.
// Transient errors (API failures) return error to trigger requeue.
func TestIngestorReconciler_TerminalVsTransientErrors(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, clientgoscheme.AddToScheme(scheme))

	// Set up cluster labels for CEL evaluation
	SetClusterLabels(map[string]string{"region": "eastus"})
	t.Cleanup(func() { SetClusterLabels(nil) })

	t.Run("cel_error_is_terminal", func(t *testing.T) {
		ingestor := &adxmonv1.Ingestor{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-ingestor",
				Namespace:  "default",
				Generation: 1,
			},
			Spec: adxmonv1.IngestorSpec{
				Replicas:           1,
				Image:              "test:latest",
				CriteriaExpression: `invalid.syntax.here`, // Invalid CEL
				ADXClusterSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "test"},
				},
			},
		}

		client := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(ingestor).
			WithStatusSubresource(&adxmonv1.Ingestor{}).
			Build()

		reconciler := &IngestorReconciler{
			Client:             client,
			Scheme:             scheme,
			waitForReadyReason: ReasonWaitForReady,
		}

		result, err := reconciler.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "test-ingestor", Namespace: "default"},
		})

		// Terminal error pattern: return nil (not error) with no requeue
		require.NoError(t, err, "Terminal errors should return nil, not error")
		require.True(t, result.IsZero(), "Terminal errors should not requeue")

		// Verify condition is set with ConditionFalse
		updated := &adxmonv1.Ingestor{}
		require.NoError(t, client.Get(context.Background(), types.NamespacedName{Name: "test-ingestor", Namespace: "default"}, updated))

		condition := findCondition(updated.Status.Conditions, adxmonv1.IngestorConditionOwner)
		require.NotNil(t, condition)
		require.Equal(t, metav1.ConditionFalse, condition.Status, "Terminal errors should set ConditionFalse")
	})

	t.Run("cel_false_is_terminal", func(t *testing.T) {
		ingestor := &adxmonv1.Ingestor{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-ingestor-false",
				Namespace:  "default",
				Generation: 1,
			},
			Spec: adxmonv1.IngestorSpec{
				Replicas:           1,
				Image:              "test:latest",
				CriteriaExpression: `region == "westus"`, // Evaluates to false
				ADXClusterSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "test"},
				},
			},
		}

		client := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(ingestor).
			WithStatusSubresource(&adxmonv1.Ingestor{}).
			Build()

		reconciler := &IngestorReconciler{
			Client:             client,
			Scheme:             scheme,
			waitForReadyReason: ReasonWaitForReady,
		}

		result, err := reconciler.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "test-ingestor-false", Namespace: "default"},
		})

		// Terminal condition: expression is valid but false
		require.NoError(t, err, "Expression false should return nil")
		require.True(t, result.IsZero(), "Expression false should not requeue")
	})
}

// findCondition is a helper to find a condition by type
func findCondition(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}
