package otlp

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"buf.build/gen/go/opentelemetry/opentelemetry/bufbuild/connect-go/opentelemetry/proto/collector/trace/v1/tracev1connect"
	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/trace/v1"
	connect "github.com/bufbuild/connect-go"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func TestTraceHttp(t *testing.T) {
	httpPath := "/v1/traces"

	// Creates a TraceService protobuf from a JSON representation
	var msg v1.ExportTraceServiceRequest
	require.NoError(t, protojson.Unmarshal(trace, &msg))

	// Serializes the protobuf to a byte slice
	b, err := proto.Marshal(&msg)
	require.NoError(t, err)

	// Creates a new TraceService so we can test its HTTP handler
	traceSvc := NewTraceService(TraceServiceOpts{
		Path: httpPath,
	})

	// Creates a new HTTP request with the serialized protobuf as the body
	req, err := http.NewRequest("POST", httpPath, bytes.NewReader(b))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-protobuf")

	// Now test our response. Note, this test will be expanded upon as the
	// handler's functionality is completed.
	resp := httptest.NewRecorder()
	traceSvc.Handler(resp, req)
	require.Equal(t, http.StatusNotImplemented, resp.Code)
}

func TestTraceGrpc(t *testing.T) {
	path := "/v1/traces"

	// Creates a new TraceService so we can test its gRPC handler
	traceSvc := NewTraceService(TraceServiceOpts{
		Path: path,
	})

	// Creates a new HTTP server with the TraceService's gRPC handler
	mux := http.NewServeMux()
	mux.Handle(tracev1connect.NewTraceServiceHandler(traceSvc))

	srv := httptest.NewUnstartedServer(mux)
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()

	// Creates a TraceService protobuf from a JSON representation
	var msg v1.ExportTraceServiceRequest
	require.NoError(t, protojson.Unmarshal(trace, &msg))

	// Creates a new gRPC client to test the gRPC handler
	client := tracev1connect.NewTraceServiceClient(srv.Client(), srv.URL, connect.WithGRPC())
	_, err := client.Export(context.Background(), connect.NewRequest(&msg))
	require.NoError(t, err)
}

// https://github.com/open-telemetry/opentelemetry-proto/blob/main/examples/trace.json
var trace = []byte(`{
	"resourceSpans": [
	  {
		"resource": {
		  "attributes": [
			{
			  "key": "service.name",
			  "value": {
				"stringValue": "my.service"
			  }
			}
		  ]
		},
		"scopeSpans": [
		  {
			"scope": {
			  "name": "my.library",
			  "version": "1.0.0",
			  "attributes": [
				{
				  "key": "my.scope.attribute",
				  "value": {
					"stringValue": "some scope attribute"
				  }
				}
			  ]
			},
			"spans": [
			  {
				"traceId": "5B8EFFF798038103D269B633813FC60C",
				"spanId": "EEE19B7EC3C1B174",
				"parentSpanId": "EEE19B7EC3C1B173",
				"name": "I'm a server span",
				"startTimeUnixNano": "1544712660000000000",
				"endTimeUnixNano": "1544712661000000000",
				"kind": 2,
				"attributes": [
				  {
					"key": "my.span.attr",
					"value": {
					  "stringValue": "some value"
					}
				  }
				]
			  }
			]
		  }
		]
	  }
	]
  }`)
