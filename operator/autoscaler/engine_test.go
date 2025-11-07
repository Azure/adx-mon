package autoscaler

import (
	"context"
	"errors"
	"testing"
	"time"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	record "k8s.io/client-go/tools/record"
	clocktesting "k8s.io/utils/clock/testing"
	"k8s.io/utils/ptr"
	ctrlclientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type stubCollector struct {
	values []float64
	err    error
}

func (s *stubCollector) AverageCPU(ctx context.Context, window time.Duration) (float64, error) {
	if s.err != nil {
		return 0, s.err
	}
	if len(s.values) == 0 {
		return 0, nil
	}
	value := s.values[0]
	s.values = s.values[1:]
	return value, nil
}

func setupScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	require.NoError(t, adxmonv1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	return scheme
}

func TestEngineScaleUp(t *testing.T) {
	scheme := setupScheme(t)

	ingestor := &adxmonv1.Ingestor{
		ObjectMeta: metav1.ObjectMeta{Name: "ingestor", Namespace: "test"},
		Spec: adxmonv1.IngestorSpec{
			Replicas:           2,
			ADXClusterSelector: &metav1.LabelSelector{},
			Autoscaler: &adxmonv1.IngestorAutoscalerSpec{
				Enabled:             true,
				MinReplicas:         1,
				MaxReplicas:         10,
				ScaleUpCPUThreshold: 60,
				ScaleUpBasePercent:  25,
			},
		},
	}

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "ingestor", Namespace: "test"},
		Spec: appsv1.StatefulSetSpec{
			Replicas: ptr.To[int32](2),
		},
		Status: appsv1.StatefulSetStatus{
			Replicas:      2,
			ReadyReplicas: 2,
		},
	}

	client := ctrlclientfake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&adxmonv1.Ingestor{}).
		WithObjects(ingestor.DeepCopy(), sts.DeepCopy()).
		Build()

	collector := &stubCollector{values: []float64{80}}
	recorder := record.NewFakeRecorder(10)
	clk := clocktesting.NewFakeClock(time.Now())

	engine := NewEngine(client, collector, recorder, clk)

	ctx := context.Background()

	ing := &adxmonv1.Ingestor{}
	require.NoError(t, client.Get(ctx, types.NamespacedName{Name: "ingestor", Namespace: "test"}, ing))

	requeue, err := engine.Run(ctx, ing)
	require.NoError(t, err)
	require.Equal(t, 5*time.Minute, requeue)

	updatedSTS := &appsv1.StatefulSet{}
	require.NoError(t, client.Get(ctx, types.NamespacedName{Name: "ingestor", Namespace: "test"}, updatedSTS))
	require.Equal(t, int32(3), *updatedSTS.Spec.Replicas)

	updatedIngestor := &adxmonv1.Ingestor{}
	require.NoError(t, client.Get(ctx, types.NamespacedName{Name: "ingestor", Namespace: "test"}, updatedIngestor))
	require.NotNil(t, updatedIngestor.Status.Autoscaler)
	require.Equal(t, adxmonv1.AutoscalerActionScaleUp, updatedIngestor.Status.Autoscaler.LastAction)
	require.NotNil(t, updatedIngestor.Status.Autoscaler.LastScaleTime)
}

