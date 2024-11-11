package k8s_test

import (
	"context"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/testutils/ingestor"
	"github.com/Azure/adx-mon/pkg/testutils/k8s"
	"github.com/Azure/adx-mon/pkg/testutils/kustainer"
	"github.com/stretchr/testify/require"
)

func TestK8s(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode")
	}

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(10*time.Minute))
	defer cancel()

	var manifests []string
	c := k8s.New(ctx, manifests)
	require.NoError(t, c.Open(ctx))
	require.NoError(t, c.Close())
}

func TestK8sWithStack(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode")
	}

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(10*time.Minute))
	defer cancel()

	manifests := []string{
		"build/k8s/ksm.yaml",
		"build/k8s/ingestor.yaml",
		"pkg/testutils/kustainer/k8s.yaml",
	}
	c := k8s.New(ctx, manifests)
	require.NoError(t, c.Open(ctx))
	require.NoError(t, c.InstallCRD(ctx, "kustomize/bases/functions_crd.yaml"))
	t.Logf("Kubeconfig: %s", c.GetKubeConfig())

	k := kustainer.New(c.GetKubeConfig())
	require.NoError(t, k.Open(ctx))

	t.Logf("Kusto endpoint: %s", k.Endpoint())
	require.NoError(t, ingestor.RunWithKustoEndpoint(ctx, c))

	require.NoError(t, k.Close())
	require.NoError(t, c.Close())
}
