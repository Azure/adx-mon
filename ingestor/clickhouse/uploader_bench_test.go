package clickhouse

import (
	"testing"
	"time"

	"github.com/Azure/adx-mon/schema"
)

type noopBatchWriter struct{}

func (noopBatchWriter) Append(values ...any) error { return nil }
func (noopBatchWriter) Flush() error               { return nil }
func (noopBatchWriter) Send() error                { return nil }
func (noopBatchWriter) Close() error               { return nil }

func BenchmarkProcessRecords(b *testing.B) {
	columns := convertToColumns(schema.NewMetricsSchema())
	converters := buildConverters(columns)
	rowBuffer := make([]any, len(columns))
	baseRecord := make([]string, len(columns))
	for i, column := range columns {
		switch column.Type {
		case "DateTime64":
			baseRecord[i] = time.Unix(0, 0).UTC().Format(time.RFC3339Nano)
		case "UInt64":
			baseRecord[i] = "1234567890"
		case "Int64", "Int32":
			baseRecord[i] = "42"
		case "Float64":
			baseRecord[i] = "3.14159"
		default:
			baseRecord[i] = "value"
		}
	}

	// Build a slice of records to simulate batching.
	records := make([][]string, 0, 128)
	for i := 0; i < cap(records); i++ {
		record := append([]string(nil), baseRecord...)
		if len(record) > 0 {
			record[0] = time.Unix(int64(i), 0).UTC().Format(time.RFC3339Nano)
		}
		records = append(records, record)
	}

	writer := noopBatchWriter{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, record := range records {
			if err := convertRecordInto(record, columns, converters, rowBuffer); err != nil {
				b.Fatalf("convertRecordInto: %v", err)
			}
			if err := writer.Append(rowBuffer...); err != nil {
				b.Fatalf("append: %v", err)
			}
		}
	}
}
