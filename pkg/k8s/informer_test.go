package k8s

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"

	"github.com/stretchr/testify/require"
)

func TestPodInformerWatchdog(t *testing.T) {
	originalThreshold := PodInformerStaleThreshold
	originalInterval := podInformerWatchdogInterval
	originalPanic := podInformerPanic
	defer func() {
		PodInformerStaleThreshold = originalThreshold
		podInformerWatchdogInterval = originalInterval
		podInformerPanic = originalPanic
	}()

	PodInformerStaleThreshold = 30 * time.Millisecond
	podInformerWatchdogInterval = 10 * time.Millisecond

	t.Run("emptyStoreDoesNotPanic", func(t *testing.T) {
		client := fake.NewSimpleClientset()
		ctx, cancel := context.WithCancel(context.Background())

		panicCh := make(chan string, 1)
		podInformerPanic = func(msg string) { panicCh <- msg }
		defer func() { podInformerPanic = originalPanic }()

		informer := NewPodInformer(client, "node1")
		reg, err := informer.Add(ctx, cache.ResourceEventHandlerFuncs{})
		require.NoError(t, err)
		defer func() {
			cancel()
			informer.Remove(reg)
		}()

		informer.lastActivity.Store(time.Now().Add(-2 * PodInformerStaleThreshold).UnixNano())

		select {
		case msg := <-panicCh:
			t.Fatalf("unexpected watchdog panic: %s", msg)
		case <-time.After(3 * PodInformerStaleThreshold):
			// Expected: watchdog should see empty store and skip panic.
		}
	})

	t.Run("nonEmptyStorePanics", func(t *testing.T) {
		client := fake.NewSimpleClientset()
		ctx, cancel := context.WithCancel(context.Background())

		panicCh := make(chan string, 1)
		podInformerPanic = func(msg string) { panicCh <- msg }
		defer func() { podInformerPanic = originalPanic }()

		informer := NewPodInformer(client, "node1")
		reg, err := informer.Add(ctx, cache.ResourceEventHandlerFuncs{})
		require.NoError(t, err)
		defer func() {
			cancel()
			informer.Remove(reg)
		}()

		store := informer.informer.GetStore()
		require.NotNil(t, store)
		err = store.Add(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod-1",
				Namespace: "default",
				UID:       "uid-1",
			},
			Spec: corev1.PodSpec{
				NodeName: "node1",
			},
		})
		require.NoError(t, err)

		informer.lastActivity.Store(time.Now().Add(-2 * PodInformerStaleThreshold).UnixNano())

		select {
		case msg := <-panicCh:
			require.Contains(t, msg, "stale")
		case <-time.After(3 * PodInformerStaleThreshold):
			t.Fatal("expected watchdog panic when store is non-empty")
		}
	})
}
