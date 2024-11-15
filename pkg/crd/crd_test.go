package crd_test

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	v1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/crd"
	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type TestStore struct {
	t *testing.T

	received int32
}

func (s *TestStore) Receive(ctx context.Context, list client.ObjectList) error {
	s.t.Helper()
	require.NotNil(s.t, list)

	items, ok := list.(*v1.FunctionList)
	require.True(s.t, ok)
	if items == nil || len(items.Items) == 0 {
		return nil
	}

	atomic.AddInt32(&s.received, int32(len(items.Items)))
	return nil
}

func (s *TestStore) Count() int32 {
	return atomic.LoadInt32(&s.received)
}

func TestCRD(t *testing.T) {
	scheme := clientgoscheme.Scheme
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, v1.AddToScheme(scheme))

	crdPath := filepath.Join(t.TempDir(), "crd.yaml")
	require.NoError(t, testutils.CopyFile("../../kustomize/bases/functions_crd.yaml", crdPath))
	fnCrdPath := filepath.Join(t.TempDir(), "fn-crd.yaml")
	os.WriteFile(fnCrdPath, []byte(fnCrd), 0644)

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
	ctrlCli, err := ctrlclient.New(config, ctrlclient.Options{
		WarningHandler: ctrlclient.WarningHandlerOptions{
			SuppressWarnings: true,
		},
	})
	require.NoError(t, err)

	ts := &TestStore{t: t}
	opts := crd.Options{
		CtrlCli:       ctrlCli,
		List:          &v1.FunctionList{},
		Store:         ts,
		PollFrequency: 100 * time.Millisecond,
	}
	c := crd.New(opts)
	require.NoError(t, c.Open(ctx))

	require.Eventually(t, func() bool {
		return ts.Count() > 0
	}, time.Minute, time.Second)

	require.NoError(t, c.Close())
}

var fnCrd = `---
apiVersion: adx-mon.azure.com/v1
kind: Function
metadata:
  name: some-crd
spec:
  body: some-function-body
  database: some-database
---`
