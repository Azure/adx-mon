package storage_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	v1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/ingestor/storage"
	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func TestFunctions(t *testing.T) {
	testutils.IntegrationTest(t)

	scheme := clientgoscheme.Scheme
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, v1.AddToScheme(scheme))

	crdPath := filepath.Join(t.TempDir(), "crd.yaml")
	require.NoError(t, testutils.CopyFile("../../kustomize/bases/functions_crd.yaml", crdPath))
	fnCrdPath := filepath.Join(t.TempDir(), "fn-crd.yaml")
	os.WriteFile(fnCrdPath, []byte(crd), 0644)

	ctx := context.Background()
	k3sContainer, err := k3s.Run(ctx, "rancher/k3s:v1.31.2-k3s1")
	testcontainers.CleanupContainer(t, k3sContainer)
	require.NoError(t, err)

	require.NoError(t, k3sContainer.CopyFileToContainer(ctx, crdPath, filepath.Join(testutils.K3sManifests, "crd.yaml"), 0644))
	require.NoError(t, k3sContainer.CopyFileToContainer(ctx, fnCrdPath, filepath.Join(testutils.K3sManifests, "fn-crd.yaml"), 0644))

	kubeconfig, err := testutils.WriteKubeConfig(ctx, k3sContainer, t.TempDir())
	require.NoError(t, err)

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	require.NoError(t, err)
	config.WarningHandler = rest.NoWarnings{}
	ctrlCli, err := ctrlclient.New(config, ctrlclient.Options{})
	require.NoError(t, err)

	functionStore := storage.NewFunctions(ctrlCli)
	list := &v1.FunctionList{}

	t.Run("Get Functions", func(t *testing.T) {
		require.Eventually(t, func() bool {
			err := ctrlCli.List(ctx, list)
			return err == nil && len(list.Items) > 0
		}, time.Minute, time.Second)
	})

	t.Run("Function is installed", func(t *testing.T) {
		require.Eventually(t, func() bool {
			require.NoError(t, functionStore.Receive(ctx, list))
			return len(functionStore.List()) == 1
		}, time.Minute, time.Second)
	})
}

var crd = `---
apiVersion: adx-mon.azure.com/v1
kind: Function
metadata:
  name: some-crd
spec:
  body: some-function-body
  database: some-database
---`
