package operator

import (
	context "context"
	"testing"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestAlerterReconciler_IsReady(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))

	alerter := &adxmonv1.Alerter{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "adx-mon.azure.com/v1",
			Kind:       "Alerter",
		},
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
			Replicas: to.Ptr(int32(2)),
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
	dep.Spec.Replicas = to.Ptr(int32(3))
	require.NoError(t, client.Update(context.Background(), dep))

	result, err = r.IsReady(context.Background(), alerter)
	require.NoError(t, err)
	require.False(t, result.IsZero())
}

func TestAlerterReconciler_ReconcileComponent(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))

	alerter := &adxmonv1.Alerter{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "adx-mon.azure.com/v1",
			Kind:       "Alerter",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-alerter",
			Namespace: "default",
		},
		Spec: adxmonv1.AlerterSpec{
			Image:              "test-image:v1",
			ADXClusterSelector: &metav1.LabelSelector{},
		},
	}
	require.NoError(t, alerter.Spec.StoreAppliedProvisioningState())

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-alerter",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: to.Ptr(int32(2)),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "alerter",
						Image: "test-image:v1",
						Args:  []string{"--foo=bar"},
					}},
				},
			},
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

	// No update needed
	result, err := r.ReconcileComponent(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-alerter",
			Namespace: "default",
		},
	})
	require.NoError(t, err)
	require.True(t, result.IsZero())

	// Update image to trigger update path
	dep.Spec.Template.Spec.Containers[0].Image = "old-image:v1"
	require.NoError(t, client.Update(context.Background(), dep))

	result, err = r.ReconcileComponent(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-alerter",
			Namespace: "default",
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsZero())
}

func TestAlerterReconciler_CreateAlerter(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))

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
		},
		Status: adxmonv1.ADXClusterStatus{
			Conditions: []metav1.Condition{{
				Type:   adxmonv1.ADXClusterConditionOwner,
				Status: metav1.ConditionTrue,
			}},
		},
	}

	alerter := &adxmonv1.Alerter{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "adx-mon.azure.com/v1",
			Kind:       "Alerter",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-alerter",
			Namespace: "default",
		},
		Spec: adxmonv1.AlerterSpec{
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
	require.True(t, meta.FindStatusCondition(updated.Status.Conditions, adxmonv1.AlerterConditionOwner) != nil)
}

func TestAlerterReconciler_handleADXClusterSelectorChange(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))

	initialSpec := adxmonv1.AlerterSpec{
		Image: "initial-image:v1",
		ADXClusterSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"env": "prod"},
		},
	}
	alerter := &adxmonv1.Alerter{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-alerter",
			Namespace: "default",
		},
		Spec: initialSpec,
	}
	require.NoError(t, alerter.Spec.StoreAppliedProvisioningState())

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-alerter",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: to.Ptr(int32(1)),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "alerter",
						Image: "initial-image:v1",
						Args: []string{
							"--kusto-endpoint=https://oldcluster.kusto.windows.net",
							"--other-arg=value",
						},
					}},
				},
			},
		},
	}

	newCluster := &adxmonv1.ADXCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "new-cluster",
			Namespace: "default",
			Labels:    map[string]string{"env": "staging"},
		},
		Spec: adxmonv1.ADXClusterSpec{
			Endpoint: "https://newcluster.kusto.windows.net",
		},
		Status: adxmonv1.ADXClusterStatus{
			Conditions: []metav1.Condition{{
				Type:   adxmonv1.ADXClusterConditionOwner,
				Status: metav1.ConditionTrue,
			}},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(alerter, dep, newCluster).
		WithStatusSubresource(&adxmonv1.Alerter{}, &adxmonv1.ADXCluster{}).
		Build()

	r := &AlerterReconciler{Client: client, Scheme: scheme}

	storedSpec, err := alerter.Spec.LoadAppliedProvisioningState()
	require.NoError(t, err)
	require.NotNil(t, storedSpec)

	updatedAlerter := alerter.DeepCopy()
	updatedAlerter.Spec.ADXClusterSelector = &metav1.LabelSelector{
		MatchLabels: map[string]string{"env": "staging"},
	}

	changed, err := r.handleADXClusterSelectorChange(context.Background(), dep, updatedAlerter, storedSpec)
	require.NoError(t, err)
	require.True(t, changed, "Expected change to be detected")

	// Args should be updated: only --other-arg=value and new --kusto-endpoint
	expectedArgs := []string{
		"--other-arg=value",
		"--kusto-endpoint=https://newcluster.kusto.windows.net",
	}
	require.ElementsMatch(t, expectedArgs, dep.Spec.Template.Spec.Containers[0].Args, "Args mismatch after selector change")

	// --- Test Case 2: Selector Not Changed ---
	dep.Spec.Template.Spec.Containers[0].Args = expectedArgs
	require.NoError(t, updatedAlerter.Spec.StoreAppliedProvisioningState())
	require.NoError(t, client.Update(context.Background(), updatedAlerter))

	storedSpecNow, err := updatedAlerter.Spec.LoadAppliedProvisioningState()
	require.NoError(t, err)
	require.NotNil(t, storedSpecNow)

	changed, err = r.handleADXClusterSelectorChange(context.Background(), dep, updatedAlerter, storedSpecNow)
	require.NoError(t, err)
	require.False(t, changed, "Expected no change when selector is the same")
	require.ElementsMatch(t, expectedArgs, dep.Spec.Template.Spec.Containers[0].Args, "Args should not change when selector is the same")

	// --- Test Case 3: Stored Spec is Nil ---
	changed, err = r.handleADXClusterSelectorChange(context.Background(), dep, updatedAlerter, nil)
	require.NoError(t, err)
	require.False(t, changed, "Expected no change when stored spec is nil")
	require.ElementsMatch(t, expectedArgs, dep.Spec.Template.Spec.Containers[0].Args, "Args should not change when stored spec is nil")
}
