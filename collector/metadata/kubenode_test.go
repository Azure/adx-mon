package metadata

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
)

func TestKubeNode_OpenAndMetadata(t *testing.T) {
	t.Parallel()

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-test",
			Labels: map[string]string{
				"role": "worker",
			},
			Annotations: map[string]string{
				"zone": "us-east",
			},
		},
	}

	client := fake.NewSimpleClientset(node)
	watcher := NewKubeNode(client, "node-test")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, watcher.Open(ctx))
	t.Cleanup(func() {
		require.NoError(t, watcher.Close())
	})

	label, ok := watcher.Label("role")
	require.True(t, ok)
	require.Equal(t, "worker", label)

	found := map[string]string{}
	watcher.WalkAnnotations(func(key, value string) {
		found[key] = value
	})
	require.Equal(t, "us-east", found["zone"])

	current, err := client.CoreV1().Nodes().Get(context.Background(), "node-test", metav1.GetOptions{})
	require.NoError(t, err)
	current.Labels["role"] = "infra"
	current.Annotations["zone"] = "us-west"
	_, err = client.CoreV1().Nodes().Update(context.Background(), current, metav1.UpdateOptions{})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		value, ok := watcher.Label("role")
		return ok && value == "infra"
	}, time.Second, 20*time.Millisecond, "expected role label to update")

	require.Eventually(t, func() bool {
		value, ok := watcher.Annotation("zone")
		return ok && value == "us-west"
	}, time.Second, 20*time.Millisecond, "expected zone annotation to update")
}

func TestKubeNode_Walkers(t *testing.T) {
	t.Parallel()

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-test",
			Labels: map[string]string{
				"role":  "worker",
				"arch":  "amd64",
				"zone":  "us-east",
				"class": "general",
			},
			Annotations: map[string]string{
				"alpha": "one",
				"beta":  "two",
			},
		},
	}

	client := fake.NewSimpleClientset(node)
	watcher := NewKubeNode(client, "node-test")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, watcher.Open(ctx))
	t.Cleanup(func() {
		require.NoError(t, watcher.Close())
	})

	labels := map[string]string{}
	watcher.WalkLabels(func(key, value string) {
		labels[key] = value
	})
	require.Equal(t, "amd64", labels["arch"])
	require.Equal(t, "worker", labels["role"])

	annotations := map[string]string{}
	watcher.WalkAnnotations(func(key, value string) {
		annotations[key] = value
	})
	require.Equal(t, "one", annotations["alpha"])
	require.Equal(t, "two", annotations["beta"])
}

func TestKubeNode_DeleteEventKeepsMetadata(t *testing.T) {
	t.Parallel()

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-test",
			Labels: map[string]string{
				"role": "worker",
			},
			Annotations: map[string]string{
				"zone": "us-east",
			},
		},
	}

	client := fake.NewSimpleClientset(node)
	watcher := NewKubeNode(client, "node-test")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, watcher.Open(ctx))
	t.Cleanup(func() {
		require.NoError(t, watcher.Close())
	})

	value, ok := watcher.Label("role")
	require.True(t, ok)
	require.Equal(t, "worker", value)

	value, ok = watcher.Label("role")
	require.True(t, ok)
	require.Equal(t, "worker", value)

	zone, ok := watcher.Annotation("zone")
	require.True(t, ok)
	require.Equal(t, "us-east", zone)

	err := client.CoreV1().Nodes().Delete(context.Background(), "node-test", metav1.DeleteOptions{})
	require.NoError(t, err)
	// Force the delete callback synchronously to verify behavior.
	watcher.handleDelete(node)
	watcher.handleDelete(cache.DeletedFinalStateUnknown{Obj: node})

	value, ok = watcher.Label("role")
	require.True(t, ok)
	require.Equal(t, "worker", value)

	zone, ok = watcher.Annotation("zone")
	require.True(t, ok)
	require.Equal(t, "us-east", zone)
}

func TestKubeNode_OpenRequiresNodeName(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset()
	watcher := NewKubeNode(client, "")

	err := watcher.Open(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "node name is empty")
}

func TestKubeNode_CloseIdempotent(t *testing.T) {
	t.Parallel()

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-test",
		},
	}

	client := fake.NewSimpleClientset(node)
	watcher := NewKubeNode(client, "node-test")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, watcher.Open(ctx))

	require.NoError(t, watcher.Close())
	require.NoError(t, watcher.Close())
}
