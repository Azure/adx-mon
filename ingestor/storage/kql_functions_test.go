package storage_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/ingestor/storage"
)

func TestFunctions(t *testing.T) {
	testutils.IntegrationTest(t)

	scheme := clientgoscheme.Scheme
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, adxmonv1.AddToScheme(scheme))

	crdPath := filepath.Join(t.TempDir(), "crd.yaml")
	require.NoError(t, testutils.CopyFile("../../kustomize/bases/functions_crd.yaml", crdPath))

	ctx := context.Background()
	k3sContainer, err := k3s.Run(ctx, "rancher/k3s:v1.31.2-k3s1")
	testcontainers.CleanupContainer(t, k3sContainer)
	require.NoError(t, err)

	require.NoError(t, k3sContainer.CopyFileToContainer(ctx, crdPath, filepath.Join(testutils.K3sManifests, "crd.yaml"), 0644))

	kubeconfig, err := testutils.WriteKubeConfig(ctx, k3sContainer, t.TempDir())
	require.NoError(t, err)

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	require.NoError(t, err)
	config.WarningHandler = rest.NoWarnings{}
	ctrlCli, err := ctrlclient.New(config, ctrlclient.Options{})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		crd := &adxmonv1.Function{}
		err := ctrlCli.Get(ctx, types.NamespacedName{Namespace: "default"}, crd)
		return !errors.Is(err, &meta.NoKindMatchError{}) && !errors.Is(err, &meta.NoResourceMatchError{})
	}, time.Minute, time.Second)

	functionStore := storage.NewFunctions(ctrlCli, nil)

	resourceName := "testtest"
	typeNamespacedName := types.NamespacedName{
		Name:      resourceName,
		Namespace: "default",
	}

	t.Run("Install function", func(t *testing.T) {
		fn := &adxmonv1.Function{
			ObjectMeta: metav1.ObjectMeta{
				Name:      typeNamespacedName.Name,
				Namespace: typeNamespacedName.Namespace,
			},
			Spec: adxmonv1.FunctionSpec{
				Body:     "some-function-body",
				Database: "some-database",
			},
		}
		require.NoError(t, ctrlCli.Create(ctx, fn))
	})

	t.Run("List Functions", func(t *testing.T) {
		fns, err := functionStore.List(ctx)
		require.NoError(t, err)
		require.Len(t, fns, 1)
		require.Equal(t, int64(1), fns[0].GetGeneration())
		require.Equal(t, int64(0), fns[0].Status.ObservedGeneration)
	})

	t.Run("Does not included suspended", func(t *testing.T) {
		fn := &adxmonv1.Function{}
		require.NoError(t, ctrlCli.Get(ctx, typeNamespacedName, fn))

		fn.Spec.Suspend = new(bool)
		*fn.Spec.Suspend = true
		require.NoError(t, ctrlCli.Update(ctx, fn))

		fns, err := functionStore.List(ctx)
		require.NoError(t, err)
		require.Empty(t, fns)

		require.NoError(t, ctrlCli.Get(ctx, typeNamespacedName, fn))
		*fn.Spec.Suspend = false
		require.NoError(t, ctrlCli.Update(ctx, fn))
	})

	t.Run("Updates status", func(t *testing.T) {
		fn := &adxmonv1.Function{}
		require.NoError(t, ctrlCli.Get(ctx, typeNamespacedName, fn))

		fn.Status.Status = adxmonv1.Success
		require.NoError(t, functionStore.UpdateStatus(ctx, fn))

		fn = &adxmonv1.Function{}
		require.NoError(t, ctrlCli.Get(ctx, typeNamespacedName, fn))
		require.Equal(t, fn.Status.Status, adxmonv1.Success)
		require.Equal(t, fn.Status.ObservedGeneration, fn.GetGeneration())
		require.False(t, fn.Status.LastTimeReconciled.IsZero())

		fns, err := functionStore.List(ctx)
		require.NoError(t, err)
		require.Empty(t, fns) // because the generation is up to date
	})

	t.Run("Does not update generation upon failure", func(t *testing.T) {
		fn := &adxmonv1.Function{}
		require.NoError(t, ctrlCli.Get(ctx, typeNamespacedName, fn))

		currentGeneration := fn.GetGeneration()
		fn.Status.Status = adxmonv1.Failed
		require.NoError(t, functionStore.UpdateStatus(ctx, fn))

		fn = &adxmonv1.Function{}
		require.NoError(t, ctrlCli.Get(ctx, typeNamespacedName, fn))
		require.Equal(t, fn.Status.Status, adxmonv1.Failed)
		require.Equal(t, currentGeneration, fn.GetGeneration())
	})

	t.Run("Respects leader election", func(t *testing.T) {
		coord := &elector{isLeader: true}
		functionStore := storage.NewFunctions(ctrlCli, coord)

		// Update the function to ensure the generation is updated
		fn := &adxmonv1.Function{}
		require.NoError(t, ctrlCli.Get(ctx, typeNamespacedName, fn))
		fn.Spec.Database = "some-other-database"
		require.NoError(t, ctrlCli.Update(ctx, fn))

		fns, err := functionStore.List(ctx)
		require.NoError(t, err)
		require.Len(t, fns, 1)

		coord.isLeader = false
		fns, err = functionStore.List(ctx)
		require.NoError(t, err)
		require.Empty(t, fns)
	})

	t.Run("Can delete function", func(t *testing.T) {
		fn := &adxmonv1.Function{}
		require.NoError(t, ctrlCli.Get(ctx, typeNamespacedName, fn))

		require.True(t, controllerutil.ContainsFinalizer(fn, storage.FinalizerName))
		require.NoError(t, ctrlCli.Delete(ctx, fn))

		fns, err := functionStore.List(ctx)
		require.NoError(t, err)
		require.Len(t, fns, 1)

		fns[0].Status.Status = adxmonv1.Success
		require.NoError(t, functionStore.UpdateStatus(ctx, fns[0]))

		fns, err = functionStore.List(ctx)
		require.NoError(t, err)
		require.Empty(t, fns)
	})
}

