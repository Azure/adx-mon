package testutils_test

import (
	"context"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func TestRollback(t *testing.T) {
	testutils.RollbackTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	t.Cleanup(cancel)

	// StartCluster will build adx-mon and install those components into
	// the cluster.
	kustoUrl, k3sContainer := StartCluster(ctx, t)

	rollbackIngestorImage := "ghcr.io/azure/adx-mon/ingestor:latest"
	rollbackCollectorImage := "ghcr.io/azure/adx-mon/collector:latest"
	k3sContainer.LoadImages(ctx, rollbackIngestorImage, rollbackCollectorImage)

	// Wait for rows to show up in Kusto, which signals to us that adx-mon
	// is up and running.
	require.Eventually(t, func() bool {
		return testutils.TableHasRows(ctx, t, "Logs", "Ingestor", kustoUrl)
	}, 10*time.Minute, time.Second)

	restConfig, _, err := testutils.GetKubeConfig(ctx, k3sContainer)
	require.NoError(t, err)

	k8sClient, err := kubernetes.NewForConfig(restConfig)
	require.NoError(t, err)

	t.Run("Rollback Ingestor", func(t *testing.T) {

		// Now we rollback, by installing the previously published images.
		ingestorSs, err := k8sClient.AppsV1().StatefulSets("adx-mon").Get(ctx, "ingestor", metav1.GetOptions{})
		require.NoError(t, err)

		ingestorSs.Spec.Template.Spec.Containers[0].Image = rollbackIngestorImage

		_, err = k8sClient.AppsV1().StatefulSets("adx-mon").Update(ctx, ingestorSs, metav1.UpdateOptions{})
		require.NoError(t, err)

		// Wait for all the Pods to be running again.
		require.Eventually(t, func() bool {
			pods, err := k8sClient.CoreV1().Pods("adx-mon").List(ctx, metav1.ListOptions{
				LabelSelector: "app=ingestor",
			})
			if err != nil {
				return false
			}

			for _, pod := range pods.Items {
				if pod.Spec.Containers[0].Image != rollbackIngestorImage {
					return false
				}
				if pod.Status.Phase != "Running" {
					return false
				}
			}
			return true
		}, 10*time.Minute, time.Second)

		now := time.Now()
		require.Eventually(t, func() bool {
			return testutils.TableHasRowsSince(ctx, t, "Logs", "Ingestor", kustoUrl, now)
		}, 10*time.Minute, time.Second)
	})

	t.Run("Rollback Collector", func(t *testing.T) {

		// Now we rollback, by installing the previously published images.
		collectorDs, err := k8sClient.AppsV1().DaemonSets("adx-mon").Get(ctx, "collector", metav1.GetOptions{})
		require.NoError(t, err)

		collectorDs.Spec.Template.Spec.Containers[0].Image = rollbackCollectorImage

		_, err = k8sClient.AppsV1().DaemonSets("adx-mon").Update(ctx, collectorDs, metav1.UpdateOptions{})
		require.NoError(t, err)

		// Wait for all the Pods to be running again.
		require.Eventually(t, func() bool {
			pods, err := k8sClient.CoreV1().Pods("adx-mon").List(ctx, metav1.ListOptions{
				LabelSelector: " adxmon=collector",
			})
			if err != nil {
				return false
			}

			for _, pod := range pods.Items {
				if pod.Spec.Containers[0].Image != rollbackCollectorImage {
					return false
				}
				if pod.Status.Phase != "Running" {
					return false
				}
			}
			return true
		}, 10*time.Minute, time.Second)

		now := time.Now()
		require.Eventually(t, func() bool {
			return testutils.TableHasRowsSince(ctx, t, "Logs", "Collector", kustoUrl, now)
		}, 10*time.Minute, time.Second)
	})

}
