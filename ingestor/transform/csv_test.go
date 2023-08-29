package transform

import (
	"bytes"
	"testing"

	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestMarshalCSV(t *testing.T) {
	ts := prompb.TimeSeries{
		Labels: []prompb.Label{
			{
				Name:  []byte("__name__"),
				Value: []byte("__redis__"),
			},
			{
				Name:  []byte("measurement"),
				Value: []byte("used_cpu_user_children"),
			},
			{
				Name:  []byte("hostname"),
				Value: []byte("host_1"),
			},
			{
				Name:  []byte("region"),
				Value: []byte("eastus"),
			},
		},

		Samples: []prompb.Sample{
			{
				Timestamp: 1669112524001,
				Value:     0,
			},
			{
				Timestamp: 1669112525002,
				Value:     1,
			},
			{
				Timestamp: 1669112526003,
				Value:     2,
			},
		},
	}

	var b bytes.Buffer
	w := NewCSVWriter(&b, nil)
	err := w.MarshalCSV(ts)
	require.NoError(t, err)
	require.Equal(t, `2022-11-22T10:22:04.001Z,-9070404444212865161,"{""measurement"":""used_cpu_user_children"",""hostname"":""host_1"",""region"":""eastus""}",0.000000000
2022-11-22T10:22:05.002Z,-9070404444212865161,"{""measurement"":""used_cpu_user_children"",""hostname"":""host_1"",""region"":""eastus""}",1.000000000
2022-11-22T10:22:06.003Z,-9070404444212865161,"{""measurement"":""used_cpu_user_children"",""hostname"":""host_1"",""region"":""eastus""}",2.000000000
`, string(w.Bytes()))

}

func BenchmarkMarshalCSV(b *testing.B) {
	ts := prompb.TimeSeries{
		Labels: []prompb.Label{
			{
				Name:  []byte("__name__"),
				Value: []byte("__redis__"),
			},
			{
				Name:  []byte("measurement"),
				Value: []byte("used_cpu_user_children"),
			},
			{
				Name:  []byte("hostname"),
				Value: []byte("host_1"),
			},
			{
				Name:  []byte("region"),
				Value: []byte("eastus"),
			},
		},

		Samples: []prompb.Sample{
			{
				Timestamp: 1669112524001,
				Value:     0,
			},
			{
				Timestamp: 1669112525002,
				Value:     1,
			},
			{
				Timestamp: 1669112526003,
				Value:     2,
			},
		},
	}

	buf := bytes.NewBuffer(make([]byte, 0, 64*1024))
	w := NewCSVWriter(buf, []string{"region", "Hostname", "bar"})
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		w.MarshalCSV(ts)
		buf.Reset()
	}
}

func TestMarshalCSV_LiftLabel(t *testing.T) {
	ts := prompb.TimeSeries{
		Labels: []prompb.Label{
			{
				Name:  []byte("__name__"),
				Value: []byte("__redis__"),
			},
			{
				Name:  []byte("hostname"),
				Value: []byte("host_1"),
			},
			{
				Name:  []byte("measurement"),
				Value: []byte("used_cpu_user_children"),
			},
			{
				Name:  []byte("region"),
				Value: []byte("eastus"),
			},
		},

		Samples: []prompb.Sample{
			{
				Timestamp: 1669112524001,
				Value:     0,
			},
			{
				Timestamp: 1669112525002,
				Value:     1,
			},
			{
				Timestamp: 1669112526003,
				Value:     2,
			},
		},
	}

	var b bytes.Buffer
	w := NewCSVWriter(&b, []string{"region", "Hostname", "bar"})

	err := w.MarshalCSV(ts)
	require.NoError(t, err)
	require.Equal(t, `2022-11-22T10:22:04.001Z,1265838189064375029,,host_1,eastus,"{""measurement"":""used_cpu_user_children""}",0.000000000
2022-11-22T10:22:05.002Z,1265838189064375029,,host_1,eastus,"{""measurement"":""used_cpu_user_children""}",1.000000000
2022-11-22T10:22:06.003Z,1265838189064375029,,host_1,eastus,"{""measurement"":""used_cpu_user_children""}",2.000000000
`, b.String())
}

func TestNormalize(t *testing.T) {
	require.Equal(t, "Redis", string(Normalize([]byte("__redis__"))))
	require.Equal(t, "UsedCpuUserChildren", string(Normalize([]byte("used_cpu_user_children"))))
	require.Equal(t, "Host1", string(Normalize([]byte("host_1"))))
	require.Equal(t, "Region", string(Normalize([]byte("region"))))

}