func TestReportFunctions(t *testing.T) {
	functions := []*adxmonv1.Function{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "new-fn",
				Namespace: "default",
			},
			Status: adxmonv1.FunctionStatus{
				Reason: "Function created",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "updated-fn",
				Namespace:  "default",
				Generation: 2,
			},
			Status: adxmonv1.FunctionStatus{
				ObservedGeneration: 1,
				Status:             adxmonv1.Success,
				Reason:             "Function updated",
			},
		},
	}
	buf := new(bytes.Buffer)
	logger := slog.New(slog.NewJSONHandler(buf, nil))
	slog.SetDefault(logger)

	storage.ReportFunctions(functions)
	t.Logf(buf.String())

	var logMsg FunctionLog
	require.NoError(t, json.Unmarshal(buf.Bytes(), &logMsg))
	require.Equal(t, "new-fn", logMsg.NewFn.Name)
	require.Equal(t, "default", logMsg.NewFn.Namespace)
	require.Equal(t, "Function created", logMsg.NewFn.Reason)
	require.Equal(t, "updated-fn", logMsg.UpdatedFn.Name)
	require.Equal(t, "default", logMsg.UpdatedFn.Namespace)
	require.Equal(t, "Function updated", logMsg.UpdatedFn.Reason)
}

type elector struct {
	isLeader bool
}

func (e *elector) IsLeader() bool {
	return e.isLeader
}

type FunctionLog struct {
	Time      string   `json:"time"`
	Level     string   `json:"level"`
	Msg       string   `json:"msg"`
	NewFn     Function `json:"new-fn"`
	UpdatedFn Function `json:"updated-fn"`
}

type Function struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Status    string `json:"status"`
	Reason    string `json:"reason"`
}
