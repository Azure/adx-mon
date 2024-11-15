package adx

import (
	"context"
	"errors"
	"testing"
	"time"

	v1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/ingestor/storage"
	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/Azure/adx-mon/pkg/testutils/kustainer"
	"github.com/Azure/adx-mon/schema"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/kql"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

func TestSyncer_EnsureMapping(t *testing.T) {
	kcli := kusto.NewMockClient()

	s := NewSyncer(kcli, "db", schema.SchemaMapping{}, PromMetrics, &storage.Functions{})
	name, err := s.EnsureDefaultMapping("Test")
	require.NoError(t, err)
	require.Equal(t, "Test_15745692345339290292", name)
}

func TestSyncer_EnsureTable(t *testing.T) {
	kcli := &fakeKustoMgmt{
		expectedQuery: ".create-merge table ['Test'] ()",
	}

	s := NewSyncer(kcli, "db", schema.SchemaMapping{}, PromMetrics, &storage.Functions{})
	require.NoError(t, s.EnsureDefaultTable("Test"))
	kcli.Verify(t)
}

func TestSanitizerErrorString(t *testing.T) {
	err := errors.New("https://mystoragequeue.queue.core.windows.net/someaccount/myTable?se=2024-02-09T10%3A23%3A23Z&sig=SomeMagicalS3cr3tString%3D&sp=a&st=2024-02-08T22%3A18%3A23Z&sv=2022-11-02")
	require.Contains(t, sanitizeErrorString(err).Error(), "sig=REDACTED")
	require.NotContains(t, sanitizeErrorString(err).Error(), "SomeMagicalS3cr3tString")

	err = errors.New(`Failed to upload file: Op(OpFileIngest): Kind(KBlobstore): -> github.com/Azure/azure-pipeline-go/pipeline.NewError, /app/3rdparty/adx-mon/vendor/github.com/Azure/azure-pipeline-go/pipeline/error.go:157\nHTTP request failed\n\nPost \"https://mystoragequeue.queue.core.windows.net/mystorageaccount/myqueue?se=2024-02-09T10%3A23%3A23Z&sig=SomeS3cretThatIsnotPublic149%3D&sp=a&st=2024-02-08T22%3A18%3A23Z&sv=2022-11-02&visibilitytimeout=0\": dial tcp 20.60.109.47:443: connect: connection refused\n`)
	require.Contains(t, sanitizeErrorString(err).Error(), "sig=REDACTED")
	require.NotContains(t, sanitizeErrorString(err).Error(), "SomeS3cretThatIsnotPublic149")
}

func TestSyncer(t *testing.T) {
	var (
		database = "NetDefaultDB"
		table    = "Metrics"
		ctx      = context.Background()
	)
	kustoContainer, err := kustainer.Run(ctx, "mcr.microsoft.com/azuredataexplorer/kustainer-linux:latest", kustainer.WithStarted())
	testcontainers.CleanupContainer(t, kustoContainer)
	require.NoError(t, err)

	cb := kusto.NewConnectionStringBuilder(kustoContainer.ConnectionUrl())
	client, err := kusto.New(cb)
	require.NoError(t, err)
	defer client.Close()

	s := NewSyncer(client, database, schema.DefaultMetricsMapping, PromMetrics, &TestFunctionStore{t: t})
	require.NoError(t, s.Open(ctx))

	t.Run("Default functions exist", func(t *testing.T) {
		connectionUri := kustoContainer.ConnectionUrl()
		defaultFunctions := []string{"prom_increase", "prom_rate", "prom_delta", "CountCardinality"}
		for _, fn := range defaultFunctions {
			t.Run("Function exists: "+fn, func(t *testing.T) {
				require.Eventually(t, func() bool {
					return testutils.FunctionExists(ctx, t, database, "prom_increase", connectionUri)
				}, time.Minute, time.Second)
			})
		}
	})

	t.Run("Create ingestion table", func(t *testing.T) {
		require.NoError(t, s.EnsureDefaultTable(table))
	})

	t.Run("Table exists in Kusto", func(t *testing.T) {
		require.Eventually(t, func() bool {
			return testutils.TableExists(ctx, t, database, table, kustoContainer.ConnectionUrl())
		}, time.Minute, time.Second)
	})

	var mapping string
	t.Run("Create ingestion mapping", func(t *testing.T) {
		mapping, err = s.EnsureDefaultMapping(table)
		require.NoError(t, err)
		require.NotEmpty(t, mapping)
	})

	t.Run("Verify ingestion mapping", func(t *testing.T) {
		stmt := kql.New(".show table ").AddUnsafe(table).AddLiteral(" ingestion csv mapping ").AddString(mapping)
		_, err = client.Mgmt(ctx, database, stmt)
		require.NoError(t, err)
	})

	require.NoError(t, s.Close())
}

type TestFunctionStore struct {
	t *testing.T
}

func (store *TestFunctionStore) Functions() []*v1.Function {
	return nil
}

func (store *TestFunctionStore) View(database, table string) (*v1.Function, bool) {
	return nil, false
}

func (store *TestFunctionStore) UpdateStatus(ctx context.Context, fn *v1.Function) error {
	return nil
}
