package testutils_test

import (
	"context"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/Azure/adx-mon/pkg/testutils/collector"
	"github.com/Azure/adx-mon/pkg/testutils/ingestor"
	"github.com/Azure/adx-mon/pkg/testutils/kustainer"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func TestRollback(t *testing.T) {
	testutils.RollbackTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	t.Cleanup(cancel)

	// Instantiate the cluster
	r := &rollback{t: t}
	r.Open(ctx)

	// Install our current release
	r.InstallReleased(ctx)
	r.Verify(ctx)

	// Upgrade, installing the release candidate
	r.InstallCadidate(ctx)
	r.Verify(ctx)

	// Downgrade, installing the current release
	r.setIngestorContainerImage(ctx, r.rollbackIngestorImage)
	r.setCollectorContainerImage(ctx, r.rollbackCollectorImage)
	r.Verify(ctx)
}

type rollback struct {
	t *testing.T

	kustoContainer *kustainer.KustainerContainer
	k3sContainer   *k3s.K3sContainer

	restConfig *rest.Config
	k8sClient  *kubernetes.Clientset

	rollbackIngestorImage  string
	rollbackCollectorImage string
}

func (r *rollback) Open(ctx context.Context) {
	r.t.Helper()
	r.t.Log("Starting cluster")

	var err error
	r.k3sContainer, err = k3s.Run(ctx, "rancher/k3s:v1.31.2-k3s1")
	testcontainers.CleanupContainer(r.t, r.k3sContainer)
	require.NoError(r.t, err)

	r.kustoContainer, err = kustainer.Run(ctx, "mcr.microsoft.com/azuredataexplorer/kustainer-linux:latest", kustainer.WithCluster(ctx, r.k3sContainer))
	testcontainers.CleanupContainer(r.t, r.kustoContainer)
	require.NoError(r.t, err)

	r.restConfig, _, err = testutils.GetKubeConfig(ctx, r.k3sContainer)
	require.NoError(r.t, err)
	require.NoError(r.t, r.kustoContainer.PortForward(ctx, r.restConfig))

	r.k8sClient, err = kubernetes.NewForConfig(r.restConfig)
	require.NoError(r.t, err)

	r.rollbackIngestorImage = "ghcr.io/azure/adx-mon/ingestor:latest"
	r.rollbackCollectorImage = "ghcr.io/azure/adx-mon/collector:latest"
	r.k3sContainer.LoadImages(ctx, r.rollbackIngestorImage, r.rollbackCollectorImage)

	r.t.Log("Creating databases")
	opts := kustainer.IngestionBatchingPolicy{
		MaximumBatchingTimeSpan: 30 * time.Second,
	}
	for _, dbName := range []string{"Metrics", "Logs"} {
		require.NoError(r.t, r.kustoContainer.CreateDatabase(ctx, dbName))
		require.NoError(r.t, r.kustoContainer.SetIngestionBatchingPolicy(ctx, dbName, opts))
	}

	kubeconfig, err := testutils.WriteKubeConfig(ctx, r.k3sContainer, r.t.TempDir())
	require.NoError(r.t, err)
	r.t.Logf("Kubeconfig: %s", kubeconfig)
	r.t.Logf("Kustainer: %s", r.kustoContainer.ConnectionUrl())
}

func (r *rollback) InstallReleased(ctx context.Context) {
	r.t.Helper()
	r.t.Log("Installing released")

	require.NoError(r.t, ingestor.InstallManifests(ctx, r.k3sContainer))
	ss := ingestor.StatefulSet()
	ss.Spec.Template.Spec.Containers[0].Image = r.rollbackIngestorImage

	// Have to wait for the manifests to come online
	require.Eventually(r.t, func() bool {
		_, err := r.k8sClient.AppsV1().StatefulSets("adx-mon").Create(ctx, ss, metav1.CreateOptions{})
		return err == nil
	}, 10*time.Minute, time.Second)

	require.NoError(r.t, collector.InstallManifests(ctx, r.k3sContainer))
	ds := collector.DaemonSet()
	ds.Spec.Template.Spec.Containers[0].Image = r.rollbackCollectorImage

	require.Eventually(r.t, func() bool {
		_, err := r.k8sClient.AppsV1().DaemonSets("adx-mon").Create(ctx, ds, metav1.CreateOptions{})
		return err == nil
	}, 10*time.Minute, time.Second)
}

func (r *rollback) InstallCadidate(ctx context.Context) {
	r.t.Helper()
	r.t.Log("Installing candidate")

	r.t.Run("install candidate ingestor", func(t *testing.T) {
		ingestorContainer, err := ingestor.Run(ctx)
		testcontainers.CleanupContainer(t, ingestorContainer)
		require.NoError(r.t, err)
		require.NoError(r.t, r.k3sContainer.LoadImages(ctx, ingestor.DefaultImage+":"+ingestor.DefaultTag))
		r.setIngestorContainerImage(ctx, ingestor.DefaultImage+":"+ingestor.DefaultTag)
	})

	r.t.Run("install candidate collector", func(t *testing.T) {
		collectorContainer, err := collector.Run(ctx)
		testcontainers.CleanupContainer(t, collectorContainer)
		require.NoError(t, err)
		require.NoError(r.t, r.k3sContainer.LoadImages(ctx, collector.DefaultImage+":"+collector.DefaultTag))
		r.setCollectorContainerImage(ctx, collector.DefaultImage+":"+collector.DefaultTag)
	})
}

func (r *rollback) setIngestorContainerImage(ctx context.Context, containerImage string) {
	r.t.Helper()

	patch := []byte(`{"spec":{"template":{"spec":{"containers":[{"name":"ingestor","image":"` + containerImage + `"}]}}}}`)
	_, err := r.k8sClient.AppsV1().StatefulSets("adx-mon").Patch(ctx, "ingestor", types.StrategicMergePatchType, patch, metav1.PatchOptions{})
	require.NoError(r.t, err)

	require.Eventually(r.t, func() bool {
		pods, err := r.k8sClient.CoreV1().Pods("adx-mon").List(ctx, metav1.ListOptions{
			LabelSelector: "app=ingestor",
		})
		if err != nil {
			return false
		}

		if len(pods.Items) != 1 {
			return false
		}
		for _, pod := range pods.Items {
			if pod.Spec.Containers[0].Image != containerImage {
				r.k8sClient.CoreV1().Pods("adx-mon").Delete(ctx, pod.Name, metav1.DeleteOptions{})
				return false
			}
			if pod.Status.Phase != "Running" {
				return false
			}
		}

		return true
	}, 10*time.Minute, time.Second)
}

func (r *rollback) setCollectorContainerImage(ctx context.Context, containerImage string) {
	r.t.Helper()

	patch := []byte(`{"spec":{"template":{"spec":{"containers":[{"name":"collector","image":"` + containerImage + `"}]}}}}`)
	_, err := r.k8sClient.AppsV1().DaemonSets("adx-mon").Patch(ctx, "collector", types.StrategicMergePatchType, patch, metav1.PatchOptions{})
	require.NoError(r.t, err)

	// Wait for all the Pods to be running again.
	require.Eventually(r.t, func() bool {
		pods, err := r.k8sClient.CoreV1().Pods("adx-mon").List(ctx, metav1.ListOptions{
			LabelSelector: " adxmon=collector",
		})
		if err != nil {
			return false
		}

		if len(pods.Items) != 1 {
			return false
		}
		for _, pod := range pods.Items {
			if pod.Spec.Containers[0].Image != containerImage {
				r.k8sClient.CoreV1().Pods("adx-mon").Delete(ctx, pod.Name, metav1.DeleteOptions{})
				return false
			}
			if pod.Status.Phase != "Running" {
				return false
			}
		}
		return true
	}, 10*time.Minute, time.Second)
}

func (r *rollback) Verify(ctx context.Context) {
	r.t.Helper()

	r.t.Run("tables exist", func(t *testing.T) {
		require.Eventually(t, func() bool {
			return testutils.TableExists(ctx, t, "Logs", "Ingestor", r.kustoContainer.ConnectionUrl()) &&
				testutils.FunctionExists(ctx, t, "Logs", "Ingestor", r.kustoContainer.ConnectionUrl()) &&
				testutils.TableExists(ctx, t, "Logs", "Collector", r.kustoContainer.ConnectionUrl()) &&
				testutils.FunctionExists(ctx, t, "Logs", "Collector", r.kustoContainer.ConnectionUrl())
		}, 10*time.Minute, time.Second)
	})

	r.t.Run("verify ingestor log flow", func(t *testing.T) {
		now := time.Now()
		require.Eventually(t, func() bool {
			return testutils.TableHasRowsSince(ctx, t, "Logs", "Ingestor", r.kustoContainer.ConnectionUrl(), now)
		}, 10*time.Minute, time.Second)
	})

	r.t.Run("verify collector log flow", func(t *testing.T) {
		now := time.Now()
		require.Eventually(t, func() bool {
			return testutils.TableHasRowsSince(ctx, t, "Logs", "Collector", r.kustoContainer.ConnectionUrl(), now)
		}, 10*time.Minute, time.Second)
	})

	r.t.Run("verify component health", func(t *testing.T) {
		now := time.Now()

		require.Eventually(t, func() bool {
			return testutils.TableHasRowsSince(ctx, t, "Metrics", "AdxmonCollectorHealthCheck", r.kustoContainer.ConnectionUrl(), now)
		}, 10*time.Minute, time.Second)

		require.Eventually(t, func() bool {
			return testutils.TableHasRowsSince(ctx, t, "Metrics", "AdxmonIngestorHealthCheck", r.kustoContainer.ConnectionUrl(), now)
		}, 10*time.Minute, time.Second)

		require.True(r.t, testutils.ComponentMetricsHealth(ctx, r.t, r.kustoContainer.ConnectionUrl(), now))
	})
}
