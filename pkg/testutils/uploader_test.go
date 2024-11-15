package testutils_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/Azure/adx-mon/pkg/testutils/kustainer"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

func TestUploader(t *testing.T) {
	var (
		database = "NetDefaultDB"
		table    = "Table_0"
		ctx      = context.Background()
	)
	k, err := kustainer.Run(ctx, "mcr.microsoft.com/azuredataexplorer/kustainer-linux:latest", kustainer.WithStarted())
	testcontainers.CleanupContainer(t, k)
	require.NoError(t, err)

	cb := kusto.NewConnectionStringBuilder(k.ConnectionUrl())
	client, err := kusto.New(cb)
	require.NoError(t, err)
	defer client.Close()

	t.Run("Ingest", func(t *testing.T) {
		uploader := testutils.NewUploadReader(client, database, table)
		r := strings.NewReader("a")
		result, err := uploader.FromReader(ctx, r)
		require.NoError(t, err)
		require.NotNil(t, result)
	})

	t.Run("Table exists in Kusto", func(t *testing.T) {
		require.Eventually(t, func() bool {
			return testutils.TableExists(ctx, t, database, table, k.ConnectionUrl())
		}, time.Minute, time.Second)
	})

	t.Run("Table has rows", func(t *testing.T) {
		require.Eventually(t, func() bool {
			return testutils.TableHasRows(ctx, t, database, table, k.ConnectionUrl())
		}, time.Minute, time.Second)
	})
}
