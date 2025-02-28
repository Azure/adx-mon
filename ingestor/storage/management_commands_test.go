package storage_test

import (
	"context"
	"errors"
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

func TestManagementCommands(t *testing.T) {
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

	store := storage.NewManagementCommands(ctrlCli, nil)

	resourceName := "testtest"
	typeNamespacedName := types.NamespacedName{
		Name:      resourceName,
		Namespace: "default",
	}

	t.Run("Install management command", func(t *testing.T) {
		fn := &adxmonv1.ManagementCommand{
			ObjectMeta: metav1.ObjectMeta{
				Name:      typeNamespacedName.Name,
				Namespace: typeNamespacedName.Namespace,
			},
			Spec: adxmonv1.ManagementCommandSpec{
				Body:     "some-function-body",
				Database: "some-database",
			},
		}
		require.NoError(t, ctrlCli.Create(ctx, fn))
	})

	t.Run("List management commands", func(t *testing.T) {
		cmds, err := store.List(ctx)
		require.NoError(t, err)
		require.Len(t, cmds, 1)

		cmd := cmds[0]
		require.Equal(t, int64(1), cmd.GetGeneration())
		require.Equal(t, 0, len(cmd.Status.Conditions))
	})

	t.Run("Updates status", func(t *testing.T) {
		cmd := &adxmonv1.ManagementCommand{}
		require.NoError(t, ctrlCli.Get(ctx, typeNamespacedName, cmd))
		require.NoError(t, store.UpdateStatus(ctx, cmd, nil))

		cmd = &adxmonv1.ManagementCommand{}
		require.NoError(t, ctrlCli.Get(ctx, typeNamespacedName, cmd))

		require.Equal(t, 1, len(cmd.Status.Conditions))
		condition := cmd.Status.Conditions[0]
		require.Equal(t, metav1.ConditionTrue, condition.Status)
		require.Equal(t, cmd.GetGeneration(), condition.ObservedGeneration)
		require.False(t, condition.LastTransitionTime.IsZero())
		require.Empty(t, condition.Message)

		cmds, err := store.List(ctx)
		require.NoError(t, err)
		require.Empty(t, cmds) // because the generation is up to date
	})

	t.Run("Does not update generation upon failure", func(t *testing.T) {
		cmd := &adxmonv1.ManagementCommand{}
		require.NoError(t, ctrlCli.Get(ctx, typeNamespacedName, cmd))

		currentGeneration := cmd.GetGeneration()
		require.NoError(t, store.UpdateStatus(ctx, cmd, errors.New("some error")))

		cmd = &adxmonv1.ManagementCommand{}
		require.NoError(t, ctrlCli.Get(ctx, typeNamespacedName, cmd))
		require.Equal(t, 1, len(cmd.Status.Conditions))

		condition := cmd.Status.Conditions[0]
		require.Equal(t, metav1.ConditionFalse, condition.Status)
		require.Equal(t, "some error", condition.Message)
		require.Equal(t, currentGeneration, condition.ObservedGeneration)
		require.Equal(t, currentGeneration, cmd.GetGeneration())
	})

	t.Run("Respects leader election", func(t *testing.T) {
		coord := &elector{isLeader: true}
		store := storage.NewManagementCommands(ctrlCli, coord)

		// Update the command to ensure the generation is updated
		cmd := &adxmonv1.ManagementCommand{}
		require.NoError(t, ctrlCli.Get(ctx, typeNamespacedName, cmd))
		cmd.Spec.Database = "some-other-database"
		require.NoError(t, ctrlCli.Update(ctx, cmd))

		cmds, err := store.List(ctx)
		require.NoError(t, err)
		require.Len(t, cmds, 1)

		coord.isLeader = false
		cmds, err = store.List(ctx)
		require.NoError(t, err)
		require.Empty(t, cmds)
	})
}