func TestEngineScaleDownFlow(t *testing.T) {
	scheme := setupScheme(t)

	ingestor := &adxmonv1.Ingestor{
		ObjectMeta: metav1.ObjectMeta{Name: "ingestor", Namespace: "test"},
		Spec: adxmonv1.IngestorSpec{
			Replicas:           2,
			ADXClusterSelector: &metav1.LabelSelector{},
			Autoscaler: &adxmonv1.IngestorAutoscalerSpec{
				Enabled:               true,
				MinReplicas:           1,
				MaxReplicas:           5,
				ScaleDownCPUThreshold: 30,
				ScaleUpBasePercent:    25,
			},
		},
	}

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "ingestor", Namespace: "test"},
		Spec: appsv1.StatefulSetSpec{
			Replicas: ptr.To[int32](2),
		},
		Status: appsv1.StatefulSetStatus{
			Replicas:      2,
			ReadyReplicas: 2,
		},
	}

	pod0 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ingestor-0",
			Namespace: "test",
			Labels:    map[string]string{"app": "ingestor"},
		},
		Spec: corev1.PodSpec{NodeName: "node-a"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}
	pod1 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ingestor-1",
			Namespace: "test",
			Labels:    map[string]string{"app": "ingestor"},
		},
		Spec: corev1.PodSpec{NodeName: "node-b"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}

	client := ctrlclientfake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&adxmonv1.Ingestor{}).
		WithObjects(ingestor.DeepCopy(), sts.DeepCopy(), pod0.DeepCopy(), pod1.DeepCopy()).
		Build()

	collector := &stubCollector{values: []float64{20, 20}}
	recorder := record.NewFakeRecorder(10)
	clk := clocktesting.NewFakeClock(time.Now())

	engine := NewEngine(client, collector, recorder, clk)

	ctx := context.Background()

	// First run: request shutdown on highest ordinal pod
	ing := &adxmonv1.Ingestor{}
	require.NoError(t, client.Get(ctx, types.NamespacedName{Name: "ingestor", Namespace: "test"}, ing))

	_, err := engine.Run(ctx, ing)
	require.NoError(t, err)

	annotated := &corev1.Pod{}
	require.NoError(t, client.Get(ctx, types.NamespacedName{Name: "ingestor-1", Namespace: "test"}, annotated))
	require.Contains(t, annotated.Annotations, shutdownRequestedAnnotation)
	initialReplicaCount := &appsv1.StatefulSet{}
	require.NoError(t, client.Get(ctx, types.NamespacedName{Name: "ingestor", Namespace: "test"}, initialReplicaCount))
	require.Equal(t, int32(2), *initialReplicaCount.Spec.Replicas)

	// Simulate shutdown completion
	if annotated.Annotations == nil {
		annotated.Annotations = map[string]string{}
	}
	annotated.Annotations[shutdownCompletedAnnotation] = time.Now().Format(time.RFC3339)
	require.NoError(t, client.Update(ctx, annotated))

	// Fetch fresh ingestor for second run
	ing2 := &adxmonv1.Ingestor{}
	require.NoError(t, client.Get(ctx, types.NamespacedName{Name: "ingestor", Namespace: "test"}, ing2))

	_, err = engine.Run(ctx, ing2)
	require.NoError(t, err)

	updatedSTS := &appsv1.StatefulSet{}
	require.NoError(t, client.Get(ctx, types.NamespacedName{Name: "ingestor", Namespace: "test"}, updatedSTS))
	require.Equal(t, int32(1), *updatedSTS.Spec.Replicas)

	updatedIngestor := &adxmonv1.Ingestor{}
	require.NoError(t, client.Get(ctx, types.NamespacedName{Name: "ingestor", Namespace: "test"}, updatedIngestor))
	require.NotNil(t, updatedIngestor.Status.Autoscaler)
	require.Equal(t, adxmonv1.AutoscalerActionScaleDown, updatedIngestor.Status.Autoscaler.LastAction)
	require.NotNil(t, updatedIngestor.Status.Autoscaler.LastScaleTime)

	// Pod should be deleted after scale down
	err = client.Get(ctx, types.NamespacedName{Name: "ingestor-1", Namespace: "test"}, &corev1.Pod{})
	require.Error(t, err)
}

