package operator

import (
	context "context"
	"testing"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestAlerterReconciler_IsReady(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))

	alerter := &adxmonv1.Alerter{
		APIVersion: "adx-mon.azure.com/v1",
		Kind:       "Alerter",
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-alerter",
			Namespace: "default",
		},
	}

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "alerter",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: new(int32(2)),
		},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas: 2,
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&adxmonv1.Alerter{}).
		Build()
	require.NoError(t, client.Create(context.Background(), alerter))
	require.NoError(t, client.Create(context.Background(), dep))
	r := &AlerterReconciler{Client: client, Scheme: scheme}

	// Ready case
	result, err := r.IsReady(context.Background(), alerter)
	require.NoError(t, err)
	require.True(t, result.IsZero())

	// Not ready case
	dep.Spec.Replicas = new(int32(3))
	require.NoError(t, client.Update(context.Background(), dep))

	result, err = r.IsReady(context.Background(), alerter)
	require.NoError(t, err)
	require.False(t, result.IsZero())
}

func TestAlerterReconciler_CreateAlerter(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, adxmonv1.AddToScheme(scheme))

	cluster := &adxmonv1.ADXCluster{
		APIVersion: "adx-mon.azure.com/v1",
		Kind:       "ADXCluster",
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
				{DatabaseName: "Metrics", TelemetryType: adxmonv1.DatabaseTelemetryMetrics},
			},
		},
		Status: adxmonv1.ADXClusterStatus{
			Conditions: []metav1.Condition{{
				Type:   adxmonv1.ADXClusterConditionOwner,
				Status: metav1.ConditionTrue,
			}},
		},
	}

	alerter := &adxmonv1.Alerter{
		APIVersion: "adx-mon.azure.com/v1",
		Kind:       "Alerter",
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-alerter",
			Namespace: "default",
		},
		Spec: adxmonv1.AlerterSpec{
			NotificationEndpoints: []string{
				"http://notifier-a:8080",
				"https://notifier-b.example.com/team,prod?sig=1",
				"http://notifier-a:8080/",
			},
			ADXClusterSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "test-cluster",
				},
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&adxmonv1.Alerter{}).
		Build()

	reconciler := &AlerterReconciler{Client: client, Scheme: scheme, waitForReadyReason: "WaitForReady"}

	require.NoError(t, client.Create(context.Background(), cluster))
	require.NoError(t, client.Create(context.Background(), alerter))

	result, err := reconciler.CreateAlerter(context.Background(), alerter)
	require.NoError(t, err)
	require.NotNil(t, result)

	updated := &adxmonv1.Alerter{}
	require.NoError(t, client.Get(context.Background(), types.NamespacedName{
		Name:      "test-alerter",
		Namespace: "default",
	}, updated))
	require.NotNil(t, meta.FindStatusCondition(updated.Status.Conditions, adxmonv1.AlerterConditionOwner))
	require.Empty(t, updated.Spec.AppliedProvisionState)

	dep := &appsv1.Deployment{}
	require.NoError(t, client.Get(context.Background(), types.NamespacedName{
		Name:      "alerter",
		Namespace: "default",
	}, dep))
	require.NotNil(t, dep.Spec.Template.Spec.TerminationGracePeriodSeconds)
	require.Equal(t, int64(60), *dep.Spec.Template.Spec.TerminationGracePeriodSeconds)
	require.Equal(t, defaultAlerterImage, dep.Spec.Template.Spec.Containers[0].Image)
	require.Equal(t, []string{
		"--alerter-address=http://notifier-a:8080",
		"--alerter-address=https://notifier-b.example.com/team,prod?sig=1",
		"--port=8080",
		"--kusto-endpoint=https://bring-your-own-adx-cluster",
	}, dep.Spec.Template.Spec.Containers[0].Args)
}

