//go:build !disableDocker

package adx

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	v1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/ingestor/crd"
	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/Azure/adx-mon/pkg/testutils/kustainer"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/kql"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestKQLHandler(t *testing.T) {
	crdPath := filepath.Join(t.TempDir(), "crd.yaml")
	require.NoError(t, testutils.CopyFile("../../kustomize/bases/functions_crd.yaml", crdPath))

	ctx := context.Background()
	k3sContainer, err := k3s.Run(ctx, "rancher/k3s:v1.31.2-k3s1")
	testcontainers.CleanupContainer(t, k3sContainer)
	require.NoError(t, err)
	require.NoError(t, k3sContainer.CopyFileToContainer(ctx, crdPath, filepath.Join(testutils.K3sManifests, "crd.yaml"), 0644))

	kustoContainer, err := kustainer.Run(ctx, "mcr.microsoft.com/azuredataexplorer/kustainer-linux:latest", kustainer.WithCluster(ctx, k3sContainer))
	testcontainers.CleanupContainer(t, kustoContainer)
	require.NoError(t, err)

	restConfig, _, err := testutils.GetKubeConfig(ctx, k3sContainer)
	require.NoError(t, err)
	require.NoError(t, kustoContainer.PortForward(ctx, restConfig))

	cb := kusto.NewConnectionStringBuilder(kustoContainer.ConnectionUrl())
	kustoClient, err := kusto.New(cb)
	require.NoError(t, err)
	defer kustoClient.Close()

	k8sClient, err := client.New(restConfig, client.Options{})
	require.NoError(t, err)

	databaseName := "NetDefaultDB"
	handler := NewKQLHandler(KQLHandlerOpts{
		KustoCli:      kustoClient,
		Database:      databaseName,
		RetryInterval: time.Second,
		MaxRetry:      3,
	})
	require.NoError(t, handler.Open(ctx))
	defer handler.Close()

	opts := crd.Options{
		RestConfig:    restConfig,
		Handler:       handler,
		PollFrequency: time.Second,
	}
	c := crd.New(opts)
	require.NoError(t, c.Open(ctx))
	defer c.Close()

	t.Run("add", func(t *testing.T) {
		fnName := "testadd"
		fn := &v1.Function{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fnName,
				Namespace: "default",
			},
			Spec: v1.FunctionSpec{
				Body:     fmt.Sprintf(".create-or-alter function %s () {print now();}", fnName),
				Database: databaseName,
			},
		}
		require.NoError(t, k8sClient.Create(ctx, fn))
		require.Eventually(t, func() bool {
			return testutils.FunctionExists(ctx, t, databaseName, fnName, kustoContainer.ConnectionUrl())
		}, time.Minute, time.Second)
	})

	t.Run("update", func(t *testing.T) {
		fnName := "testupdate"
		fn := &v1.Function{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fnName,
				Namespace: "default",
			},
			Spec: v1.FunctionSpec{
				Body:     fmt.Sprintf(".create-or-alter function %s () {print now();}", fnName),
				Database: databaseName,
			},
		}
		require.NoError(t, k8sClient.Create(ctx, fn))
		require.Eventually(t, func() bool {
			return testutils.FunctionExists(ctx, t, databaseName, fnName, kustoContainer.ConnectionUrl())
		}, time.Minute, time.Second)

		require.NoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(fn), fn))
		fn.Spec.Body = fmt.Sprintf(".create-or-alter function %s () {print 'Updated';}", fnName)
		require.NoError(t, k8sClient.Update(ctx, fn))

		require.Eventually(t, func() bool {
			fn := testutils.GetFunction(ctx, t, databaseName, fnName, kustoContainer.ConnectionUrl())
			return strings.Contains(fn.Body, "Updated")
		}, time.Minute, time.Second)
	})

	t.Run("delete", func(t *testing.T) {
		fnName := "testdelete"
		fn := &v1.Function{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fnName,
				Namespace: "default",
			},
			Spec: v1.FunctionSpec{
				Body:     fmt.Sprintf(".create-or-alter function %s () {print now();}", fnName),
				Database: databaseName,
			},
		}
		require.NoError(t, k8sClient.Create(ctx, fn))
		require.Eventually(t, func() bool {
			return testutils.FunctionExists(ctx, t, databaseName, fnName, kustoContainer.ConnectionUrl())
		}, time.Minute, time.Second)

		require.NoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(fn), fn))
		require.NoError(t, k8sClient.Delete(ctx, fn))

		require.Eventually(t, func() bool {
			return !testutils.FunctionExists(ctx, t, databaseName, fnName, kustoContainer.ConnectionUrl())
		}, time.Minute, time.Second)
	})

	t.Run("retry", func(t *testing.T) {
		// To test retries, we'll create a function that references a
		// table that doesn't yet exist. We'll then create the table
		// and poll until the function is able to be created via the
		// retry queue.

		// Create the CRD. The associated function will reference a table
		// that doesn't yet exist, so the function will fail to create.
		fnName := "testretry"
		tableName := "testtable"
		fn := &v1.Function{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fnName,
				Namespace: "default",
			},
			Spec: v1.FunctionSpec{
				Body:     fmt.Sprintf(".create-or-alter function %s () {%s}", fnName, tableName),
				Database: databaseName,
			},
		}
		require.NoError(t, k8sClient.Create(ctx, fn))

		// Wait for our function to be added to the retry queue.
		require.Eventually(t, func() bool {
			handler.mu.Lock()
			defer handler.mu.Unlock()

			return len(handler.retryQueue) > 0
		}, time.Minute, time.Second)

		// Now create the table that our function references
		stmt := kql.New(".create-merge table ").AddUnsafe(tableName).AddLiteral(" (x:int)")
		_, err := kustoClient.Mgmt(ctx, databaseName, stmt)
		require.NoError(t, err)

		// Wait for the function to be successfully created
		require.Eventually(t, func() bool {
			return testutils.FunctionExists(ctx, t, databaseName, fnName, kustoContainer.ConnectionUrl())
		}, time.Minute, time.Second)
	})
}
