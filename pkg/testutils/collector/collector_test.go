package collector_test

import (
	"context"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/Azure/adx-mon/pkg/testutils/collector"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func TestCollector(t *testing.T) {
	testutils.IntegrationTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	t.Run("outside cluster", func(t *testing.T) {
		c, err := collector.Run(ctx)
		testcontainers.CleanupContainer(t, c)
		require.NoError(t, err)
	})

	t.Run("inside cluster", func(t *testing.T) {
		k3sContainer, err := k3s.Run(ctx, "rancher/k3s:v1.31.2-k3s1")
		testcontainers.CleanupContainer(t, k3sContainer)
		require.NoError(t, err)

		collectorContainer, err := collector.Run(ctx, collector.WithCluster(ctx, k3sContainer))
		testcontainers.CleanupContainer(t, collectorContainer)
		require.NoError(t, err)

		restConfig, _, err := testutils.GetKubeConfig(ctx, k3sContainer)
		require.NoError(t, err)

		clientset, err := kubernetes.NewForConfig(restConfig)
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			_, err := clientset.CoreV1().ConfigMaps("adx-mon").Get(ctx, "collector-config", metav1.GetOptions{})
			return err == nil
		}, 10*time.Minute, time.Second)

		require.Eventually(t, func() bool {
			pods, err := clientset.CoreV1().Pods("adx-mon").List(ctx, metav1.ListOptions{
				LabelSelector: "adxmon=collector",
			})
			if err != nil {
				return false
			}
			if len(pods.Items) != 1 {
				return false
			}
			return pods.Items[0].Status.Phase == "Running"
		}, 30*time.Minute, time.Second)
	})
}
