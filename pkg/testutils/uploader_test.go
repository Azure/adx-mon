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
	if testing.Short() {
		t.Skip("skipping test in short mode")
	}

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(3*time.Minute))
	defer cancel()

	kustainerContainer, err := kustainer.Run(ctx, "mcr.microsoft.com/azuredataexplorer/kustainer-linux:latest")
	defer testcontainers.CleanupContainer(t, kustainerContainer)
	require.NoError(t, err)

	var (
		dbName  = "testdb"
		tblName = "testtbl"
		columns = []kustainer.TableColum{
			{
				Name: "id",
				Type: "int",
			},
			{
				Name: "name",
				Type: "string",
			},
		}
	)
	require.NoError(t, kustainerContainer.CreateDatabase(ctx, dbName))
	require.NoError(t, kustainerContainer.CreateTable(ctx, dbName, tblName, columns))

	kcsb := kusto.NewConnectionStringBuilder(kustainerContainer.MustConnectionUrl(ctx))
	client, err := kusto.New(kcsb)
	require.NoError(t, err)
	defer client.Close()

	data := "2,bar\n"
	r := strings.NewReader(data)
	uploader := testutils.NewUploadReader(client, dbName, tblName)
	_, err = uploader.FromReader(ctx, r)
	require.NoError(t, err)
}
