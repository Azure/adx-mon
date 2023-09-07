package sink

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"

	"buf.build/gen/go/opentelemetry/opentelemetry/bufbuild/connect-go/opentelemetry/proto/collector/logs/v1/logsv1connect"
	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	commonv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/common/v1"
	logsv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/logs/v1"
	"github.com/Azure/adx-mon/collector/logs"
	"github.com/Azure/adx-mon/pkg/logger"
	connect_go "github.com/bufbuild/connect-go"
	"golang.org/x/net/http2"
)

type OLTPLogSink struct {
	client logsv1connect.LogsServiceClient
}

func NewOLTPLogSink(endpoint string, insecureSkipVerify bool) (*OLTPLogSink, error) {
	uri, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("NewOLTPLogSink: %w", err)
	}
	uri.Path = ""
	grpcEndpoint := uri.String()
	logger.Infof("gRPC endpoint: %s", grpcEndpoint)

	// Create our HTTP2 client with optional TLS configuration
	httpClient := http.DefaultClient
	if insecureSkipVerify && uri.Scheme == "https" {
		logger.Warnf("Using insecure TLS configuration")
		httpClient = &http.Client{
			Transport: &http2.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: insecureSkipVerify},
			},
		}
	}
	if uri.Scheme == "http" {
		logger.Warnf("Disabling TLS for HTTP endpoint")
		httpClient = &http.Client{
			Transport: &http2.Transport{
				AllowHTTP: true,
				DialTLS: func(network, addr string, _ *tls.Config) (net.Conn, error) {
					return net.Dial(network, addr)
				},
				TLSClientConfig: &tls.Config{InsecureSkipVerify: insecureSkipVerify},
			},
		}
	}
	client := logsv1connect.NewLogsServiceClient(httpClient, grpcEndpoint, connect_go.WithGRPC())
	return &OLTPLogSink{client: client}, nil
}

func (s *OLTPLogSink) Open(ctx context.Context) error {
	return nil
}

// TODO this interface should include some kind of async operation, and a way to know if it worked.
func (s *OLTPLogSink) Send(ctx context.Context, batch *logs.LogBatch) error {
	resourceLogs := logbatchToResourceLogs(batch)

	req := &v1.ExportLogsServiceRequest{
		ResourceLogs: resourceLogs,
	}

	resp, err := s.client.Export(ctx, connect_go.NewRequest(req))
	if err != nil {
		return fmt.Errorf("Send: %w", err)
	}
	if resp.Msg.GetPartialSuccess() != nil {
		return fmt.Errorf("Send: %s", resp.Msg.GetPartialSuccess().GetErrorMessage())
	}
	return nil
}

func (s *OLTPLogSink) Close() error {
	// TODO
	return nil
}

func logbatchToResourceLogs(batch *logs.LogBatch) []*logsv1.ResourceLogs {
	// TODO pool
	logRecords := make([]*logsv1.LogRecord, len(batch.Logs))
	// TODO - right now ingestor just reads the "string" value of the body, but we should support other types
	for i, log := range batch.Logs {
		serializedBody, err := json.Marshal(log.Body)
		if err != nil {
			continue
			// TODO log
		}
		logRecords[i] = &logsv1.LogRecord{
			TimeUnixNano:         log.Timestamp,
			ObservedTimeUnixNano: log.ObservedTimestamp,
			Body:                 &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: string(serializedBody)}},
			Attributes:           attributesToKV(log.Attributes),
		}
	}

	scopeLogs := []*logsv1.ScopeLogs{
		{
			LogRecords: logRecords,
		},
	}
	resourceLogs := &logsv1.ResourceLogs{
		ScopeLogs: scopeLogs,
	}

	// TODO Silly allocation
	return []*logsv1.ResourceLogs{resourceLogs}
}

func attributesToKV(attributes map[string]any) []*commonv1.KeyValue {
	// TODO pool
	keyValueList := make([]*commonv1.KeyValue, len(attributes))
	i := 0
	for k, v := range attributes {
		var value *commonv1.AnyValue
		switch v := v.(type) {
		case string:
			value = &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: v}}
		case int:
			value = &commonv1.AnyValue{Value: &commonv1.AnyValue_IntValue{IntValue: int64(v)}}
		case int64:
			value = &commonv1.AnyValue{Value: &commonv1.AnyValue_IntValue{IntValue: v}}
		case float64:
			value = &commonv1.AnyValue{Value: &commonv1.AnyValue_DoubleValue{DoubleValue: v}}
		case bool:
			value = &commonv1.AnyValue{Value: &commonv1.AnyValue_BoolValue{BoolValue: v}}
		case []byte:
			value = &commonv1.AnyValue{Value: &commonv1.AnyValue_BytesValue{BytesValue: v}}
		default:
			// TODO - handle arrays and maps better.
			serialized, err := json.Marshal(v)
			if err != nil {
				// TODO log
				continue
			}
			value = &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: string(serialized)}}
		}

		keyValueList[i] = &commonv1.KeyValue{
			Key:   k,
			Value: value,
		}
		i++
	}
	return keyValueList
}
