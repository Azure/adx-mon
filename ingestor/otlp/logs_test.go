package otlp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"buf.build/gen/go/opentelemetry/opentelemetry/bufbuild/connect-go/opentelemetry/proto/collector/logs/v1/logsv1connect"
	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	v11 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/common/v1"
	resourcev1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/logs/v1"
	"github.com/Azure/adx-mon/ingestor/adx"
	"github.com/Azure/adx-mon/ingestor/otlp"
	"github.com/bufbuild/connect-go"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
)

func TestOTLP(t *testing.T) {
	dir := t.TempDir()
	svc, err := otlp.NewLogsService(otlp.LogsServiceOpts{
		StorageDir: dir,
		K8sCli:     fake.NewSimpleClientset(),
		Uploaders: []adx.Uploader{
			adx.NewFakeUploader(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle(svc.HandleReceive())

	srv := httptest.NewUnstartedServer(mux)
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()

	client := logsv1connect.NewLogsServiceClient(srv.Client(), srv.URL, connect.WithGRPC())
	resp, err := client.Export(context.Background(), connect.NewRequest(&v1.ExportLogsServiceRequest{
		ResourceLogs: []*resourcev1.ResourceLogs{
			{
				ScopeLogs: []*resourcev1.ScopeLogs{
					{
						LogRecords: []*resourcev1.LogRecord{
							{
								Attributes: []*v11.KeyValue{
									{
										Key:   "kusto.table",
										Value: &v11.AnyValue{Value: &v11.AnyValue_StringValue{StringValue: "fake"}},
									},
									{
										Key:   "kusto.database",
										Value: &v11.AnyValue{Value: &v11.AnyValue_StringValue{StringValue: "fake"}},
									},
								},
								Body: &v11.AnyValue{Value: &v11.AnyValue_StringValue{StringValue: `{"message": "hello world"}`}},
							},
						},
					},
				},
			},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Msg.GetPartialSuccess() != nil {
		t.Fatal("Did not expect a partial success")
	}

	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}

	var totalWals int
	err = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if strings.HasSuffix(path, ".wal") {
			totalWals++
			info, err := d.Info()
			if err != nil {
				return err
			}
			require.NotEqual(t, 0, info.Size())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	require.Equal(t, 1, totalWals)
}
