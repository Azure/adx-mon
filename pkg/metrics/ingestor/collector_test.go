package ingestor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	clocktesting "k8s.io/utils/clock/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlclientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type stubNodeMetricsClient struct {
	responses     []*metricsv1beta1.NodeMetricsList
	err           error
	responseIndex int
}

func (s *stubNodeMetricsClient) List(ctx context.Context, opts metav1.ListOptions) (*metricsv1beta1.NodeMetricsList, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.responseIndex >= len(s.responses) {
		return nil, errors.New("no more responses")
	}
	resp := s.responses[s.responseIndex]
	s.responseIndex++
	return resp, nil
}

func TestAverageCPUWithHistory(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	pods := []client.Object{
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ingestor-0",
				Namespace: "adx-mon",
				Labels:    map[string]string{"app": "ingestor"},
			},
			Spec: corev1.PodSpec{NodeName: "node-a"},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ingestor-1",
				Namespace: "adx-mon",
				Labels:    map[string]string{"app": "ingestor"},
			},
			Spec: corev1.PodSpec{NodeName: "node-b"},
		},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node-a"},
			Status:     corev1.NodeStatus{Capacity: corev1.ResourceList{corev1.ResourceCPU: *resource.NewMilliQuantity(2000, resource.DecimalSI)}},
		},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node-b"},
			Status:     corev1.NodeStatus{Capacity: corev1.ResourceList{corev1.ResourceCPU: *resource.NewMilliQuantity(2000, resource.DecimalSI)}},
		},
	}

	fakeClient := ctrlclientfake.NewClientBuilder().WithScheme(scheme).WithObjects(pods...).Build()

	metricsClient := &stubNodeMetricsClient{
		responses: []*metricsv1beta1.NodeMetricsList{
			{
				Items: []metricsv1beta1.NodeMetrics{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "node-a"},
						Usage:      corev1.ResourceList{corev1.ResourceCPU: *resource.NewMilliQuantity(800, resource.DecimalSI)},
					},
					{
						ObjectMeta: metav1.ObjectMeta{Name: "node-b"},
						Usage:      corev1.ResourceList{corev1.ResourceCPU: *resource.NewMilliQuantity(400, resource.DecimalSI)},
					},
				},
			},
			{
				Items: []metricsv1beta1.NodeMetrics{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "node-a"},
						Usage:      corev1.ResourceList{corev1.ResourceCPU: *resource.NewMilliQuantity(1200, resource.DecimalSI)},
					},
					{
						ObjectMeta: metav1.ObjectMeta{Name: "node-b"},
						Usage:      corev1.ResourceList{corev1.ResourceCPU: *resource.NewMilliQuantity(600, resource.DecimalSI)},
					},
				},
			},
		},
	}

	now := time.Now()
	fakeClock := clocktesting.NewFakeClock(now)

	collector := NewCollector(fakeClient, metricsClient, "adx-mon", "ingestor", fakeClock)

	ctx := context.Background()

	avg, err := collector.AverageCPU(ctx, 10*time.Minute)
	require.NoError(t, err)
	require.InDelta(t, 30.0, avg, 0.01)

	fakeClock.Step(6 * time.Minute)

	avg2, err := collector.AverageCPU(ctx, 5*time.Minute)
	require.NoError(t, err)
	require.InDelta(t, 45.0, avg2, 0.01)
}

func TestAverageCPUNoPods(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	fakeClient := ctrlclientfake.NewClientBuilder().WithScheme(scheme).Build()
	metricsClient := &stubNodeMetricsClient{
		responses: []*metricsv1beta1.NodeMetricsList{{}},
	}

	collector := NewCollector(fakeClient, metricsClient, "adx-mon", "ingestor", clocktesting.NewFakeClock(time.Now()))

	_, err := collector.AverageCPU(context.Background(), 5*time.Minute)
	require.ErrorIs(t, err, ErrNoPodsScheduled)
}

func TestCollectorWithCustomLabelSelector(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	pods := []client.Object{
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ingestor-0",
				Namespace: "adx-mon",
				Labels:    map[string]string{"component": "ingestor"},
			},
			Spec: corev1.PodSpec{NodeName: "node-a"},
		},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node-a"},
			Status:     corev1.NodeStatus{Capacity: corev1.ResourceList{corev1.ResourceCPU: *resource.NewMilliQuantity(4000, resource.DecimalSI)}},
		},
	}

	fakeClient := ctrlclientfake.NewClientBuilder().WithScheme(scheme).WithObjects(pods...).Build()

	metricsClient := &stubNodeMetricsClient{
		responses: []*metricsv1beta1.NodeMetricsList{
			{
				Items: []metricsv1beta1.NodeMetrics{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "node-a"},
						Usage:      corev1.ResourceList{corev1.ResourceCPU: *resource.NewMilliQuantity(2000, resource.DecimalSI)},
					},
				},
			},
		},
	}

	selector := labels.SelectorFromSet(labels.Set{"component": "ingestor"})
	collector := NewCollector(fakeClient, metricsClient, "adx-mon", "ingestor", clocktesting.NewFakeClock(time.Now()), WithLabelSelector(selector))

	avg, err := collector.AverageCPU(context.Background(), 5*time.Minute)
	require.NoError(t, err)
	require.InDelta(t, 50.0, avg, 0.01)
}
