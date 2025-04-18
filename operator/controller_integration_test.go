package operator

import (
	"context"
	"testing"
	"time"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/Azure/adx-mon/pkg/testutils/kustainer"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func TestOperatorCRDPhases(t *testing.T) {
	testutils.IntegrationTest(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	kustoUrl, k3sContainer := StartTestEnv(t)

	// Set the controller-runtime logger to use t.Log via testutils.NewTestLogger
	log.SetLogger(testutils.NewTestLogger(t))

	// Install CRDs
	require.NoError(t, testutils.InstallCrds(ctx, k3sContainer))

	// Build scheme
	scheme := clientgoscheme.Scheme
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, adxmonv1.AddToScheme(scheme))

	// Get kubeconfig and create manager
	restConfig, _, err := testutils.GetKubeConfig(ctx, k3sContainer)
	require.NoError(t, err)
	mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	require.NoError(t, err)

	// Set up reconciler
	r := &Reconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}
	require.NoError(t, r.SetupWithManager(mgr))

	// Start manager in background
	done := make(chan struct{})
	go func() {
		defer close(done)
		require.NoError(t, mgr.Start(ctx))
	}()
	// Give the manager a moment to start
	t.Cleanup(func() {
		cancel() // Stop the manager
		<-done   // Wait for the goroutine to finish
	})
	time.Sleep(2 * time.Second)

	// Create a minimal Operator CRD
	operator := &adxmonv1.Operator{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-operator",
			Namespace: "default",
		},
	}

	ctrlClient := mgr.GetClient()
	require.NoError(t, ctrlClient.Create(ctx, operator))

	// Wait for the controller to set the initial phase condition
	var fetched adxmonv1.Operator
	require.Eventually(t, func() bool {
		err := ctrlClient.Get(ctx, types.NamespacedName{
			Name:      operator.Name,
			Namespace: operator.Namespace,
		}, &fetched)
		if err != nil {
			return false
		}
		condition := meta.FindStatusCondition(fetched.Status.Conditions, adxmonv1.ADXClusterConditionOwner)
		return condition != nil
	}, 10*time.Minute, time.Second, "Operator phase should be EnsureKusto")

	// Now we'll update the CRD to contain a completed Kusto cluster and ensure the next phase is triggered
	require.NoError(t, ctrlClient.Get(ctx, types.NamespacedName{
		Name:      operator.Name,
		Namespace: operator.Namespace,
	}, &fetched),
	)
	fetched.Spec.ADX = &adxmonv1.ADXConfig{
		Clusters: []adxmonv1.ADXClusterSpec{
			{
				Name:     "test-cluster",
				Endpoint: kustoUrl,
				Databases: []adxmonv1.ADXDatabaseSpec{
					{
						Name:          "Metrics",
						TelemetryType: adxmonv1.DatabaseTelemetryMetrics,
					},
					{
						Name:          "Logs",
						TelemetryType: adxmonv1.DatabaseTelemetryLogs,
					},
				},
			},
		},
	}
	require.NoError(t, ctrlClient.Update(ctx, &fetched))

	// Wait for the controller to set the next phase condition
	require.Eventually(t, func() bool {
		err := ctrlClient.Get(ctx, types.NamespacedName{
			Name:      operator.Name,
			Namespace: operator.Namespace,
		}, &fetched)
		if err != nil {
			return false
		}
		condition := meta.FindStatusCondition(fetched.Status.Conditions, adxmonv1.ADXClusterConditionOwner)
		if condition == nil {
			return false
		}
		if condition.Status != metav1.ConditionTrue {
			return false
		}

		// Now ensure the next phase is set
		condition = meta.FindStatusCondition(fetched.Status.Conditions, adxmonv1.IngestorClusterConditionOwner)
		if condition == nil {
			return false
		}
		return condition.Status == metav1.ConditionTrue
	}, 10*time.Minute, time.Second, "Operator phase for Kusto should be complete")

	// Optionally, simulate phase progression by updating status and checking next phase
	// ...future test logic for phase progression...
}

func StartTestEnv(t *testing.T) (kustoUrl string, k3sContainer *k3s.K3sContainer) {
	t.Helper()

	ctx := context.Background()
	var err error
	k3sContainer, err = k3s.Run(ctx, "rancher/k3s:v1.31.2-k3s1")
	testcontainers.CleanupContainer(t, k3sContainer)
	require.NoError(t, err)

	kustoContainer, err := kustainer.Run(ctx, "mcr.microsoft.com/azuredataexplorer/kustainer-linux:latest", kustainer.WithStarted())
	testcontainers.CleanupContainer(t, kustoContainer)
	require.NoError(t, err)
	kustoUrl = kustoContainer.ConnectionUrl()

	opts := kustainer.IngestionBatchingPolicy{
		MaximumBatchingTimeSpan: 30 * time.Second,
	}
	for _, dbName := range []string{"Metrics", "Logs"} {
		require.NoError(t, kustoContainer.CreateDatabase(ctx, dbName))
		require.NoError(t, kustoContainer.SetIngestionBatchingPolicy(ctx, dbName, opts))
	}

	return
}
