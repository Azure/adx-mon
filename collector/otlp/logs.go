package otlp

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"

	"buf.build/gen/go/opentelemetry/opentelemetry/bufbuild/connect-go/opentelemetry/proto/collector/logs/v1/logsv1connect"
	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/pool"
	connect_go "github.com/bufbuild/connect-go"
	"golang.org/x/net/http2"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/proto"
)

// LogsProxyHandler implements an HTTP handler that receives OTLP logs and forwards them to an OTLP endpoint over gRPC.
func LogsProxyHandler(ctx context.Context, endpoints []string, insecureSkipVerify bool) http.HandlerFunc {

	rpcClients := make([]logsv1connect.LogsServiceClient, len(endpoints))
	for i, endpoint := range endpoints {
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
		rpcClients[i] = logsv1connect.NewLogsServiceClient(httpClient, grpcEndpoint, connect_go.WithGRPC())
	}
	bufs := pool.NewBytes(1024 * 1024)

	return func(w http.ResponseWriter, r *http.Request) {
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
				return
			}
			if n < int(r.ContentLength) {
				logger.Warn("Short read %d < %d", n, r.ContentLength)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			var msg v1.ExportLogsServiceRequest
			if err := proto.Unmarshal(b, &msg); err != nil {
				logger.Error("Failed to unmarshal request body: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}

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
			for _, rpcClient := range rpcClients {
				rpcClient := rpcClient
				g.Go(func() error {
					resp, err := rpcClient.Export(gctx, connect_go.NewRequest(&msg))
					if err != nil {
						logger.Error("Failed to send request: %v", err)
						return err
					}
					// Partial failures are left to the caller to handle. If they want at-least-once
					// delivery, they should retry the request until it succeeds.
					if partial := resp.Msg.GetPartialSuccess(); partial != nil && partial.GetRejectedLogRecords() != 0 {
						logger.Error("Partial success: %s", partial.String())
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
					return
				}
			} else {
				// The logs have been committed by the OTLP endpoint
				respBodyBytes, err = proto.Marshal(&v1.ExportLogsServiceResponse{})
				if err != nil {
					logger.Error("Failed to marshal response: %v", err)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
			}
			// Even in the case of a partial response, OTLP API requires us to send StatusOK
			w.Header().Add("Content-Type", "application/x-protobuf")
			w.WriteHeader(http.StatusOK)
			w.Write(respBodyBytes)

		case "application/json":
			// We're receiving JSON, so we need to unmarshal the JSON
			// into an OTLP protobuf, then use gRPC to send the OTLP
			// protobuf to the OTLP endpoint
			w.WriteHeader(http.StatusNotImplemented)

		default:
			logger.Error("Unsupported Content-Type: %s", r.Header.Get("Content-Type"))
			w.WriteHeader(http.StatusBadRequest)
		}
	}
}
