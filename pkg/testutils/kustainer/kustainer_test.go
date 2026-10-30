package kustainer

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIngestionBatchingPolicyRendering(t *testing.T) {
	tests := []struct {
		Duration time.Duration
		Expect   string
	}{
		{
			Duration: 32 * time.Second,
			Expect:   "00:00:32",
		},
		{
			Duration: 3 * time.Minute,
			Expect:   "00:03:00",
		},
		{
			Duration: 10 * time.Hour,
			Expect:   "10:00:00",
		},
	}
	for _, tt := range tests {
		t.Run(tt.Duration.String(), func(t *testing.T) {
			p := IngestionBatchingPolicy{
				MaximumBatchingTimeSpan: 30 * time.Second,
			}
			d, err := p.MarshalJSON()
			require.NoError(t, err)
			require.JSONEq(t, `{"MaximumBatchingTimeSpan":"00:00:30"}`, string(d))
		})
	}
}

func TestReadyPodName(t *testing.T) {
	pods := []corev1.Pod{
		{ObjectMeta: metav1.ObjectMeta{Name: "old"}, Status: corev1.PodStatus{
			Phase:             corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{{Ready: false}},
		}},
		{ObjectMeta: metav1.ObjectMeta{Name: "pending"}, Status: corev1.PodStatus{
			Phase:             corev1.PodPending,
			ContainerStatuses: []corev1.ContainerStatus{{Ready: true}},
		}},
		{ObjectMeta: metav1.ObjectMeta{Name: "ready"}, Status: corev1.PodStatus{
			Phase:             corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{{Ready: true}},
		}},
	}

	require.Equal(t, "ready", readyPodName(pods))
}

func TestDescribePods(t *testing.T) {
	pods := []corev1.Pod{
		{ObjectMeta: metav1.ObjectMeta{Name: "kusto"}, Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodScheduled, Status: corev1.ConditionTrue},
				{Type: corev1.ContainersReady, Status: corev1.ConditionFalse, Reason: "ContainersNotReady"},
			},
		}},
	}

	require.Equal(t, "kusto phase=Pending not-true=[ContainersReady=False(ContainersNotReady)]", describePods(pods))
}
