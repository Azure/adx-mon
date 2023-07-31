package otlp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"buf.build/gen/go/opentelemetry/opentelemetry/bufbuild/connect-go/opentelemetry/proto/collector/logs/v1/logsv1connect"
	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	"github.com/Azure/adx-mon/ingestor/otlp"
	"github.com/bufbuild/connect-go"
)

func TestOTLP(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle(logsv1connect.NewLogsServiceHandler(otlp.NewLogsServer()))

	srv := httptest.NewUnstartedServer(mux)
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()

	client := logsv1connect.NewLogsServiceClient(srv.Client(), srv.URL, connect.WithGRPC())
	resp, err := client.Export(context.Background(), connect.NewRequest(&v1.ExportLogsServiceRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Msg.GetPartialSuccess() != nil {
		t.Fatal("Did not expect a partial success")
	}
}
