package storage_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	v1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/ingestor/adx"
	"github.com/Azure/adx-mon/ingestor/storage"
	"github.com/Azure/adx-mon/pkg/crd"
	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/Azure/adx-mon/pkg/testutils/kustainer"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func TestFunctions(t *testing.T) {
	// Setup our test environment, which consists of
	// a Kubernetes and Kusto cluster.
	ctx := context.Background()
	k3sContainer, err := k3s.Run(ctx, "rancher/k3s:v1.31.2-k3s1")
	testcontainers.CleanupContainer(t, k3sContainer)
	require.NoError(t, err)

	kustoContainer, err := kustainer.Run(ctx, "mcr.microsoft.com/azuredataexplorer/kustainer-linux:latest", kustainer.WithCluster(ctx, k3sContainer))
	testcontainers.CleanupContainer(t, kustoContainer)
	require.NoError(t, err)

	// Install our CRD definition and create a Function
	crdPath := filepath.Join(t.TempDir(), "crd.yaml")
	require.NoError(t, testutils.CopyFile("../../kustomize/bases/functions_crd.yaml", crdPath))
	require.NoError(t, k3sContainer.CopyFileToContainer(ctx, crdPath, filepath.Join(testutils.K3sManifests, "crd.yaml"), 0644))

	restConfig, ctrlCli, err := testutils.GetKubeConfig(ctx, k3sContainer)
	require.NoError(t, err)
	require.NoError(t, kustoContainer.PortForward(ctx, restConfig))

	require.NoError(t, ctrlCli.Create(ctx, &crdFn))

	cb := kusto.NewConnectionStringBuilder(kustoContainer.ConnectionUrl())
	client, err := kusto.New(cb)
	require.NoError(t, err)
	defer client.Close()

	// Now we instantiate our CRD operator and task.
	// - functionStore knows how to handle Functions and distinguish them from Views.
	// - kqlFnOperator knows how to list CRD/Functions from Kubernetes and send them to functionStore.
	// - task knows how to sync Functions from Kubernetes to Kusto.
	functionStore := storage.NewFunctions(ctrlCli)
	kqlCRDOpts := crd.Options{
		CtrlCli:       ctrlCli,
		List:          &v1.FunctionList{},
		Store:         functionStore,
		PollFrequency: time.Second,
	}
	kqlFnOperator := crd.New(kqlCRDOpts)
	require.NoError(t, kqlFnOperator.Open(ctx))
	task := adx.NewSyncFunctionsTask(functionStore, &kustoExecutor{kcli: client})

	// Our first test is to ensure that CRD/Functions can be installed
	// into a k8s cluster and we interact with them using a k8s client.
	t.Run("Get CRD/Functions from k8s", func(t *testing.T) {
		require.Eventually(t, func() bool {
			list := &v1.FunctionList{}
			err := ctrlCli.List(ctx, list)
			if err == nil && len(list.Items) > 0 {
				return true
			}
			return false
		}, time.Minute, time.Second)
	})

	// Our second test ensures that our mechanisms for reacting to CRD/Functions
	// in k8s can correctly recongize and store them.
	var installedFunction *v1.Function
	t.Run("Ensure CRD/Functions flow through our constructs", func(t *testing.T) {
		require.Eventually(t, func() bool {
			obs, err := kqlFnOperator.List(ctx)
			require.NoError(t, err)
			require.NoError(t, functionStore.Receive(ctx, obs))

			installedFunctions := functionStore.List()
			if len(installedFunctions) == 1 {
				installedFunction = installedFunctions[0]
				return true
			}
			return false
		}, time.Minute, time.Second)
	})

	var (
		database    = "NetDefaultDB"
		kustoFnName = crdFn.GetName()
		// Note that a CRD/Function name does not have to match the function name in Kusto; however,
		// until we can parse the function's name from the CRD/Function's Body, it's not possible
		// for us to delete the function from Kusto if the CRD/Function's name does not match
		// the Function's name in Kusto.
	)

	// Our third test ensures that our task can install the function into Kusto.
	t.Run("Function installs into Kusto", func(t *testing.T) {
		require.Eventually(t, func() bool {
			require.NoError(t, task.Run(ctx))
			return testutils.FunctionExists(ctx, t, database, kustoFnName, kustoContainer.ConnectionUrl())
		}, time.Minute, time.Second)
	})

	// Our fourth test ensures that our task can update the function in Kusto.
	t.Run("Function updates in Kusto", func(t *testing.T) {
		// Update the CRD/Function in k8s
		ctrlCli.Get(ctx, runtimeclient.ObjectKeyFromObject(&crdFn), &crdFn)
		crdFn.Spec.Body = `.create-or-alter function testfn() { print now(1m); }`
		require.NoError(t, ctrlCli.Update(ctx, &crdFn))

		// Wait for the state to be relfected in Kusto, forcing the state update as we wait.
		require.Eventually(t, func() bool {
			require.NoError(t, task.Run(ctx))
			if !testutils.FunctionExists(ctx, t, database, kustoFnName, kustoContainer.ConnectionUrl()) {
				return false
			}

			fn := testutils.GetFunction(ctx, t, database, kustoFnName, kustoContainer.ConnectionUrl())
			return strings.Contains(fn.Body, "now(1m)")
		}, time.Minute, time.Second)
	})

	// Lastly, we ensure that if an operator deletes a CRD/Function from k8s,
	// that the task will delete the function from Kusto.
	t.Run("Function is deleted from Kusto", func(t *testing.T) {
		gracePeriod := int64(0)
		require.NoError(t, ctrlCli.Delete(ctx, installedFunction, &runtimeclient.DeleteOptions{GracePeriodSeconds: &gracePeriod})) // Delete the object from k8s

		require.Eventually(t, func() bool {
			require.NoError(t, task.Run(ctx))                                                                       // Process the new state, which is being synced in the background by the CRD operator.
			return testutils.FunctionExists(ctx, t, database, kustoFnName, kustoContainer.ConnectionUrl()) == false // Ensure the function is deleted from Kusto.
		}, time.Minute, time.Second)
	})

	require.NoError(t, kqlFnOperator.Close())
}

type kustoExecutor struct {
	kcli *kusto.Client
}

func (k *kustoExecutor) Database() string {
	return "NetDefaultDB"
}

func (k *kustoExecutor) Mgmt(ctx context.Context, stmt kusto.Statement, opts ...kusto.QueryOption) (*kusto.RowIterator, error) {
	return k.kcli.Mgmt(ctx, k.Database(), stmt, opts...)
}

var crdFn = v1.Function{
	TypeMeta: metav1.TypeMeta{
		APIVersion: "adx-mon.azure.com/v1",
		Kind:       "Function",
	},
	ObjectMeta: metav1.ObjectMeta{
		Name:      "testfn",
		Namespace: "default",
	},
	Spec: v1.FunctionSpec{
		Body:     `.create-or-alter function testfn() { print now(); }`,
		Database: "NetDefaultDB",
	},
}
