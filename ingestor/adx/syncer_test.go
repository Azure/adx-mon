package adx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
	name, err := s.EnsureDefaultMapping(context.Background(), "Test")
	require.NoError(t, err)
	require.Equal(t, "Test_15745692345339290292", name)
}

func TestSyncer_EnsureTable(t *testing.T) {
	kcli := &fakeKustoMgmt{
		expectedQuery: ".create-merge table ['Test'] ()",
	}

	s := NewSyncer(kcli, "db", schema.SchemaMapping{}, PromMetrics)
	require.NoError(t, s.EnsureDefaultTable(context.Background(), "Test"))
	kcli.Verify(t)
}

func TestSyncer_EnsurePromMetricsFunctionsCreatesPromRate(t *testing.T) {
	kcli := &fakeKustoMgmt{}

	s := NewSyncer(kcli, "db", schema.SchemaMapping{}, PromMetrics)
	require.NoError(t, s.ensurePromMetricsFunctions(context.Background()))
	require.Len(t, kcli.queries, 3)

	require.Contains(t, kcli.queries[0], ".create-or-alter function prom_increase")
	require.Contains(t, kcli.queries[1], ".create-or-alter function prom_rate")
	require.Contains(t, kcli.queries[2], ".create-or-alter function prom_delta")

	promRate := kcli.queries[1]
	require.True(t, strings.HasPrefix(promRate, ".create-or-alter function prom_rate "), promRate)
	require.Contains(t, promRate, "| partition hint.strategy=shuffle by SeriesId (")
	require.Contains(t, promRate, "| extend sampleGap=(Timestamp-prevTs)/1s")
	require.Contains(t, promRate, "| extend Value=iff(sampleGap > 0, inc/sampleGap, real(null))")
	require.Contains(t, promRate, "| where isfinite(Value)")
	require.NotContains(t, promRate, "| invoke prom_increase")
	require.NotContains(t, promRate, "Value=inc/((Timestamp-prevTs)/1s)")
}

func TestSyncer_PromMetricsFunctionsUseCurrentValueOnReset(t *testing.T) {
	kcli := &fakeKustoMgmt{}
	s := NewSyncer(kcli, "db", schema.SchemaMapping{}, PromMetrics)
	require.NoError(t, s.ensurePromMetricsFunctions(t.Context()))
	require.Len(t, kcli.queries, 3)

	for _, query := range kcli.queries {
		require.NotContains(t, query, "next(")
	}
	require.Contains(t, kcli.queries[0], "case(diff < 0, Value, diff)")
	require.Contains(t, kcli.queries[1], "Value-prevVal < 0, Value, Value-prevVal")
	require.Contains(t, kcli.queries[2], "case(diff < 0, Value, diff)")
}

func TestSyncer_PromMetricsCounterResets(t *testing.T) {
	testutils.IntegrationTest(t)

	ctx := t.Context()
	k, err := kustainer.Run(ctx, "mcr.microsoft.com/azuredataexplorer/kustainer-linux:latest", kustainer.WithStarted())
	testcontainers.CleanupContainer(t, k)
	require.NoError(t, err)

	client, err := azkustodata.New(azkustodata.NewConnectionStringBuilder(k.ConnectionUrl()))
	require.NoError(t, err)
	defer func() { require.NoError(t, client.Close()) }()

	s := NewSyncer(client, "NetDefaultDB", schema.SchemaMapping{}, PromMetrics)
	require.NoError(t, s.ensurePromMetricsFunctions(ctx))

	type sample struct {
		seriesID int64
		offset   int64
		increase float64
		rate     float64
	}
	tests := []struct {
		name    string
		samples string
		want    []sample
	}{
		{
			name:    "monotonic with uneven spacing",
			samples: "1,0,100, 1,10,110, 1,30,130",
			want:    []sample{{1, 0, 0, 0}, {1, 10, 10, 1}, {1, 30, 20, 1}},
		},
		{
			name:    "reset with uneven spacing",
			samples: "1,0,100, 1,10,3, 1,30,13",
			want:    []sample{{1, 0, 0, 0}, {1, 10, 3, 0.3}, {1, 30, 10, 0.5}},
		},
		{
			name:    "reset at final sample",
			samples: "1,0,100, 1,10,3",
			want:    []sample{{1, 0, 0, 0}, {1, 10, 3, 0.3}},
		},
		{
			name:    "reset to zero",
			samples: "1,0,100, 1,10,0, 1,30,4",
			want:    []sample{{1, 0, 0, 0}, {1, 10, 0, 0}, {1, 30, 4, 0.2}},
		},
		{
			name:    "consecutive resets",
			samples: "1,0,100, 1,10,10, 1,20,5, 1,30,20",
			want:    []sample{{1, 0, 0, 0}, {1, 10, 10, 1}, {1, 20, 5, 0.5}, {1, 30, 15, 1.5}},
		},
		{
			name:    "constant counter",
			samples: "1,0,5, 1,10,5, 1,30,5",
			want:    []sample{{1, 0, 0, 0}, {1, 10, 0, 0}, {1, 30, 0, 0}},
		},
		{
			name:    "single sample",
			samples: "1,0,100",
			want:    []sample{{1, 0, 0, 0}},
		},
		{
			name:    "independent series with unordered input",
			samples: "2,30,8, 1,10,3, 2,0,50, 1,0,100, 2,10,4, 1,30,13",
			want: []sample{
				{1, 0, 0, 0}, {1, 10, 3, 0.3}, {1, 30, 10, 0.5},
				{2, 0, 0, 0}, {2, 10, 4, 0.4}, {2, 30, 4, 0.2},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, name := range []string{"prom_increase", "prom_delta", "prom_rate"} {
				t.Run(name, func(t *testing.T) {
					query := fmt.Sprintf(`datatable(SeriesId:long, Offset:long, Value:real) [%s]
| extend Timestamp=datetime(2026-01-01) + Offset * 1s, Labels=dynamic({"test":"reset"})
| project Timestamp, SeriesId, Labels, Value
| invoke %s()
| project SeriesId, Offset=tolong((Timestamp-datetime(2026-01-01))/1s), Value, NullValue=isnull(Value), Label=tostring(Labels.test)
| order by SeriesId asc, Offset asc`, tt.samples, name)
					ds, err := client.Query(t.Context(), "NetDefaultDB", kql.New("").AddUnsafe(query))
					require.NoError(t, err)
					require.NotEmpty(t, ds.Tables())

					type result struct {
						SeriesID  int64 `kusto:"SeriesId"`
						Offset    int64
						Value     float64
						NullValue bool
						Label     string
					}
					var got []result
					for _, row := range ds.Tables()[0].Rows() {
						var value result
						require.NoError(t, row.ToStruct(&value))
						require.False(t, value.NullValue)
						got = append(got, value)
					}

					var want []result
					for _, value := range tt.want {
						if name == "prom_rate" && value.offset == 0 {
							continue
						}
						expected := value.increase
						if name == "prom_rate" {
							expected = value.rate
						}
						want = append(want, result{
							SeriesID: value.seriesID,
							Offset:   value.offset,
							Value:    expected,
							Label:    "reset",
						})
					}
					require.Len(t, got, len(want))
					for i, expected := range want {
						require.Equal(t, expected.SeriesID, got[i].SeriesID)
						require.Equal(t, expected.Offset, got[i].Offset)
						require.Equal(t, expected.Label, got[i].Label)
						require.InDelta(t, expected.Value, got[i].Value, 1e-12)
					}
				})
			}
		})
	}
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
	require.NoError(t, s.EnsureTable(ctx, "CpuUsage", schema.DefaultMetricsMapping))
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
