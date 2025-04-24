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
	corev1 "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func TestOperatorCRDPhases(t *testing.T) {
	// testutils.IntegrationTest(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	k3sContainer := StartTestEnv(t)

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
	r := &Reconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		AdxCtor:   KustainerClusterCreator(t, k3sContainer),
		AdxUpdate: KustainerClusterCreator(t, k3sContainer),
		AdxRdy:    KustainerClusterReady(t),
	}
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
	// Wait for cache to sync
	require.True(t, mgr.GetCache().WaitForCacheSync(ctx), "failed to sync cache")

	// Create namespace first
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "adx-mon",
		},
	}

	// Create a minimal Operator CRD
	operator := &adxmonv1.Operator{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "operator",
			Namespace: "adx-mon",
		},
	}

	ctrlClient := mgr.GetClient()
	require.NoError(t, ctrlClient.Create(ctx, namespace))
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
		return meta.IsStatusConditionTrue(fetched.Status.Conditions, adxmonv1.InitConditionOwner)
	}, 10*time.Minute, time.Second, "Wait for initial phase condition")

	// Wait for ADX cluster to become ready
	require.Eventually(t, func() bool {
		err := ctrlClient.Get(ctx, types.NamespacedName{
			Name:      operator.Name,
			Namespace: operator.Namespace,
		}, &fetched)
		if err != nil {
			return false
		}
		return meta.IsStatusConditionTrue(fetched.Status.Conditions, adxmonv1.ADXClusterConditionOwner)
	}, 10*time.Minute, time.Second, "Wait for ADX cluster to be ready")

	// Wait for Ingestor to become ready
	require.Eventually(t, func() bool {
		err := ctrlClient.Get(ctx, types.NamespacedName{
			Name:      operator.Name,
			Namespace: operator.Namespace,
		}, &fetched)
		if err != nil {
			return false
		}
		return meta.IsStatusConditionTrue(fetched.Status.Conditions, adxmonv1.IngestorClusterConditionOwner)
	}, 10*time.Minute, time.Second, "Wait for Ingestor to be ready")

	// Wait for Collector to become ready
	require.Eventually(t, func() bool {
		err := ctrlClient.Get(ctx, types.NamespacedName{
			Name:      operator.Name,
			Namespace: operator.Namespace,
		}, &fetched)
		if err != nil {
			return false
		}
		return meta.IsStatusConditionTrue(fetched.Status.Conditions, adxmonv1.CollectorClusterConditionOwner)
	}, 10*time.Minute, time.Second, "Wait for Collector to be ready")
}

func StartTestEnv(t *testing.T) *k3s.K3sContainer {
	t.Helper()

	ctx := context.Background()
	k3sContainer, err := k3s.Run(ctx, "rancher/k3s:v1.31.2-k3s1")
	testcontainers.CleanupContainer(t, k3sContainer)
	require.NoError(t, err)

	kubeConfig, err := testutils.WriteKubeConfig(ctx, k3sContainer, t.TempDir())
	require.NoError(t, err)
	t.Logf("Kubeconfig: %s", kubeConfig)

	return k3sContainer
}

func KustainerClusterCreator(t *testing.T, k3sContainer *k3s.K3sContainer) AdxClusterCreator {
	t.Helper()

	return func(ctx context.Context, r *Reconciler, operator *adxmonv1.Operator) (ctrl.Result, error) {
		// Create a Kustainer cluster
		kustainerContainer, err := kustainer.Run(ctx, "mcr.microsoft.com/azuredataexplorer/kustainer-linux:latest", kustainer.WithCluster(ctx, k3sContainer))
		testcontainers.CleanupContainer(t, kustainerContainer)
		require.NoError(t, err)

		restConfig, _, err := testutils.GetKubeConfig(ctx, k3sContainer)
		require.NoError(t, err)
		require.NoError(t, kustainerContainer.PortForward(ctx, restConfig))

		opts := kustainer.IngestionBatchingPolicy{
			MaximumBatchingTimeSpan: 30 * time.Second,
		}
		for _, dbName := range []string{"Metrics", "Logs"} {
			require.NoError(t, kustainerContainer.CreateDatabase(ctx, dbName))
			require.NoError(t, kustainerContainer.SetIngestionBatchingPolicy(ctx, dbName, opts))
		}

		operator.Spec.ADX = &adxmonv1.ADXConfig{
			Clusters: []adxmonv1.ADXClusterSpec{
				{
					Name:     "test-cluster",
					Endpoint: "http://kustainer.default.svc.cluster.local:8080",
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
		require.NoError(t, r.Update(ctx, operator))

		c := metav1.Condition{
			Type:               adxmonv1.ADXClusterConditionOwner,
			Status:             metav1.ConditionUnknown,
			Reason:             string(adxmonv1.OperatorServiceReasonInstalling),
			LastTransitionTime: metav1.NewTime(time.Now()),
			ObservedGeneration: operator.GetGeneration(),
		}
		if meta.SetStatusCondition(&operator.Status.Conditions, c) {
			require.NoError(t, r.Status().Update(ctx, operator))
		}

		return ctrl.Result{Requeue: true}, nil
	}
}

func KustainerClusterReady(t *testing.T) AdxClusterReady {
	t.Helper()

	return func(ctx context.Context, r *Reconciler, operator *adxmonv1.Operator) (ctrl.Result, error) {
		c := metav1.Condition{
			Type:               adxmonv1.ADXClusterConditionOwner,
			Status:             metav1.ConditionTrue,
			Reason:             string(adxmonv1.OperatorServiceReasonInstalled),
			LastTransitionTime: metav1.NewTime(time.Now()),
			ObservedGeneration: operator.GetGeneration(),
		}
		if meta.SetStatusCondition(&operator.Status.Conditions, c) {
			require.NoError(t, r.Status().Update(ctx, operator))
		}

		return ctrl.Result{Requeue: true}, nil
	}
}