func TestMarshalCSV_OTLPLog(t *testing.T) {
	rawlog := []byte(`{
    "resourceLogs": [
        {
            "resource": {
                "attributes": [
                    {
                        "key": "source",
                        "value": {
                            "stringValue": "hostname"
                        }
                    }
                ],
                "droppedAttributesCount": 1
            },
            "scopeLogs": [
                {
                    "scope": {
                        "name": "name",
                        "version": "version",
                        "droppedAttributesCount": 1
                    },
                    "logRecords": [
                        {
                            "timeUnixNano": "1669112524001",
                            "observedTimeUnixNano": "1669112524001",
                            "severityNumber": 17,
                            "severityText": "Error",
                            "body": {
                                "stringValue": "{\"msg\":\"something happened\"}"
                            },
                            "attributes": [
                                {
                                    "key": "kusto.table",
                                    "value": {
                                        "stringValue": "ATable"
                                    }
                                },
								{
                                    "key": "kusto.database",
                                    "value": {
                                        "stringValue": "ADatabase"
                                    }
                                }
                            ],
                            "droppedAttributesCount": 1,
                            "flags": 1,
                            "traceId": "",
                            "spanId": ""
                        }
                    ],
                    "schemaUrl": "scope_schema"
                }
            ],
            "schemaUrl": "resource_schema"
        }
    ]
}`)
	var log v1.ExportLogsServiceRequest
	if err := protojson.Unmarshal(rawlog, &log); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	w := NewCSVWriter(&b, nil)

	err := w.MarshalCSV(log.ResourceLogs[0].ScopeLogs[0].LogRecords)
	// err := w.marshalLog(log.ResourceLogs[0].ScopeLogs[0].LogRecords)
	require.NoError(t, err)
	require.Equal(t, `2022-11-22T10:22:04.001Z,2022-11-22T10:22:04.001Z,,,Error,SEVERITY_NUMBER_ERROR,"{""msg"":""something happened""}","{""kusto.table"":""ATable"",""kusto.database"":""ADatabase""}"
`, b.String())
}

func BenchmarkMarshalCSV_OTLPLog(b *testing.B) {
	rawlog := []byte(`{
    "resourceLogs": [
        {
            "resource": {
                "attributes": [
                    {
                        "key": "RPTenant",
                        "value": {
                            "stringValue": "eastus"
                        }
                    },
					{
                        "key": "UnderlayName",
                        "value": {
                            "stringValue": "hcp-underlay-eastus-cx-test"
                        }
                    }
                ],
                "droppedAttributesCount": 1
            },
            "scopeLogs": [
                {
                    "scope": {
                        "name": "name",
                        "version": "version",
                        "droppedAttributesCount": 1
                    },
                    "logRecords": [
                        {
                            "timeUnixNano": "1669112524001",
                            "observedTimeUnixNano": "1669112524001",
                            "severityNumber": 17,
                            "severityText": "Error",
                            "body": {
                                "stringValue": "{\"msg\":\"something happened\"}"
                            },
                            "attributes": [
                                {
                                    "key": "kusto.table",
                                    "value": {
                                        "stringValue": "ATable"
                                    }
                                },
								{
                                    "key": "kusto.database",
                                    "value": {
                                        "stringValue": "ADatabase"
                                    }
                                }
                            ],
                            "droppedAttributesCount": 1,
                            "flags": 1,
                            "traceId": "",
                            "spanId": ""
                        }
                    ],
                    "schemaUrl": "scope_schema"
                }
            ],
            "schemaUrl": "resource_schema"
        }
    ]
}`)
	var log v1.ExportLogsServiceRequest
	protojson.Unmarshal(rawlog, &log)
	var buf bytes.Buffer
	w := NewCSVWriter(&buf, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.MarshalCSV(log.ResourceLogs[0].ScopeLogs[0].LogRecords)
	}
}

// Benchmark ways of making CSVWriter compatible with Metrics and Logs

type writerType int

const (
	metricWriter writerType = iota
	logWriter
)

type writer struct {
	wType writerType
}

func (w writer) WriteInterface(t interface{}) {
	switch t := t.(type) {
	case prompb.TimeSeries:
		w.writeMetric(t)
	case *v1.ExportLogsServiceRequest:
		w.writeLog(t)
	}
}

func (w writer) WriteType(t interface{}) {
	switch w.wType {
	case metricWriter:
		w.writeMetric(t.(prompb.TimeSeries))
	case logWriter:
		w.writeLog(t.(*v1.ExportLogsServiceRequest))
	}
}

func (w writer) writeMetric(t prompb.TimeSeries) {
	// do nothing
}

func (w writer) writeLog(t *v1.ExportLogsServiceRequest) {
	// do nothing
}

func BenchmarkMethod(b *testing.B) {
	w := writer{}
	for i := 0; i < b.N; i++ {
		w.writeMetric(prompb.TimeSeries{})
	}
}

func BenchmarkInterface(b *testing.B) {
	w := writer{}
	for i := 0; i < b.N; i++ {
		w.WriteInterface(prompb.TimeSeries{})
	}
}

func BenchmarkType(b *testing.B) {
	w := writer{wType: metricWriter}
	for i := 0; i < b.N; i++ {
		w.WriteType(prompb.TimeSeries{})
	}
}

/*
Running tool: /usr/local/go/bin/go test -benchmem -run=^$ -bench ^(BenchmarkMethod|BenchmarkInterface|BenchmarkType)$ github.com/Azure/adx-mon/ingestor/transform

goos: linux
goarch: amd64
pkg: github.com/Azure/adx-mon/ingestor/transform
cpu: Intel(R) Xeon(R) Platinum 8370C CPU @ 2.80GHz
BenchmarkMethod-16       	1000000000	         0.2971 ns/op	       0 B/op	       0 allocs/op
BenchmarkInterface-16    	1000000000	         0.2929 ns/op	       0 B/op	       0 allocs/op
BenchmarkType-16         	1000000000	         0.2916 ns/op	       0 B/op	       0 allocs/op
PASS
ok  	github.com/Azure/adx-mon/ingestor/transform	0.992s

Conclusion, since there's no meaningful difference between these approaches, the easiest
solution from the caller's perspective, which requires the least amount of upstream changes,
is to perform the type assertion against the interface{} and call the method directly.
*/
