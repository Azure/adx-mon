package adx

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/Azure/adx-mon/pkg/testutils/kustainer"
	"github.com/Azure/adx-mon/schema"
	"github.com/Azure/azure-kusto-go/azkustodata"
	"github.com/Azure/azure-kusto-go/azkustodata/kql"
	kustov1 "github.com/Azure/azure-kusto-go/azkustodata/query/v1"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

func TestSyncer_EnsureMapping(t *testing.T) {
	kcli := &fakeKustoMgmt{}

	s := NewSyncer(kcli, "db", schema.SchemaMapping{}, PromMetrics)
	name, err := s.EnsureDefaultMapping("Test")
	require.NoError(t, err)
	require.Equal(t, "Test_15745692345339290292", name)
}

func TestSyncer_EnsureTable(t *testing.T) {
	kcli := &fakeKustoMgmt{
		expectedQuery: ".create-merge table ['Test'] ()",
	}

	s := NewSyncer(kcli, "db", schema.SchemaMapping{}, PromMetrics)
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

type countingKustoMgmt struct {
	queries []string
	err     error
}

func (f *countingKustoMgmt) Mgmt(ctx context.Context, db string, query azkustodata.Statement, options ...azkustodata.QueryOption) (kustov1.Dataset, error) {
	f.queries = append(f.queries, query.String())
	if f.err != nil {
		return nil, f.err
	}
	return (&fakeKustoMgmt{}).Mgmt(ctx, db, query, options...)
}

func TestSyncer_EnsureStreamingPolicy(t *testing.T) {
	kcli := &countingKustoMgmt{}
	s := NewSyncer(kcli, "db", schema.SchemaMapping{}, PromMetrics)

	require.NoError(t, s.EnsureStreamingPolicy(context.Background(), "CpuUsage"))
	require.Equal(t, []string{".alter table CpuUsage policy streamingingestion enable"}, kcli.queries)

	// The policy is only enabled once per table.
	require.NoError(t, s.EnsureStreamingPolicy(context.Background(), "CpuUsage"))
	require.Len(t, kcli.queries, 1)

	require.NoError(t, s.EnsureStreamingPolicy(context.Background(), "MemoryUsage"))
	require.Len(t, kcli.queries, 2)
}

func TestSyncer_EnsureStreamingPolicyCachesFailure(t *testing.T) {
	kcli := &countingKustoMgmt{err: errors.New("forbidden")}
	s := NewSyncer(kcli, "db", schema.SchemaMapping{}, PromMetrics)

	err := s.EnsureStreamingPolicy(context.Background(), "CpuUsage")
	require.ErrorContains(t, err, "enable streaming ingestion policy for db.CpuUsage: forbidden")

	// The failure is cached until the retry interval passes.
	require.ErrorContains(t, s.EnsureStreamingPolicy(context.Background(), "CpuUsage"), "forbidden")
	require.Len(t, kcli.queries, 1)

	kcli.err = nil
	s.streamingPolicies["CpuUsage"] = streamingPolicyState{err: err, retryAt: time.Now().Add(-time.Second)}
	require.NoError(t, s.EnsureStreamingPolicy(context.Background(), "CpuUsage"))
	require.Len(t, kcli.queries, 2)
	require.NoError(t, s.EnsureStreamingPolicy(context.Background(), "CpuUsage"))
	require.Len(t, kcli.queries, 2)
}

func TestSyncer_EnsureStreamingPolicyQuotesTable(t *testing.T) {
	kcli := &countingKustoMgmt{}
	s := NewSyncer(kcli, "db", schema.SchemaMapping{}, PromMetrics)
	require.NoError(t, s.EnsureStreamingPolicy(context.Background(), "my'table"))
	require.Equal(t, `.alter table ["my\'table"] policy streamingingestion enable`, kcli.queries[0])
}

func TestSyncer_EnsureStreamingPolicyKustainer(t *testing.T) {
	testutils.IntegrationTest(t)

	ctx := context.Background()
	k, err := kustainer.Run(ctx, "mcr.microsoft.com/azuredataexplorer/kustainer-linux:latest", kustainer.WithStarted())
	testcontainers.CleanupContainer(t, k)
	require.NoError(t, err)

	client, err := azkustodata.New(azkustodata.NewConnectionStringBuilder(k.ConnectionUrl()))
	require.NoError(t, err)
	defer client.Close()

	s := NewSyncer(client, "NetDefaultDB", schema.DefaultMetricsMapping, PromMetrics)
	require.NoError(t, s.EnsureTable("CpuUsage", schema.DefaultMetricsMapping))
	require.NoError(t, s.EnsureStreamingPolicy(ctx, "CpuUsage"))

	ds, err := client.Mgmt(ctx, "NetDefaultDB", kql.New(".show table CpuUsage policy streamingingestion"))
	require.NoError(t, err)
	table, ok := primaryResultTable(ds)
	require.True(t, ok)
	require.Len(t, table.Rows(), 1)
	policy, err := table.Rows()[0].StringByName("Policy")
	require.NoError(t, err)
	var parsed struct{ IsEnabled bool }
	require.NoError(t, json.Unmarshal([]byte(policy), &parsed))
	require.True(t, parsed.IsEnabled)
}
