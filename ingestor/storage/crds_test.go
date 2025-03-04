package storage_test

import (
	"context"
	"testing"

	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/ingestor/storage"
)

func TestCRDs(t *testing.T) {
	testutils.IntegrationTest(t)

	scheme := clientgoscheme.Scheme
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, adxmonv1.AddToScheme(scheme))

	ctx := context.Background()
	k3sContainer, err := k3s.Run(ctx, "rancher/k3s:v1.31.2-k3s1")
	testcontainers.CleanupContainer(t, k3sContainer)
	require.NoError(t, err)

	require.NoError(t, testutils.InstallCrds(ctx, k3sContainer))
	_, ctrlCli, err := testutils.GetKubeConfig(ctx, k3sContainer)
	require.NoError(t, err)

	store := storage.NewCRDHandler(ctrlCli, nil)

	resourceName := "testtest"
	typeNamespacedName := types.NamespacedName{
		Name:      resourceName,
		Namespace: "default",
	}

	// Create a SummaryRule object
	sr := &adxmonv1.SummaryRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      typeNamespacedName.Name,
			Namespace: typeNamespacedName.Namespace,
		},
	}
	require.NoError(t, ctrlCli.Create(ctx, sr))

	// List the SummaryRule objects. We expect to receive the one we just created
	list := &adxmonv1.SummaryRuleList{}
	require.NoError(t, store.List(ctx, list))
	require.Len(t, list.Items, 1)

	// Retrieve the SummaryRule object by its name and namespace as stored in k8s
	require.NoError(t, ctrlCli.Get(ctx, typeNamespacedName, sr))
	require.NoError(t, store.UpdateStatus(ctx, sr, nil))

	// There should be no objects returned by List because we set the status to success
	list = &adxmonv1.SummaryRuleList{}
	require.NoError(t, store.List(ctx, list))
	require.Len(t, list.Items, 0)

	// Get the SummaryRule object again and check its status
	sr = &adxmonv1.SummaryRule{}
	require.NoError(t, ctrlCli.Get(ctx, typeNamespacedName, sr))
	require.Equal(t, 1, len(sr.Status.Conditions))

	cnd := sr.GetCondition()
	require.Equal(t, metav1.ConditionTrue, cnd.Status)
	require.Equal(t, sr.GetGeneration(), cnd.ObservedGeneration)
	require.Equal(t, adxmonv1.SummaryRuleOwner, cnd.Type)
}