func TestAlerterReconciler_ReconcileUpdatesDeploymentDirectly(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, adxmonv1.AddToScheme(scheme))

	prodCluster := &adxmonv1.ADXCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "prod-cluster",
			Namespace: "default",
			Labels:    map[string]string{"env": "prod"},
		},
		Spec: adxmonv1.ADXClusterSpec{
			Endpoint: "https://prod.kusto.windows.net",
			Databases: []adxmonv1.ADXClusterDatabaseSpec{
				{DatabaseName: "ProdDB", TelemetryType: adxmonv1.DatabaseTelemetryMetrics},
			},
		},
		Status: adxmonv1.ADXClusterStatus{
			Conditions: []metav1.Condition{{
				Type:   adxmonv1.ADXClusterConditionOwner,
				Status: metav1.ConditionTrue,
			}},
		},
	}

	stagingCluster := &adxmonv1.ADXCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "staging-cluster",
			Namespace: "default",
			Labels:    map[string]string{"env": "staging"},
		},
		Spec: adxmonv1.ADXClusterSpec{
			Endpoint: "https://staging.kusto.windows.net",
			Databases: []adxmonv1.ADXClusterDatabaseSpec{
				{DatabaseName: "StageDB", TelemetryType: adxmonv1.DatabaseTelemetryLogs},
			},
		},
		Status: adxmonv1.ADXClusterStatus{
			Conditions: []metav1.Condition{{
				Type:   adxmonv1.ADXClusterConditionOwner,
				Status: metav1.ConditionTrue,
			}},
		},
	}

	alerter := &adxmonv1.Alerter{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "custom-alerter-cr",
			Namespace:  "default",
			Generation: 1,
		},
		Spec: adxmonv1.AlerterSpec{
			Image:                "alerter:v1",
			NotificationEndpoint: "http://legacy-notifier:8080",
			ADXClusterSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"env": "prod"},
			},
		},
	}

	cli := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(prodCluster, stagingCluster, alerter).
		WithStatusSubresource(&adxmonv1.Alerter{}, &adxmonv1.ADXCluster{}).
		Build()

	r := &AlerterReconciler{Client: cli, Scheme: scheme, waitForReadyReason: "WaitForReady"}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "custom-alerter-cr"}}

	// Initial reconcile creates Deployment/alerter with single --alerter-address
	res, err := r.Reconcile(context.Background(), req)
	require.NoError(t, err)
	require.False(t, res.IsZero())

	dep := &appsv1.Deployment{}
	require.NoError(t, cli.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "alerter"}, dep))
	require.Equal(t, "alerter:v1", dep.Spec.Template.Spec.Containers[0].Image)
	require.Equal(t, []string{
		"--alerter-address=http://legacy-notifier:8080",
		"--port=8080",
		"--kusto-endpoint=https://prod.kusto.windows.net",
	}, dep.Spec.Template.Spec.Containers[0].Args)

	// Mark Deployment ready and inject a custom flag to ensure unrelated args are preserved
	dep.Spec.Template.Spec.Containers[0].Args = []string{
		"--alerter-address=http://legacy-notifier:8080",
		"--port=8080",
		"--concurrency=20",
		"--kusto-endpoint=https://prod.kusto.windows.net",
	}
	dep.Status.ReadyReplicas = *dep.Spec.Replicas
	require.NoError(t, cli.Update(context.Background(), dep))
	dep.Status.ReadyReplicas = *dep.Spec.Replicas
	require.NoError(t, cli.Status().Update(context.Background(), dep))

	// Reconcile transitions condition to Ready with zero Deployment update and no AppliedProvisionState
	res, err = r.Reconcile(context.Background(), req)
	require.NoError(t, err)
	require.True(t, res.IsZero())

	require.NoError(t, cli.Get(context.Background(), req.NamespacedName, alerter))
	require.Empty(t, alerter.Spec.AppliedProvisionState)
	cond := meta.FindStatusCondition(alerter.Status.Conditions, adxmonv1.AlerterConditionOwner)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, "Ready", cond.Reason)

	// Simulate legacy Deployment created with terminationGracePeriodSeconds=30 before upgrade
	dep.Spec.Template.Spec.TerminationGracePeriodSeconds = new(int64(30))

	// Migrate CR from single NotificationEndpoint to NotificationEndpoints, bump image and selector
	// WITHOUT bumping Generation first (simulating direct Deployment drift / fake-client update)
	alerter.Spec.Image = "alerter:v2"
	alerter.Spec.NotificationEndpoint = ""
	alerter.Spec.NotificationEndpoints = []string{
		"http://notifier-1:8080",
		"http://notifier-2:8080",
	}
	alerter.Spec.ADXClusterSelector = &metav1.LabelSelector{
		MatchLabels: map[string]string{"env": "staging"},
	}
	require.NoError(t, cli.Update(context.Background(), dep))
	require.NoError(t, cli.Update(context.Background(), alerter))

	res, err = r.Reconcile(context.Background(), req)
	require.NoError(t, err)
	require.False(t, res.IsZero())

	require.NoError(t, cli.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "alerter"}, dep))
	require.NotNil(t, dep.Spec.Template.Spec.TerminationGracePeriodSeconds)
	require.Equal(t, int64(60), *dep.Spec.Template.Spec.TerminationGracePeriodSeconds)
	require.Equal(t, "alerter:v2", dep.Spec.Template.Spec.Containers[0].Image)
	require.Equal(t, []string{
		"--alerter-address=http://notifier-1:8080",
		"--alerter-address=http://notifier-2:8080",
		"--port=8080",
		"--concurrency=20",
		"--kusto-endpoint=https://staging.kusto.windows.net",
	}, dep.Spec.Template.Spec.Containers[0].Args)

	// Also verify Generation-bump update path updates Deployment/alerter in-place without AppliedProvisionState
	require.NoError(t, cli.Get(context.Background(), req.NamespacedName, alerter))
	require.Empty(t, alerter.Spec.AppliedProvisionState)
	alerter.Generation = 2
	alerter.Spec.NotificationEndpoints = []string{"http://notifier-3:8080"}
	require.NoError(t, cli.Update(context.Background(), alerter))

	res, err = r.Reconcile(context.Background(), req)
	require.NoError(t, err)
	require.False(t, res.IsZero())

	require.NoError(t, cli.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "alerter"}, dep))
	require.Equal(t, []string{
		"--alerter-address=http://notifier-3:8080",
		"--port=8080",
		"--concurrency=20",
		"--kusto-endpoint=https://staging.kusto.windows.net",
	}, dep.Spec.Template.Spec.Containers[0].Args)

	require.NoError(t, cli.Get(context.Background(), req.NamespacedName, alerter))
	require.Empty(t, alerter.Spec.AppliedProvisionState)
}