func TestEngineCollectorError(t *testing.T) {
	scheme := setupScheme(t)

	ingestor := &adxmonv1.Ingestor{
		ObjectMeta: metav1.ObjectMeta{Name: "ingestor", Namespace: "test"},
		Spec: adxmonv1.IngestorSpec{
			Replicas:           2,
			ADXClusterSelector: &metav1.LabelSelector{},
			Autoscaler: &adxmonv1.IngestorAutoscalerSpec{
				Enabled:             true,
				MinReplicas:         1,
				MaxReplicas:         5,
				ScaleUpCPUThreshold: 60,
			},
		},
	}

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "ingestor", Namespace: "test"},
		Spec:       appsv1.StatefulSetSpec{Replicas: ptr.To[int32](2)},
		Status: appsv1.StatefulSetStatus{
			Replicas:      2,
			ReadyReplicas: 2,
		},
	}

	client := ctrlclientfake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&adxmonv1.Ingestor{}).
		WithObjects(ingestor.DeepCopy(), sts.DeepCopy()).
		Build()

	collector := &stubCollector{err: errors.New("metrics unavailable")}
	recorder := record.NewFakeRecorder(10)
	clk := clocktesting.NewFakeClock(time.Now())

	engine := NewEngine(client, collector, recorder, clk)

	ctx := context.Background()
	ing := &adxmonv1.Ingestor{}
	require.NoError(t, client.Get(ctx, types.NamespacedName{Name: "ingestor", Namespace: "test"}, ing))

	requeue, err := engine.Run(ctx, ing)
	require.NoError(t, err)
	require.Equal(t, defaultRequeue, requeue)

	updatedSTS := &appsv1.StatefulSet{}
	require.NoError(t, client.Get(ctx, types.NamespacedName{Name: "ingestor", Namespace: "test"}, updatedSTS))
	require.Equal(t, int32(2), *updatedSTS.Spec.Replicas)

	updatedIngestor := &adxmonv1.Ingestor{}
	require.NoError(t, client.Get(ctx, types.NamespacedName{Name: "ingestor", Namespace: "test"}, updatedIngestor))
	require.NotNil(t, updatedIngestor.Status.Autoscaler)
	require.Equal(t, adxmonv1.AutoscalerActionSkip, updatedIngestor.Status.Autoscaler.LastAction)
	require.Contains(t, updatedIngestor.Status.Autoscaler.Reason, "metrics unavailable")
}

func TestEngineCollectorEmptyValues(t *testing.T) {
	scheme := setupScheme(t)

	ingestor := &adxmonv1.Ingestor{
		ObjectMeta: metav1.ObjectMeta{Name: "ingestor", Namespace: "test"},
		Spec: adxmonv1.IngestorSpec{
			Replicas:           2,
			ADXClusterSelector: &metav1.LabelSelector{},
			Autoscaler: &adxmonv1.IngestorAutoscalerSpec{
				Enabled:               true,
				MinReplicas:           2,
				MaxReplicas:           5,
				ScaleDownCPUThreshold: 30,
			},
		},
	}

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "ingestor", Namespace: "test"},
		Spec: appsv1.StatefulSetSpec{
			Replicas: ptr.To[int32](2),
		},
		Status: appsv1.StatefulSetStatus{
			Replicas:      2,
			ReadyReplicas: 2,
		},
	}

	client := ctrlclientfake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&adxmonv1.Ingestor{}).
		WithObjects(ingestor.DeepCopy(), sts.DeepCopy()).
		Build()

	collector := &stubCollector{}
	recorder := record.NewFakeRecorder(10)
	clk := clocktesting.NewFakeClock(time.Now())

	engine := NewEngine(client, collector, recorder, clk)

	ctx := context.Background()
	ing := &adxmonv1.Ingestor{}
	require.NoError(t, client.Get(ctx, types.NamespacedName{Name: "ingestor", Namespace: "test"}, ing))

	requeue, err := engine.Run(ctx, ing)
	require.NoError(t, err)
	require.Equal(t, 5*time.Minute, requeue)

	updatedSTS := &appsv1.StatefulSet{}
	require.NoError(t, client.Get(ctx, types.NamespacedName{Name: "ingestor", Namespace: "test"}, updatedSTS))
	require.Equal(t, int32(2), *updatedSTS.Spec.Replicas)

	updatedIngestor := &adxmonv1.Ingestor{}
	require.NoError(t, client.Get(ctx, types.NamespacedName{Name: "ingestor", Namespace: "test"}, updatedIngestor))
	require.NotNil(t, updatedIngestor.Status.Autoscaler)
	require.Equal(t, adxmonv1.AutoscalerActionSkip, updatedIngestor.Status.Autoscaler.LastAction)
	require.Contains(t, updatedIngestor.Status.Autoscaler.Reason, "min replicas")
}
