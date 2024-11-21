package adx

import (
	"context"
	"testing"

	v1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/ingestor/storage"
	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/Azure/adx-mon/pkg/testutils/kustainer"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

func TestSyncFunctions(t *testing.T) {
	scheme := clientgoscheme.Scheme
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, v1.AddToScheme(scheme))

	ctx := context.Background()
	kustoContainer, err := kustainer.Run(ctx, "mcr.microsoft.com/azuredataexplorer/kustainer-linux:latest", kustainer.WithStarted())
	testcontainers.CleanupContainer(t, kustoContainer)
	require.NoError(t, err)

	cb := kusto.NewConnectionStringBuilder("http://localhost:8080")
	client, err := kusto.New(cb)
	require.NoError(t, err)
	defer client.Close()

	k3sContainer, err := k3s.Run(ctx, "rancher/k3s:v1.31.2-k3s1")
	testcontainers.CleanupContainer(t, k3sContainer)
	require.NoError(t, err)

	require.NoError(t, testutils.InstallFunctionsCRD(ctx, k3sContainer, t.TempDir()))
	ctrlCli, err := testutils.GetKubeConfig(ctx, k3sContainer, t.TempDir())
	require.NoError(t, err)

	functionStore := storage.NewFunctions(ctrlCli)
	syncTask := NewSyncFunctionsTask(functionStore, client)

	// TODO
	// x Start a k3s cluster, no need to inject kustainer into the cluster
	// x Install CRD spec into the cluster
	// x Create a storage.function object
	// - Create a SyncFunctions task and run it with our storage.function object
	// - Install a function CRD into the cluster
	// - Verify that the function object was created in the kusto cluster
	// - Delete the function CRD
	// - Verify the function object was deleted from the kusto cluster
}
