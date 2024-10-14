package transform

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	"github.com/Azure/adx-mon/pkg/otlp"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestMarshalCSV_OTLPLog(t *testing.T) {
	tests := []struct {
		Name   string
		Log    []byte
		Expect string
	}{
		{
			Name: "unstructured",
			Log: []byte(`{
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
															"stringValue": "something happened"
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
							}`),
			Expect: `2022-11-22T10:22:04.001Z,2022-11-22T10:22:04.001Z,,,Error,SEVERITY_NUMBER_ERROR,something happened,"{""source"":""hostname""}","{""kusto.table"":""ATable"",""kusto.database"":""ADatabase""}"
`,
		},
		{
			Name: "structured",
			Log: []byte(`{
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
																"timeUnixNano": "1694564005797489936",
																"observedTimeUnixNano": "1694564005797489936",
																"severityNumber": 17,
																"severityText": "Error",
																"body": {
																	"kvlistValue": {
																		"values":[
																			{
																				"key":"namespace",
																				"value": {
																					"stringValue": "default"
																				}
																			}
																		]
																	}
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
									}`),
			Expect: `2023-09-13T00:13:25.797489936Z,2023-09-13T00:13:25.797489936Z,,,Error,SEVERITY_NUMBER_ERROR,"{""namespace"":""default""}","{""source"":""hostname""}","{""kusto.table"":""ATable"",""kusto.database"":""ADatabase""}"
`,
		},
		{
			Name: "unescaped newline",
			Log: []byte(`{
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
																	"kvlistValue": {
																		"values":[
																			{
																				"key":"msg",
																				"value": {
																					"stringValue": "something happened\n"
																				}
																			}
																		]
																	}
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
									}`),
			Expect: `2022-11-22T10:22:04.001Z,2022-11-22T10:22:04.001Z,,,Error,SEVERITY_NUMBER_ERROR,"{""msg"":""something happened\n""}","{""source"":""hostname""}","{""kusto.table"":""ATable"",""kusto.database"":""ADatabase""}"
`,
		},
		{
			Name: "nested",
			Log:  nestedRawLog,
			Expect: `2022-11-22T10:22:04.001Z,2022-11-22T10:22:04.001Z,,,Error,SEVERITY_NUMBER_ERROR,"{""kusto.database"":""FakeDatabase"",""kusto.table"":""FakeTable"",""date"":""1715247667.480479"",""output"":""09:41:07.480478650: Notice Known system binary sent/received network traffic (user=root user_loginuid=-1 connection=REDACTED container_id=host image=\u003cNA\u003e, proc=bash)"",""priority"":""Notice"",""rule"":""System procs network activity"",""source"":""syscall"",""tags"":[""mitre_exfiltration"",""network""],""output_fields"":{""container.id"":""host"",""container.image.repository"":"""",""evt.time"":""1715247667480478650"",""fd.name"":""REDACTED"",""proc.name"":""bash"",""user.loginuid"":""-1"",""user.name"":""root""}}","{""source"":""hostname""}","{""kusto.table"":""ATable"",""kusto.database"":""ADatabase""}"
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			var log v1.ExportLogsServiceRequest
			if err := protojson.Unmarshal(tt.Log, &log); err != nil {
				t.Fatal(err)
			}
			var b bytes.Buffer
			w := NewCSVWriter(&b, nil)

			logs := &otlp.Logs{
				Resources: log.ResourceLogs[0].Resource.Attributes,
				Logs:      log.ResourceLogs[0].ScopeLogs[0].LogRecords,
			}

			err := w.MarshalLog(logs)
			require.NoError(t, err)
			t.Log(prettyPrintOTLPCSV(b.String()))
			require.Equal(t, tt.Expect, b.String())
		})
	}
}

func BenchmarkMarshalCSV_OTLPLog(b *testing.B) {
	var log v1.ExportLogsServiceRequest
	protojson.Unmarshal(nestedRawLog, &log)
	var buf bytes.Buffer
	w := NewCSVWriter(&buf, nil)
	logs := &otlp.Logs{
		Resources: log.ResourceLogs[0].Resource.Attributes,
		Logs:      log.ResourceLogs[0].ScopeLogs[0].LogRecords,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.MarshalLog(logs)
	}
}

func TestSerializeAnyValue(t *testing.T) {
	var log v1.ExportLogsServiceRequest
	require.NoError(t, protojson.Unmarshal(nestedRawLog, &log))

	var (
		buf       strings.Builder
		logRecord = log.ResourceLogs[0].ScopeLogs[0].LogRecords[0]
		body      = logRecord.GetBody()
	)

	serializeAnyValue(&buf, body, 0)
	got := buf.String()
	want := `{"kusto.database":"FakeDatabase","kusto.table":"FakeTable","date":"1715247667.480479","output":"09:41:07.480478650: Notice Known system binary sent/received network traffic (user=root user_loginuid=-1 connection=REDACTED container_id=host image=\u003cNA\u003e, proc=bash)","priority":"Notice","rule":"System procs network activity","source":"syscall","tags":["mitre_exfiltration","network"],"output_fields":{"container.id":"host","container.image.repository":"","evt.time":"1715247667480478650","fd.name":"REDACTED","proc.name":"bash","user.loginuid":"-1","user.name":"root"}}`
	require.Equal(t, want, got, "Want=%s, Got=%s", prettyPrintOTLPCSV(want), prettyPrintOTLPCSV(got))
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

// logrecord represents the fields we output to CSV
type logrecord struct {
	Timestamp         string
	ObservedTimestamp string
	TraceId           string
	SpanId            string
	SeverityText      string
	SeverityNumber    string
	Body              map[string]interface{}
	Resource          map[string]interface{}
	Attributes        map[string]interface{}
}

func getLogRecord(fields []string) (*logrecord, error) {
	if len(fields) != 9 {
		return nil, fmt.Errorf("expected 9 fields, got %d", len(fields))
	}

	body := map[string]interface{}{}
	if err := json.Unmarshal([]byte(fields[6]), &body); err != nil {
		return nil, err
	}

	resource := map[string]interface{}{}
	if err := json.Unmarshal([]byte(fields[7]), &resource); err != nil {
		return nil, err
	}

	attributes := map[string]interface{}{}
	if err := json.Unmarshal([]byte(fields[8]), &attributes); err != nil {
		return nil, err
	}
	return &logrecord{
		Timestamp:         fields[0],
		ObservedTimestamp: fields[1],
		TraceId:           fields[2],
		SpanId:            fields[3],
		SeverityText:      fields[4],
		SeverityNumber:    fields[5],
		Body:              body,
		Resource:          resource,
		Attributes:        attributes,
	}, nil
}

func BenchmarkProtojson(b *testing.B) {
	var log v1.ExportLogsServiceRequest
	protojson.Unmarshal(nestedRawLog, &log)

	// We're setting up a buffer because serializeKvList uses a bytes.Buffer for writing
	// the serialized format, so we want to make sure we're benchmarking apples to apples.
	var buf bytes.Buffer
	logRecord := log.ResourceLogs[0].ScopeLogs[0].LogRecords[0]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		body := logRecord.GetBody().GetKvlistValue()
		serialized, _ := protojson.Marshal(body)
		buf.Write(serialized)
	}
}

// Running tool: /usr/local/go/bin/go test -benchmem -run=^$ -bench ^BenchmarkProtojson$ github.com/Azure/adx-mon/ingestor/transform

// goos: linux
// goarch: amd64
// pkg: github.com/Azure/adx-mon/ingestor/transform
// cpu: AMD EPYC 7763 64-Core Processor
// BenchmarkProtojson-16    	   40689	     29458 ns/op	   11512 B/op	     262 allocs/op
// PASS
// ok  	github.com/Azure/adx-mon/ingestor/transform	1.514s

func BenchmarkSerializeKvList(b *testing.B) {
	var log v1.ExportLogsServiceRequest
	protojson.Unmarshal(nestedRawLog, &log)

	var buf strings.Builder
	logRecord := log.ResourceLogs[0].ScopeLogs[0].LogRecords[0]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		body := logRecord.GetBody()
		serializeAnyValue(&buf, body, 0)
	}
}

// Running tool: /usr/local/go/bin/go test -benchmem -run=^$ -bench ^BenchmarkSerializeKvList$ github.com/Azure/adx-mon/ingestor/transform

// goos: linux
// goarch: amd64
// pkg: github.com/Azure/adx-mon/ingestor/transform
// cpu: AMD EPYC 7763 64-Core Processor
// BenchmarkSerializeKvList-16    	  386356	      2707 ns/op	    2576 B/op	      43 allocs/op
// PASS
// ok  	github.com/Azure/adx-mon/ingestor/transform	1.092s

func prettyPrintOTLPCSV(s string) string {
	var sb strings.Builder
	r := csv.NewReader(strings.NewReader(s))
	records, _ := r.ReadAll()

	if len(records) != 1 {
		return "malformed record"
	}
	fields := records[0]
	if len(fields) != 9 {
		return "malformed fields"
	}

	sb.WriteString(fmt.Sprintf("CSV: %s\n", s))
	sb.WriteString(fmt.Sprintf("Timestamp: %s\n", fields[0]))
	sb.WriteString(fmt.Sprintf("ObservedTimestamp: %s\n", fields[1]))
	sb.WriteString(fmt.Sprintf("TraceId: %s\n", fields[2]))
	sb.WriteString(fmt.Sprintf("SpanId: %s\n", fields[3]))
	sb.WriteString(fmt.Sprintf("SeverityText: %s\n", fields[4]))
	sb.WriteString(fmt.Sprintf("SeverityNumber: %s\n", fields[5]))

	body := prettyPrintJSON(fields[6])
	if body == "" {
		body = fields[6]
	}
	sb.WriteString(fmt.Sprintf("Body: %s\n", body))

	resource := prettyPrintJSON(fields[7])
	sb.WriteString(fmt.Sprintf("Resource: %s\n", resource))

	attributes := prettyPrintJSON(fields[8])
	sb.WriteString(fmt.Sprintf("Attributes: %s\n", attributes))

	return sb.String()
}

func prettyPrintJSON(input string) string {
	var objmap map[string]*json.RawMessage
	err := json.Unmarshal([]byte(input), &objmap)
	if err != nil {
		return ""
	}

	pretty, err := json.MarshalIndent(objmap, "", "  ")
	if err != nil {
		return ""
	}

	return string(pretty)
}

var nestedRawLog = []byte(`{
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
								"kvlistValue": {
									"values": [
										{
											"key": "kusto.database",
											"value": {
												"stringValue": "FakeDatabase"
											}
										},
										{
											"key": "kusto.table",
											"value": {
												"stringValue": "FakeTable"
											}
										},
										{
											"key": "date",
											"value": {
												"doubleValue": 1715247667.480479
											}
										},
										{
											"key": "output",
											"value": {
												"stringValue": "09:41:07.480478650: Notice Known system binary sent/received network traffic (user=root user_loginuid=-1 connection=REDACTED container_id=host image=<NA>, proc=bash)"
											}
										},
										{
											"key": "priority",
											"value": {
												"stringValue": "Notice"
											}
										},
										{
											"key": "rule",
											"value": {
												"stringValue": "System procs network activity"
											}
										},
										{
											"key": "source",
											"value": {
												"stringValue": "syscall"
											}
										},
										{
											"key": "tags",
											"value": {
												"arrayValue": {
													"values": [
														{
															"stringValue": "mitre_exfiltration"
														},
														{
															"stringValue": "network"
														}
													]
												}
											}
										},
										{
											"key": "output_fields",
											"value": {
												"kvlistValue": {
													"values": [
														{
															"key": "container.id",
															"value": {
																"stringValue": "host"
															}
														},
														{
															"key": "container.image.repository",
															"value": {}
														},
														{
															"key": "evt.time",
															"value": {
																"intValue": "1715247667480478650"
															}
														},
														{
															"key": "fd.name",
															"value": {
																"stringValue": "REDACTED"
															}
														},
														{
															"key": "proc.name",
															"value": {
																"stringValue": "bash"
															}
														},
														{
															"key": "user.loginuid",
															"value": {
																"intValue": "-1"
															}
														},
														{
															"key": "user.name",
															"value": {
																"stringValue": "root"
															}
														}
													]
												}
											}
										}
									]
								}
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
