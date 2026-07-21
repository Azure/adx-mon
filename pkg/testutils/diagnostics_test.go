package testutils

import (
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestFormatPodIncludesFailureState(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:         "adx-mon",
			Name:              "ingestor-0",
			CreationTimestamp: metav1.NewTime(time.Date(2026, 7, 21, 1, 2, 3, 0, time.UTC)),
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "ingestor", Image: "ingestor:test"}}},
		Status: corev1.PodStatus{
			Phase:      corev1.PodRunning,
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionFalse}},
			ContainerStatuses: []corev1.ContainerStatus{{
				Name:         "ingestor",
				ImageID:      "sha256:123",
				RestartCount: 2,
				State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{
					StartedAt: metav1.NewTime(time.Date(2026, 7, 21, 1, 3, 4, 0, time.UTC)),
				}},
				LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
					Reason: "Error",
				}},
			}},
		},
	}

	got := formatPod(pod)
	for _, want := range []string{"adx-mon/ingestor-0", "phase=Running", "ready=false", "restarts=2", "image=\"ingestor:test\"", "imageID=\"sha256:123\"", "started=\"2026-07-21T01:03:04Z\"", "lastTermination=\"Error\""} {
		if !strings.Contains(got, want) {
			t.Errorf("formatPod() = %q, want it to contain %q", got, want)
		}
	}
}

func TestReplicas(t *testing.T) {
	if got := replicas(nil); got != 1 {
		t.Fatalf("replicas(nil) = %d, want 1", got)
	}
	if got := replicas(ptr.To(int32(3))); got != 3 {
		t.Fatalf("replicas(3) = %d, want 3", got)
	}
}
