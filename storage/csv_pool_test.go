package storage

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	commonv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/common/v1"
	logsv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/logs/v1"
	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/pkg/otlp"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/stretchr/testify/require"
)

// Inspect ownership, not RSS or GC timing. Include the scratch buffers as well as output.
func csvCapacity(w any) int {
	v := reflect.ValueOf(w).Elem()
	n := v.FieldByName("line").Cap()
	for _, name := range []string{"w", "labelsBuf", "seriesIdBuf"} {
		n += v.FieldByName(name).Elem().FieldByName("buf").Cap()
	}
	n += v.FieldByName("buf").Elem().FieldByName("buf").Cap()
	return n
}

func TestCSVPoolBoundsRetainedCapacity(t *testing.T) {
	// The native-log benchmark writes 250 records/table; 256 KiB accommodates that
	// ordinary batch and the existing 8 KiB checkout without preserving multi-MiB spikes.
	const ceiling = 256 << 10
	for _, kind := range []string{"metrics", "native", "otlp"} {
		t.Run(kind, func(t *testing.T) {
			s := NewLocalStore(StoreOpts{StorageDir: t.TempDir(), Admission: wal.NewAdmission(wal.AdmissionOpts{}), LiftedLabels: []string{"region"}, LiftedResources: []string{"region"}})
			require.NoError(t, s.Open(t.Context()))
			t.Cleanup(func() { require.NoError(t, s.Close()) })
			// Drain the production pool so checkout/return ownership is deterministic.
			p := metricsCSVWriterPool
			switch kind {
			case "native":
				p = nativeLogsCSVWriterPool
			case "otlp":
				p = csvWriterPool
			}
			for range 1000 {
				p.Get(0)
			}
			write := func(payload string) {
				switch kind {
				case "metrics":
					req := &prompb.WriteRequest{Timeseries: []*prompb.TimeSeries{{Labels: []*prompb.Label{
						{Name: []byte("__name__"), Value: []byte("metric")},
						{Name: []byte("adxmon_database"), Value: []byte("DB")},
						{Name: []byte("payload"), Value: []byte(payload)},
						{Name: []byte("region"), Value: []byte("west")},
					}, Samples: []*prompb.Sample{{Timestamp: 1669112524001, Value: 1}}}}}
					require.NoError(t, s.WriteTimeSeries(context.Background(), req))
				case "native":
					log := types.NewLog()
					log.SetTimestamp(1669112524001000000)
					log.SetObservedTimestamp(1669112524001000000)
					log.SetAttributeValue(types.AttributeDatabaseName, "DB")
					log.SetAttributeValue(types.AttributeTableName, "Logs")
					log.SetResourceValue("region", "west")
					log.SetBodyValue("message", payload)
					require.NoError(t, s.WriteNativeLogs(context.Background(), &types.LogBatch{Logs: []*types.Log{log}}))
				case "otlp":
					require.NoError(t, s.WriteOTLPLogs(context.Background(), "DB", "Logs", &otlp.Logs{Logs: []*logsv1.LogRecord{{TimeUnixNano: 1669112524001000000, ObservedTimeUnixNano: 1669112524001000000, Body: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: payload}}}}}))
				}
			}
			write(strings.Repeat("x", 2<<20))
			large := p.Get(8192)
			require.LessOrEqual(t, csvCapacity(large), ceiling, "completed payload must not stay owned by idle writer")
			p.Put(large)
			write("small")
			small := p.Get(8192)
			capacity := csvCapacity(small)
			p.Put(small)
			write("small")
			reused := p.Get(8192)
			require.Same(t, small, reused)
			require.Equal(t, capacity, csvCapacity(reused), "ordinary backing buffers remain reusable")
			p.Put(reused)
			// Inspect the persisted blocks: every reuse must write the same header,
			// values and lifted columns as the preceding small batch.
			require.NoError(t, s.Close())
			var blocks [][]byte
			for _, prefix := range s.repository.Keys() {
				for _, info := range s.Index().Get(nil, string(prefix)) {
					seg, err := wal.Open(info.Path)
					require.NoError(t, err)
					it, err := seg.Iterator()
					require.NoError(t, err)
					for {
						ok, err := it.Next()
						require.NoError(t, err)
						if !ok {
							break
						}
						blocks = append(blocks, bytes.Clone(it.Value()))
					}
					require.NoError(t, it.Close())
					require.NoError(t, seg.Close())
				}
			}
			require.Len(t, blocks, 3)
			require.Equal(t, blocks[1], blocks[2])
			require.Contains(t, string(blocks[2]), "small")
			if kind != "otlp" {
				require.Contains(t, string(blocks[2]), "west")
				require.Contains(t, string(blocks[2]), "Timestamp:datetime")
			}
		})
	}
}
