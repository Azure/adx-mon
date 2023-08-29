package otlp

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"sync"

	"buf.build/gen/go/opentelemetry/opentelemetry/bufbuild/connect-go/opentelemetry/proto/collector/logs/v1/logsv1connect"
	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	commonv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/common/v1"
	resourcev1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/resource/v1"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/pool"
	connect_go "github.com/bufbuild/connect-go"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/net/http2"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/proto"
)

// LogsProxyHandler implements an HTTP handler that receives OTLP logs and forwards them to an OTLP endpoint over gRPC.
func LogsProxyHandler(ctx context.Context, endpoints []string, insecureSkipVerify bool, addAttributes map[string]string, liftAttributes []string) http.HandlerFunc {

	lift := make(map[string]struct{})
	for _, attribute := range liftAttributes {
		lift[attribute] = struct{}{}
	}

	var add []*commonv1.KeyValue
	for attribute, value := range addAttributes {
		add = append(add, &commonv1.KeyValue{
			Key: attribute,
			Value: &commonv1.AnyValue{
				Value: &commonv1.AnyValue_StringValue{
					StringValue: value,
				},
			},
		},
		)
	}

	rpcClients := make(map[string]logsv1connect.LogsServiceClient)
	for _, endpoint := range endpoints {
		// We have to strip the path component from our endpoint so gRPC can correctly setup its routing
		uri, err := url.Parse(endpoint)
		if err != nil {
			logger.Fatal("Failed to parse endpoint: %v", err)
		}
		uri.Path = ""
		grpcEndpoint := uri.String()
		logger.Info("gRPC endpoint: %s", grpcEndpoint)

		// Create our HTTP2 client with optional TLS configuration
		httpClient := http.DefaultClient
		if insecureSkipVerify && uri.Scheme == "https" {
			logger.Warn("Using insecure TLS configuration")
			httpClient = &http.Client{
				Transport: &http2.Transport{
					TLSClientConfig: &tls.Config{InsecureSkipVerify: insecureSkipVerify},
				},
			}
		}
		if uri.Scheme == "http" {
			logger.Warn("Disabling TLS for HTTP endpoint")
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

		// Create our gRPC client used to upgrade from HTTP1 to HTTP2 via gRPC and proxy to the OTLP endpoint
		rpcClients[endpoint] = logsv1connect.NewLogsServiceClient(httpClient, grpcEndpoint, connect_go.WithGRPC())
	}
	bufs := pool.NewBytes(1024 * 1024)

	return func(w http.ResponseWriter, r *http.Request) {
		m := metrics.RequestsReceived.MustCurryWith(prometheus.Labels{"path": "/logs"})
		defer r.Body.Close()

		switch r.Header.Get("Content-Type") {
		case "application/x-protobuf":
			// We're receiving an OTLP protobuf, so we just need to
			// read the body, marshal into an OTLP protobuf, then
			// use gRPC to send the OTLP protobuf to the OTLP endpoint

			// Consume the request body and marshal into a protobuf
			b := bufs.Get(int(r.ContentLength))
			defer bufs.Put(b)

			n, err := io.ReadFull(r.Body, b)
			if err != nil {
				logger.Error("Failed to read request body: %v", err)
				w.WriteHeader(http.StatusInternalServerError)
				m.WithLabelValues(strconv.Itoa(http.StatusInternalServerError)).Inc()
				return
			}
			if n < int(r.ContentLength) {
				logger.Warn("Short read %d < %d", n, r.ContentLength)
				w.WriteHeader(http.StatusInternalServerError)
				m.WithLabelValues(strconv.Itoa(http.StatusInternalServerError)).Inc()
				return
			}

			msg := &v1.ExportLogsServiceRequest{}
			if err := proto.Unmarshal(b, msg); err != nil {
				logger.Error("Failed to unmarshal request body: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				m.WithLabelValues(strconv.Itoa(http.StatusBadRequest)).Inc()
				return
			}

			var numLogs int
			msg, numLogs = modifyAttributes(msg, add, lift)
			metrics.LogsProxyReceived.Add(float64(numLogs))

			// OTLP API https://opentelemetry.io/docs/specs/otlp/#otlphttp-response
			// requires us send a partial success where appropriate. We'll keep track
			// of the maximum number of failed records so the caller can make the
			// appropriate decision on whether to retry the request.
			var (
				rejectedRecords int64
				mu              sync.Mutex
			)

			// Create our RPC request and send it
			g, gctx := errgroup.WithContext(ctx)
			for endpoint, rpcClient := range rpcClients {
				endpoint := endpoint
				rpcClient := rpcClient
				g.Go(func() error {
					resp, err := rpcClient.Export(gctx, connect_go.NewRequest(msg))
					if err != nil {
						logger.Error("Failed to send request: %v", err)
						metrics.LogsProxyFailures.WithLabelValues(endpoint).Inc()
						return err
					}
					// Partial failures are left to the caller to handle. If they want at-least-once
					// delivery, they should retry the request until it succeeds.
					if partial := resp.Msg.GetPartialSuccess(); partial != nil && partial.GetRejectedLogRecords() != 0 {
						logger.Error("Partial success: %s", partial.String())
						metrics.LogsProxyPartialFailures.WithLabelValues(endpoint).Add(float64(partial.GetRejectedLogRecords()))
						mu.Lock()
						if partial.GetRejectedLogRecords() > rejectedRecords {
							rejectedRecords = partial.GetRejectedLogRecords()
						}
						mu.Unlock()
						return errors.New(partial.ErrorMessage)
					}
					return nil
				})
			}

			var respBodyBytes []byte
			if err := g.Wait(); err != nil {
				// Construct a partial success response with the maximum number of rejected records
				logger.Error("Failed to proxy request: %v", err)
				respBodyBytes, err = proto.Marshal(&v1.ExportLogsPartialSuccess{RejectedLogRecords: rejectedRecords})
				if err != nil {
					logger.Error("Failed to marshal response: %v", err)
					w.WriteHeader(http.StatusInternalServerError)
					m.WithLabelValues(strconv.Itoa(http.StatusInternalServerError)).Inc()
					return
				}
			} else {
				// The logs have been committed by the OTLP endpoint
				respBodyBytes, err = proto.Marshal(&v1.ExportLogsServiceResponse{})
				if err != nil {
					logger.Error("Failed to marshal response: %v", err)
					w.WriteHeader(http.StatusInternalServerError)
					m.WithLabelValues(strconv.Itoa(http.StatusInternalServerError)).Inc()
					return
				}
			}
			// Even in the case of a partial response, OTLP API requires us to send StatusOK
			w.Header().Add("Content-Type", "application/x-protobuf")
			w.WriteHeader(http.StatusOK)
			w.Write(respBodyBytes)

			m.WithLabelValues(strconv.Itoa(http.StatusOK)).Inc()

		case "application/json":
			// We're receiving JSON, so we need to unmarshal the JSON
			// into an OTLP protobuf, then use gRPC to send the OTLP
			// protobuf to the OTLP endpoint
			w.WriteHeader(http.StatusNotImplemented)
			m.WithLabelValues(strconv.Itoa(http.StatusNotImplemented)).Inc()

		default:
			logger.Error("Unsupported Content-Type: %s", r.Header.Get("Content-Type"))
			w.WriteHeader(http.StatusBadRequest)
			m.WithLabelValues(strconv.Itoa(http.StatusBadRequest)).Inc()
		}
	}
}

func modifyAttributes(msg *v1.ExportLogsServiceRequest, add []*commonv1.KeyValue, lift map[string]struct{}) (*v1.ExportLogsServiceRequest, int) {

	var (
		numLogs int
		ok      bool
	)
	for i := range msg.ResourceLogs {
		// Logs schema https://opentelemetry.io/docs/specs/otel/protocol/file-exporter/#examples
		// All these events have Resource in common, see https://opentelemetry.io/docs/specs/otel/logs/data-model/#field-resource,
		// since they're all coming from the same source.
		if msg.ResourceLogs[i] == nil {
			// This is a parculiar case, but it can happen if the client sends an empty request.
			break
		}
		if msg.ResourceLogs[i].Resource == nil {
			msg.ResourceLogs[i].Resource = &resourcev1.Resource{}
		}

		// Add any additional columns to the logs
		msg.ResourceLogs[i].Resource.Attributes = append(
			msg.ResourceLogs[i].Resource.Attributes,
			add...,
		)

		for j := range msg.ResourceLogs[i].ScopeLogs {
			numLogs += len(msg.ResourceLogs[i].ScopeLogs[j].LogRecords)

			// Now lift attributes from the body of the log message
			if len(lift) > 0 {
				for k := range msg.ResourceLogs[i].ScopeLogs[j].LogRecords {

					var deleted int
					for idx, v := range msg.ResourceLogs[i].ScopeLogs[j].LogRecords[k].Body.GetKvlistValue().GetValues() {
						_, ok = lift[v.GetKey()]
						if ok {
							msg.ResourceLogs[i].ScopeLogs[j].LogRecords[k].Attributes = append(
								msg.ResourceLogs[i].ScopeLogs[j].LogRecords[k].Attributes,
								v,
							)
							msg.ResourceLogs[i].ScopeLogs[j].LogRecords[k].Body.GetKvlistValue().Values = slices.Delete(
								msg.ResourceLogs[i].ScopeLogs[j].LogRecords[k].Body.GetKvlistValue().Values,
								idx-deleted, idx-deleted+1,
							)
							deleted++
						}
					}
				}
			}
		}
	}

	return msg, numLogs
}
