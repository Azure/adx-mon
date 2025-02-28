package testutils_test

import (
	"context"
	"encoding/json"
	"testing"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func TestInstallFunctionsCrd(t *testing.T) {
	testutils.IntegrationTest(t)

	ctx := context.Background()
	k3sContainer, err := k3s.Run(ctx, "rancher/k3s:v1.31.2-k3s1")
	testcontainers.CleanupContainer(t, k3sContainer)
	require.NoError(t, err)

	t.Run("install functions crd definition", func(t *testing.T) {
		require.NoError(t, testutils.InstallCrds(ctx, k3sContainer))
	})

	t.Run("ensure idempotent", func(t *testing.T) {
		require.NoError(t, testutils.InstallCrds(ctx, k3sContainer))
	})

	t.Run("install functions crd", func(t *testing.T) {
		fn := &adxmonv1.Function{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "adx-mon.azure.com/v1",
				Kind:       "Function",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
			Spec: adxmonv1.FunctionSpec{},
		}

		_, ctrlCli, err := testutils.GetKubeConfig(ctx, k3sContainer)
		require.NoError(t, err)
		patchBytes, err := json.Marshal(fn)
		require.NoError(t, err)
		err = ctrlCli.Patch(ctx, fn, ctrlclient.RawPatch(types.ApplyPatchType, patchBytes), &ctrlclient.PatchOptions{
			FieldManager: "testcontainers",
		})
		require.NoError(t, err)
	})

	t.Run("ensure definition installed", func(t *testing.T) {
		_, ctrlCli, err := testutils.GetKubeConfig(ctx, k3sContainer)
		require.NoError(t, err)

		require.NoError(t, ctrlCli.Get(ctx, types.NamespacedName{Name: "test", Namespace: "default"}, &adxmonv1.Function{}))
	})
}
