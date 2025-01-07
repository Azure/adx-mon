package alerter_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/Azure/adx-mon/pkg/testutils/alerter"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func TestAlerter(t *testing.T) {
	testutils.IntegrationTest(t)

	c, err := alerter.Run(context.Background())
	testcontainers.CleanupContainer(t, c)
	require.NoError(t, err)
}

func TestInCluster(t *testing.T) {
	testutils.IntegrationTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	t.Cleanup(cancel)

	k3sContainer, err := k3s.Run(ctx, "rancher/k3s:v1.31.2-k3s1")
	testcontainers.CleanupContainer(t, k3sContainer)
	require.NoError(t, err)

	c, err := alerter.Run(ctx, alerter.WithCluster(ctx, k3sContainer))
	testcontainers.CleanupContainer(t, c)
	require.NoError(t, err)

	restConfig, _, err := testutils.GetKubeConfig(ctx, k3sContainer)
	require.NoError(t, err)
	clientset, err := kubernetes.NewForConfig(restConfig)
	require.NoError(t, err)

	require.Eventually(t, func() bool {

		pods, err := clientset.CoreV1().Pods("adx-mon").List(ctx, metav1.ListOptions{})
		require.NoError(t, err)

		for _, pod := range pods.Items {
			if strings.HasPrefix(pod.GetName(), "alerter") {
				return pod.Status.Phase == "Running"
			}
		}

		return false
	}, 10*time.Minute, time.Second)
}
