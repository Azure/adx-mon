package adx

import (
	"context"
	"errors"
	"testing"

	"github.com/Azure/adx-mon/schema"
	"github.com/stretchr/testify/require"
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

func TestSyncer_EnsurePromMetricsFunctionsCreatesPromRate(t *testing.T) {
	kcli := &fakeKustoMgmt{}

	s := NewSyncer(kcli, "db", schema.SchemaMapping{}, PromMetrics)
	require.NoError(t, s.ensurePromMetricsFunctions(context.Background()))
	require.Len(t, kcli.queries, 3)
	require.Equal(t, `.create-or-alter function prom_rate (T:(Timestamp:datetime, SeriesId: long, Labels:dynamic, Value:real), interval:timespan=1m) {
		T
		| where isnan(Value) == false
		| partition hint.strategy=shuffle by SeriesId (
			order by Timestamp asc
			| extend prevVal=prev(Value), prevTs=prev(Timestamp)
			| extend sampleGap=(Timestamp-prevTs)/1s
			| extend inc=case(isnull(prevVal), real(null), Value-prevVal < 0, next(Value)-Value, Value-prevVal)
			| extend Value=iff(sampleGap > 0, inc/sampleGap, real(null))
			| project Timestamp, SeriesId, Labels, Value
		)
		| where isfinite(Value)}`, kcli.queries[1])
}

func TestSanitizerErrorString(t *testing.T) {
	err := errors.New("https://mystoragequeue.queue.core.windows.net/someaccount/myTable?se=2024-02-09T10%3A23%3A23Z&sig=SomeMagicalS3cr3tString%3D&sp=a&st=2024-02-08T22%3A18%3A23Z&sv=2022-11-02")
	require.Contains(t, sanitizeErrorString(err).Error(), "sig=REDACTED")
	require.NotContains(t, sanitizeErrorString(err).Error(), "SomeMagicalS3cr3tString")

	err = errors.New(`Failed to upload file: Op(OpFileIngest): Kind(KBlobstore): -> github.com/Azure/azure-pipeline-go/pipeline.NewError, /app/3rdparty/adx-mon/vendor/github.com/Azure/azure-pipeline-go/pipeline/error.go:157\nHTTP request failed\n\nPost \"https://mystoragequeue.queue.core.windows.net/mystorageaccount/myqueue?se=2024-02-09T10%3A23%3A23Z&sig=SomeS3cretThatIsnotPublic149%3D&sp=a&st=2024-02-08T22%3A18%3A23Z&sv=2022-11-02&visibilitytimeout=0\": dial tcp 20.60.109.47:443: connect: connection refused\n`)
	require.Contains(t, sanitizeErrorString(err).Error(), "sig=REDACTED")
	require.NotContains(t, sanitizeErrorString(err).Error(), "SomeS3cretThatIsnotPublic149")
}