func TestAlerterReconciler_RejectsConflictingNotificationEndpoints(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, adxmonv1.AddToScheme(scheme))

	alerter := &adxmonv1.Alerter{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "invalid-alerter",
			Namespace: "default",
		},
		Spec: adxmonv1.AlerterSpec{
			NotificationEndpoint:  "http://notifier-a:8080",
			NotificationEndpoints: []string{"http://notifier-b:8080"},
			ADXClusterSelector:    &metav1.LabelSelector{},
		},
	}

	cli := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(alerter).
		WithStatusSubresource(&adxmonv1.Alerter{}).
		Build()

	r := &AlerterReconciler{Client: cli, Scheme: scheme}
	res, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: "default", Name: "invalid-alerter"},
	})
	require.NoError(t, err)
	require.True(t, res.IsZero())

	require.NoError(t, cli.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "invalid-alerter"}, alerter))
	cond := meta.FindStatusCondition(alerter.Status.Conditions, adxmonv1.AlerterConditionOwner)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, "InvalidSpec", cond.Reason)
}

func TestAlerterReconciler_SecurityControlsValidation(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))
	require.NoError(t, clientgoscheme.AddToScheme(scheme))

	alerter := &adxmonv1.Alerter{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-alerter",
			Namespace: "default",
		},
		Spec: adxmonv1.AlerterSpec{
			Image:                "test-image:v1",
			NotificationEndpoint: "http://notifier:8080",
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
				{DatabaseName: "test-db", TelemetryType: adxmonv1.DatabaseTelemetryMetrics},
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
		WithStatusSubresource(&adxmonv1.Alerter{}).
		Build()

	reconciler := &AlerterReconciler{Client: client, Scheme: scheme, waitForReadyReason: "WaitForReady"}

	require.NoError(t, client.Create(context.Background(), cluster))
	require.NoError(t, client.Create(context.Background(), alerter))

	result, err := reconciler.CreateAlerter(context.Background(), alerter)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify that a deployment was created
	dep := &appsv1.Deployment{}
	require.NoError(t, client.Get(context.Background(), types.NamespacedName{
		Name:      "alerter",
		Namespace: "default",
	}, dep))

	// Validate pod security context (c0055 - Linux hardening)
	require.NotNil(t, dep.Spec.Template.Spec.SecurityContext, "Pod security context should be set")
	require.NotNil(t, dep.Spec.Template.Spec.SecurityContext.RunAsNonRoot, "runAsNonRoot should be set")
	require.True(t, *dep.Spec.Template.Spec.SecurityContext.RunAsNonRoot, "runAsNonRoot should be true")
	require.NotNil(t, dep.Spec.Template.Spec.SecurityContext.RunAsUser, "runAsUser should be set")
	require.Equal(t, int64(1000), *dep.Spec.Template.Spec.SecurityContext.RunAsUser, "runAsUser should be 1000")
	require.NotNil(t, dep.Spec.Template.Spec.SecurityContext.RunAsGroup, "runAsGroup should be set")
	require.Equal(t, int64(3000), *dep.Spec.Template.Spec.SecurityContext.RunAsGroup, "runAsGroup should be 3000")
	require.NotNil(t, dep.Spec.Template.Spec.SecurityContext.FSGroup, "fsGroup should be set")
	require.Equal(t, int64(65534), *dep.Spec.Template.Spec.SecurityContext.FSGroup, "fsGroup should be 65534")

	// Validate container security context
	require.Len(t, dep.Spec.Template.Spec.Containers, 1, "Should have exactly one container")
	container := dep.Spec.Template.Spec.Containers[0]
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
	require.NotNil(t, dep.Spec.Template.Spec.AutomountServiceAccountToken, "automountServiceAccountToken should be explicitly set")
	require.True(t, *dep.Spec.Template.Spec.AutomountServiceAccountToken, "automountServiceAccountToken should be true in deployment")

	// Verify that service account has automountServiceAccountToken set to false
	sa := &corev1.ServiceAccount{}
	require.NoError(t, client.Get(context.Background(), types.NamespacedName{
		Name:      "alerter",
		Namespace: "default",
	}, sa))
	require.NotNil(t, sa.AutomountServiceAccountToken, "ServiceAccount automountServiceAccountToken should be explicitly set")
	require.False(t, *sa.AutomountServiceAccountToken, "ServiceAccount automountServiceAccountToken should be false")
}
