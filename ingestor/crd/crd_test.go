//go:build !disableDocker

package crd_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	v1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/ingestor/crd"
	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestCRD(t *testing.T) {
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

	restConfig, _, err := testutils.GetKubeConfig(ctx, k3sContainer)
	require.NoError(t, err)

	k8sClient, err := client.New(restConfig, client.Options{})
	require.NoError(t, err)

	testHandler := newHandler(t, k8sClient)

	opts := crd.Options{
		RestConfig:    restConfig,
		Handler:       testHandler,
		PollFrequency: time.Second,
	}
	c := crd.New(opts)
	require.NoError(t, c.Open(ctx))

	t.Run("cache hydration", func(t *testing.T) {
		testHandler.VerifyHydrate()
	})

	t.Run("add", func(t *testing.T) {
		fn := &v1.Function{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "testadd",
				Namespace: "default",
			},
			Spec: v1.FunctionSpec{
				Body:     "some-function-body",
				Database: "some-database",
			},
		}
		testHandler.CreateAndVerify(ctx, fn)
	})

	t.Run("update", func(t *testing.T) {
		a := &v1.Function{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "testupdate",
				Namespace: "default",
			},
			Spec: v1.FunctionSpec{
				Body:     "some-function-body",
				Database: "some-database",
			},
		}
		b := &v1.Function{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "testupdate",
				Namespace: "default",
			},
			Spec: v1.FunctionSpec{
				Body:     "some-function-body-2",
				Database: "some-database",
			},
		}
		testHandler.UpdateAndVerify(ctx, a, b)
	})

	t.Run("delete", func(t *testing.T) {
		fn := &v1.Function{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "testdelete",
				Namespace: "default",
			},
			Spec: v1.FunctionSpec{
				Body:     "some-function-body",
				Database: "some-database",
			},
		}
		testHandler.DeleteAndVerify(ctx, fn)
	})

	require.NoError(t, c.Close())
}

type Handler struct {
	t      *testing.T
	client client.Client

	OnAddChan    chan interface{}
	OnUpdateChan chan interface{}
	OnDeleteChan chan interface{}
}

func newHandler(t *testing.T, client client.Client) *Handler {
	return &Handler{
		t:            t,
		client:       client,
		OnAddChan:    make(chan interface{}, 1),
		OnUpdateChan: make(chan interface{}, 1),
		OnDeleteChan: make(chan interface{}, 1),
	}
}

func (h *Handler) VerifyHydrate() {
	h.t.Helper()

	obj := <-h.OnAddChan
	_, ok := obj.(*v1.Function)
	require.True(h.t, ok)
}

func (h *Handler) CreateAndVerify(ctx context.Context, fn *v1.Function) {
	h.t.Helper()

	require.NoError(h.t, h.client.Create(ctx, fn))

	obj := <-h.OnAddChan
	rfn, ok := obj.(*v1.Function)
	require.True(h.t, ok)
	require.Equal(h.t, fn.GetName(), rfn.GetName())
	require.Equal(h.t, fn.GetNamespace(), rfn.GetNamespace())
}

func (h *Handler) OnAdd(obj interface{}, isInInitialList bool) {
	h.OnAddChan <- obj
}

func (h *Handler) UpdateAndVerify(ctx context.Context, old, new *v1.Function) {
	h.t.Helper()

	h.CreateAndVerify(ctx, old)
	require.NoError(h.t, h.client.Get(ctx, client.ObjectKeyFromObject(old), old))
	new.SetGroupVersionKind(old.GroupVersionKind())
	new.SetResourceVersion(old.GetResourceVersion())

	require.NoError(h.t, h.client.Update(ctx, new))
	obj := <-h.OnUpdateChan
	fn, ok := obj.(*v1.Function)
	require.True(h.t, ok)
	require.Equal(h.t, new.GetName(), fn.GetName())
	require.Equal(h.t, new.GetNamespace(), fn.GetNamespace())
	require.Equal(h.t, int64(2), fn.GetGeneration())
}

func (h *Handler) OnUpdate(old, new interface{}) {
	oldFn := old.(*v1.Function)
	newFn := new.(*v1.Function)
	if oldFn.GetGeneration() == newFn.GetGeneration() {
		return
	}

	h.OnUpdateChan <- new
}

func (h *Handler) DeleteAndVerify(ctx context.Context, fn *v1.Function) {
	h.t.Helper()

	h.CreateAndVerify(ctx, fn)
	require.NoError(h.t, h.client.Delete(ctx, fn))

	obj := <-h.OnDeleteChan
	rfn, ok := obj.(*v1.Function)
	require.True(h.t, ok)
	require.Equal(h.t, fn.GetName(), rfn.GetName())
	require.Equal(h.t, fn.GetNamespace(), rfn.GetNamespace())
}

func (h *Handler) OnDelete(obj interface{}) {
	h.OnDeleteChan <- obj
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
