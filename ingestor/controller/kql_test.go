package controller_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Azure/adx-mon/ingestor/controller"
	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/Azure/adx-mon/pkg/testutils/kustainer"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
)

func TestKQLController(t *testing.T) {
	// Scaffold
	logf.SetLogger(logr.Logger{})

	crdPath := filepath.Join(t.TempDir(), "crd.yaml")
	require.NoError(t, testutils.CopyFile("../../kustomize/bases/functions_crd.yaml", crdPath))

	ctx := context.Background()
	k3sContainer, err := k3s.Run(ctx, "rancher/k3s:v1.31.2-k3s1")
	testcontainers.CleanupContainer(t, k3sContainer)
	require.NoError(t, err)
	require.NoError(t, k3sContainer.CopyFileToContainer(ctx, crdPath, filepath.Join(testutils.K3sManifests, "crd.yaml"), 0644))

	restConfig, k8sClient, err := testutils.GetKubeConfig(ctx, k3sContainer)
	require.NoError(t, err)

	kustoContainer, err := kustainer.Run(ctx, "mcr.microsoft.com/azuredataexplorer/kustainer-linux:latest", kustainer.WithCluster(ctx, k3sContainer))
	testcontainers.CleanupContainer(t, kustoContainer)
	require.NoError(t, err)
	require.NoError(t, kustoContainer.PortForward(ctx, restConfig))

	cb := kusto.NewConnectionStringBuilder(kustoContainer.ConnectionUrl())
	kustoClient, err := kusto.New(cb)
	require.NoError(t, err)
	defer kustoClient.Close()

	controllerReconciler := &controller.FunctionReconciler{
		Client:   k8sClient,
		Scheme:   k8sClient.Scheme(),
		KustoCli: kustoClient,
	}

	// Create a function
	resourceName := "testtest"
	typeNamespacedName := types.NamespacedName{
		Name:      resourceName,
		Namespace: "default",
	}

	fn := &adxmonv1.Function{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: "default",
		},
		Spec: adxmonv1.FunctionSpec{
			Body:     fmt.Sprintf(".create-or-alter function %s () {print now();}", resourceName),
			Database: "NetDefaultDB",
		},
	}
	require.NoError(t, k8sClient.Create(ctx, fn))

	// Test reconciliation of a new function
	_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: typeNamespacedName,
	})
	require.NoError(t, err)

	resource := &adxmonv1.Function{}
	require.NoError(t, k8sClient.Get(ctx, typeNamespacedName, resource))
	require.Equal(t, resource.Status.Status, adxmonv1.Success)
	require.Equal(t, resource.Status.ObservedGeneration, resource.Generation)
	require.NotZero(t, resource.Status.LastTimeReconciled)
	require.NotNil(t, resource.GetFinalizers())

	require.Eventually(t, func() bool {
		return testutils.FunctionExists(ctx, t, resource.Spec.Database, resourceName, kustoClient.Endpoint())
	}, time.Minute, time.Second)

	// Update function
	resource.Spec.Body = fmt.Sprintf(".create-or-alter function %s () {print 'Update';}", resourceName)
	require.NoError(t, k8sClient.Update(ctx, resource))

	// Test reconciliation of updated function
	_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: typeNamespacedName,
	})
	require.NoError(t, err)

	require.NoError(t, k8sClient.Get(ctx, typeNamespacedName, resource))
	require.Equal(t, resource.Status.Status, adxmonv1.Success)
	require.Equal(t, fn.GetGeneration()+1, resource.Status.ObservedGeneration)
	require.Eventually(t, func() bool {
		fn := testutils.GetFunction(ctx, t, resource.Spec.Database, resourceName, kustoClient.Endpoint())
		return strings.Contains(fn.Body, "Update")
	}, time.Minute, time.Second)

	// Test for idempotency
	currentGeneration := resource.Status.ObservedGeneration
	_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: typeNamespacedName,
	})
	require.NoError(t, err)

	require.NoError(t, k8sClient.Get(ctx, typeNamespacedName, resource))
	require.Equal(t, resource.Status.Status, adxmonv1.Success)
	require.Equal(t, currentGeneration, resource.Status.ObservedGeneration)

	// Test retries by overwriting the existing kusto client with one that has an invalid url

	// - Close the connection
	invalidKustoClient, err := kusto.New(kusto.NewConnectionStringBuilder("http://invalid-url"))
	require.NoError(t, err)
	defer invalidKustoClient.Close()
	controllerReconciler.KustoCli = invalidKustoClient

	// - Update the function again, which will cause a reconcile attempt
	resource.Spec.Body = fmt.Sprintf(".create-or-alter function %s () {print 'Update 2';}", resourceName)
	require.NoError(t, k8sClient.Update(ctx, resource))

	_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: typeNamespacedName,
	})
	require.NoError(t, err)

	// - Verify the reconcile failed
	require.NoError(t, k8sClient.Get(ctx, typeNamespacedName, resource))
	require.Equal(t, resource.Status.Status, adxmonv1.Failed)

	// - Give reconcile a valid kusto client
	controllerReconciler.KustoCli = kustoClient
	_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: typeNamespacedName,
	})
	require.NoError(t, err)

	// - Wait for a successful reconciliation
	require.Eventually(t, func() bool {
		fn := testutils.GetFunction(ctx, t, resource.Spec.Database, resourceName, kustoClient.Endpoint())
		return strings.Contains(fn.Body, "Update 2")
	}, time.Minute, time.Second)

	// - Verify the reconcile succeeded
	require.NoError(t, k8sClient.Get(ctx, typeNamespacedName, resource))
	require.Equal(t, resource.Status.Status, adxmonv1.Success)

	// Delete function
	require.NoError(t, k8sClient.Delete(ctx, resource))

	// Test reconciliation of deleted function
	_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: typeNamespacedName,
	})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return !testutils.FunctionExists(ctx, t, resource.Spec.Database, resourceName, kustoClient.Endpoint())
	}, time.Minute, time.Second)
}
