package ingestor_test

import (
	"context"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/Azure/adx-mon/pkg/testutils/ingestor"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func TestIngestor(t *testing.T) {
	testutils.IntegrationTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	t.Run("outside cluster", func(t *testing.T) {
		c, err := ingestor.Run(ctx)
		testcontainers.CleanupContainer(t, c)
		require.NoError(t, err)
	})

	t.Run("inside cluster", func(t *testing.T) {
		k3sContainer, err := k3s.Run(ctx, "rancher/k3s:v1.31.2-k3s1")
		testcontainers.CleanupContainer(t, k3sContainer)
		require.NoError(t, err)

		ingestorContainer, err := ingestor.Run(ctx, ingestor.WithCluster(ctx, k3sContainer))
		testcontainers.CleanupContainer(t, ingestorContainer)
		require.NoError(t, err)

		restConfig, _, err := testutils.GetKubeConfig(ctx, k3sContainer)
		require.NoError(t, err)

		clientset, err := kubernetes.NewForConfig(restConfig)
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			pods, err := clientset.CoreV1().Pods("adx-mon").List(ctx, metav1.ListOptions{
				LabelSelector: "app=ingestor",
			})
			if err != nil {
				return false
			}
			if len(pods.Items) != 1 {
				return false
			}
			return pods.Items[0].Status.Phase == "Running"
		}, 10*time.Minute, time.Second)
	})
}
