package adx

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/Azure/adx-mon/pkg/testutils/kustainer"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

func TestClusterRequiresDirectIngest(t *testing.T) {
	testutils.IntegrationTest(t)

	ctx := context.Background()
	k, err := kustainer.Run(ctx, "mcr.microsoft.com/azuredataexplorer/kustainer-linux:latest", kustainer.WithStarted())
	testcontainers.CleanupContainer(t, k)
	require.NoError(t, err)

	cb := kusto.NewConnectionStringBuilder(k.ConnectionUrl())
	client, err := kusto.New(cb)
	require.NoError(t, err)
	defer client.Close()

	u := &uploader{
		KustoCli: client,
		database: "NetDefaultDB",
	}
	requiresDirectIngest, err := u.clusterRequiresDirectIngest(ctx)
	require.NoError(t, err)
	require.True(t, requiresDirectIngest)
}

func BenchmarkCountingReader(b *testing.B) {
	data := bytes.Repeat([]byte("testdata"), 1024)
	reader := bytes.NewReader(data)
	countReader := newCountingReader(reader)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		reader.Reset(data)
		countReader.Reset(reader)

		_, _ = io.Copy(io.Discard, countReader)
		if countReader.Count() != int64(len(data)) {
			b.Fatalf("expected %d bytes read, got %d", len(data), countReader.Count())
		}
	}
}
