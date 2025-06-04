package operator

import (
	context "context"
	"testing"
	"time"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCollectorReconciler_IsReady(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))

	collector := &adxmonv1.Collector{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "adx-mon.azure.com/v1",
			Kind:       "Collector",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-collector",
			Namespace: "default",
		},
	}

	// Test case 1: DaemonSet not found
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(collector).WithStatusSubresource(&adxmonv1.Collector{}).Build()
	r := &CollectorReconciler{
		Client: client,
		Scheme: scheme,
	}

	result, err := r.IsReady(context.Background(), collector)
	require.NoError(t, err)
	require.Equal(t, ctrl.Result{RequeueAfter: time.Minute}, result)

	// Test case 2: DaemonSet exists but not ready
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "collector",
			Namespace: "default",
		},
		Status: appsv1.DaemonSetStatus{
			DesiredNumberScheduled: 3,
			NumberReady:           1,
		},
	}
	client = fake.NewClientBuilder().WithScheme(scheme).WithObjects(collector, ds).WithStatusSubresource(&adxmonv1.Collector{}).Build()
	r.Client = client

	result, err = r.IsReady(context.Background(), collector)
	require.NoError(t, err)
	require.Equal(t, ctrl.Result{RequeueAfter: time.Minute}, result)

	// Test case 3: DaemonSet ready
	ds.Status.NumberReady = 3
	client = fake.NewClientBuilder().WithScheme(scheme).WithObjects(collector, ds).WithStatusSubresource(&adxmonv1.Collector{}).Build()
	r.Client = client

	result, err = r.IsReady(context.Background(), collector)
	require.NoError(t, err)
	require.Equal(t, ctrl.Result{}, result)

	// Verify condition is set
	require.NoError(t, r.Get(context.Background(), types.NamespacedName{Name: "test-collector", Namespace: "default"}, collector))
	require.Len(t, collector.Status.Conditions, 1)
	require.Equal(t, adxmonv1.CollectorConditionOwner, collector.Status.Conditions[0].Type)
	require.Equal(t, metav1.ConditionTrue, collector.Status.Conditions[0].Status)
	require.Equal(t, "Ready", collector.Status.Conditions[0].Reason)
}

func TestCollectorReconciler_ReconcileComponent(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))

	collector := &adxmonv1.Collector{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "adx-mon.azure.com/v1",
			Kind:       "Collector",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-collector",
			Namespace: "default",
		},
		Spec: adxmonv1.CollectorSpec{
			Image: "test-image:v1",
		},
	}

	// Create a DaemonSet with old image
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-collector",
			Namespace: "default",
		},
		Spec: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "collector",
							Image: "old-image:v1",
						},
					},
				},
			},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(collector, ds).Build()
	r := &CollectorReconciler{
		Client: client,
		Scheme: scheme,
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-collector",
			Namespace: "default",
		},
	}

	result, err := r.ReconcileComponent(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, ctrl.Result{}, result)

	// Verify the DaemonSet was updated with new image
	var updatedDS appsv1.DaemonSet
	err = r.Get(context.Background(), types.NamespacedName{Name: "test-collector", Namespace: "default"}, &updatedDS)
	require.NoError(t, err)
	require.Equal(t, "test-image:v1", updatedDS.Spec.Template.Spec.Containers[0].Image)
}

func TestCollectorReconciler_CreateCollector(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	// Minimal Collector CRD
	collector := &adxmonv1.Collector{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "adx-mon.azure.com/v1",
			Kind:       "Collector",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-collector",
			Namespace: "default",
		},
		Spec: adxmonv1.CollectorSpec{
			Image:           "test-image:v1",
			IngestorEndpoint: "https://test-ingestor.svc.cluster.local",
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(collector).WithStatusSubresource(&adxmonv1.Collector{}).Build()
	r := &CollectorReconciler{
		Client:             client,
		Scheme:             scheme,
		waitForReadyReason: "WaitForReady",
	}

	result, err := r.CreateCollector(context.Background(), collector)
	require.NoError(t, err)
	require.Equal(t, ctrl.Result{RequeueAfter: time.Minute}, result)

	// Verify condition is set - we need to refetch the object to see the updated status
	var updatedCollector adxmonv1.Collector
	require.NoError(t, r.Get(context.Background(), types.NamespacedName{Name: "test-collector", Namespace: "default"}, &updatedCollector))
	require.Len(t, updatedCollector.Status.Conditions, 1)
	require.Equal(t, adxmonv1.CollectorConditionOwner, updatedCollector.Status.Conditions[0].Type)
	require.Equal(t, metav1.ConditionTrue, updatedCollector.Status.Conditions[0].Status)
	require.Equal(t, "WaitForReady", updatedCollector.Status.Conditions[0].Reason)
}

func TestCollectorReconciler_ApplyDefaults(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))

	r := &CollectorReconciler{
		Scheme: scheme,
	}

	// Test auto-configuration of defaults
	collector := &adxmonv1.Collector{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-collector",
			Namespace: "test-namespace",
		},
		Spec: adxmonv1.CollectorSpec{
			// Leave image and endpoint empty to test defaults
		},
	}

	r.applyDefaults(collector)

	require.Equal(t, "ghcr.io/azure/adx-mon/collector:latest", collector.Spec.Image)
	require.Equal(t, "https://ingestor.test-namespace.svc.cluster.local", collector.Spec.IngestorEndpoint)

	// Test that existing values are not overridden
	collector.Spec.Image = "custom-image:v1"
	collector.Spec.IngestorEndpoint = "https://custom-ingestor.example.com"

	r.applyDefaults(collector)

	require.Equal(t, "custom-image:v1", collector.Spec.Image)
	require.Equal(t, "https://custom-ingestor.example.com", collector.Spec.IngestorEndpoint)
}