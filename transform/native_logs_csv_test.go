package transform

import (
	"bytes"
	"encoding/csv"
	"testing"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/stretchr/testify/require"
)

func TestTimeConversionForOTLPLogs(t *testing.T) {
	tests := []struct {
		TS     int64
		Expect string
	}{
		{
			TS:     1694564005797489936,
			Expect: "2023-09-13T00:13:25.797489936Z",
		},
		{
			TS:     1669112524001,
			Expect: "2022-11-22T10:22:04.001Z",
		},
	}
	for _, tt := range tests {
		t.Run(tt.Expect, func(t *testing.T) {
			require.Equal(t, tt.Expect, otlpTSToUTC(tt.TS))
		})
	}
}

func TestMarshalCSV_NativeLog(t *testing.T) {
	type testcase struct {
		Name   string
		Batch  *types.LogBatch
		Expect []*logrecord
	}

	tests := []testcase{
		{
			Name: "simple",
			Batch: &types.LogBatch{
				Logs: []*types.Log{
					{
						Timestamp:         1696983205797489936,
						ObservedTimestamp: 1696983226797489936, // +21s
						Body: map[string]interface{}{
							types.BodyKeyMessage: "something\n happened",
							"key\nwithnewline":   "value",
						},
						Attributes: map[string]interface{}{
							// adx-mon attributes are filtered
							"adxmon_cursor_position":    "abcdef",
							types.AttributeDatabaseName: "ADatabase",
							types.AttributeTableName:    "ATable",
							"hello\nkey":                "world\ntraveler",
							"other":                     "attribute",
						},
						Resource: map[string]interface{}{
							"RPTenant": "eastus",
							// Labels, annotations and adxmon_ are filtered since they are only used internally
							"label.foo":        "labels",
							"annotation.bar":   "annotations",
							"adxmon_container": "adxmon prefix",
						},
					},
				},
			},
			Expect: []*logrecord{
				{
					Timestamp:         "2023-10-11T00:13:25.797489936Z",
					ObservedTimestamp: "2023-10-11T00:13:46.797489936Z",
					SeverityText:      "",
					SeverityNumber:    "",
					TraceId:           "",
					SpanId:            "",
					Body: map[string]interface{}{
						"message":          "something\n happened",
						"key\nwithnewline": "value",
					},
					Resource: map[string]interface{}{
						"RPTenant": "eastus",
					},
					Attributes: map[string]interface{}{
						"hello\nkey": "world\ntraveler",
						"other":      "attribute",
					},
				},
			},
		},
		{
			Name: "complex values",
			Batch: &types.LogBatch{
				Logs: []*types.Log{
					{
						Timestamp:         1696983205797489936,
						ObservedTimestamp: 1696983226797489936, // +21s
						Body: map[string]interface{}{
							types.BodyKeyMessage: "something happened",
							"complexVal": map[string]interface{}{
								"nested": "value",
								"hello":  []string{"world"},
							},
						},
						Attributes: map[string]interface{}{
							// adx-mon attributes are filtered
							types.AttributeDatabaseName: "ADatabase",
							types.AttributeTableName:    "ATable",
							"hello":                     []string{"world"},
							"other":                     "attribute",
						},
						Resource: map[string]interface{}{
							"RPTenant": "eastus",
							"goodbye":  []string{"space"},
						},
					},
					{
						Timestamp:         1696983226797489936, // +21s
						ObservedTimestamp: 1696983229797489936, // +3s
						Body: map[string]interface{}{
							types.BodyKeyMessage: "something happened",
							"complexVal": map[string]interface{}{
								"nested": "other value",
								"hello":  []string{"world"},
							},
						},
						Attributes: map[string]interface{}{
							// adx-mon attributes are filtered
							types.AttributeDatabaseName: "ADatabase",
							types.AttributeTableName:    "ATable",
							"hello":                     []string{"space"},
							"other":                     "attribute",
						},
						Resource: map[string]interface{}{
							"RPTenant": "eastus",
						},
					},
				},
			},
			Expect: []*logrecord{
				{
					Timestamp:         "2023-10-11T00:13:25.797489936Z",
					ObservedTimestamp: "2023-10-11T00:13:46.797489936Z",
					SeverityText:      "",
					SeverityNumber:    "",
					TraceId:           "",
					SpanId:            "",
					Body: map[string]interface{}{
						"message": "something happened",
						"complexVal": map[string]interface{}{
							"nested": "value",
							"hello":  []interface{}{"world"},
						},
					},
					Resource: map[string]interface{}{
						"RPTenant": "eastus",
						"goodbye":  []interface{}{"space"},
					},
					Attributes: map[string]interface{}{
						"hello": []interface{}{"world"},
						"other": "attribute",
					},
				},
				{
					Timestamp:         "2023-10-11T00:13:46.797489936Z",
					ObservedTimestamp: "2023-10-11T00:13:49.797489936Z",
					SeverityText:      "",
					SeverityNumber:    "",
					TraceId:           "",
					SpanId:            "",
					Body: map[string]interface{}{
						"message": "something happened",
						"complexVal": map[string]interface{}{
							"nested": "other value",
							"hello":  []interface{}{"world"},
						},
					},
					Resource: map[string]interface{}{
						"RPTenant": "eastus",
					},
					Attributes: map[string]interface{}{
						"hello": []interface{}{"space"},
						"other": "attribute",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			var b bytes.Buffer
			w := NewCSVWriter(&b, nil)

			for _, log := range tt.Batch.Logs {
				err := w.MarshalNativeLog(log)
				require.NoError(t, err)
			}

			// Check the format instead of string comparisons
			// Iterating through maps when writing the CSV is non-deterministic on purpose in Go,
			// so we can't just do a string comparison.
			reader := csv.NewReader(&b)
			records, err := reader.ReadAll()
			require.NoError(t, err)
			require.Len(t, records, len(tt.Expect))

			for i, expect := range tt.Expect {
				record, err := getLogRecord(records[i])
				require.NoError(t, err)
				require.Equal(t, expect, record)
			}
		})
	}
}

func BenchmarkMarshalCSV_NativeLog(b *testing.B) {
	batch := &types.LogBatch{
		Logs: []*types.Log{
			{
				Timestamp:         1696983205797489936,
				ObservedTimestamp: 1696983226797489936, // +21s
				Body: map[string]interface{}{
					types.BodyKeyMessage: "something happened",
				},
				Attributes: map[string]interface{}{
					// adx-mon attributes are filtered
					types.AttributeDatabaseName: "ADatabase",
					types.AttributeTableName:    "ATable",
				},
				Resource: map[string]interface{}{
					"RPTenant":     "eastus",
					"UnderlayName": "hcp-underlay-eastus-cx-test",
				},
			},
		},
	}

	buf := bytes.NewBuffer(make([]byte, 0, 64*1024))
	enc := NewCSVWriter(buf, nil)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for _, log := range batch.Logs {
			enc.MarshalNativeLog(log)
		}
		buf.Reset()
	}

}
