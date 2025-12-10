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
		// WaitForReady - check if StatefulSet is ready
		{
			name:          "wait_for_ready",
			reason:        ReasonWaitForReady,
			status:        metav1.ConditionTrue,
			generation:    1,
			observedGen:   1,
			expectAction:  "ready",
			expectRequeue: true, // requeue if not ready yet
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
		// CRDsInstalled - should continue to template rendering
		{
			name:          "crds_installed_continue",
			reason:        ReasonCRDsInstalled,
			status:        metav1.ConditionUnknown,
			generation:    1,
			observedGen:   1,
			expectAction:  "create",
			expectRequeue: true,
		},
		// Ready - no action needed
		{
			name:          "ready_noop",
			reason:        ReasonReady,
			status:        metav1.ConditionTrue,
			generation:    1,
			observedGen:   1,
			expectAction:  "noop",
			expectRequeue: false,
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
		// CriteriaExpressionError - terminal, no action
		{
			name:          "criteria_error_noop",
			reason:        ReasonCriteriaExpressionError,
			status:        metav1.ConditionFalse,
			generation:    1,
			observedGen:   1,
			expectAction:  "noop",
			expectRequeue: false,
		},
		// CriteriaExpressionFalse - terminal, no action
		{
			name:          "criteria_false_noop",
			reason:        ReasonCriteriaExpressionFalse,
			status:        metav1.ConditionFalse,
			generation:    1,
			observedGen:   1,
			expectAction:  "noop",
			expectRequeue: false,
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
	validWaitingReasons := []string{ReasonNotReady, ReasonCRDsInstalled, ReasonWaitForReady}
	found := false
	for _, r := range validWaitingReasons {
		if condition.Reason == r {
			found = true
			break
		}
	}
	require.True(t, found, "Condition reason should be a valid waiting state, got: %s", condition.Reason)
}
