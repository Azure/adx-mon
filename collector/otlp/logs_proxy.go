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
	"github.com/Azure/adx-mon/pkg/otlp"
	connect_go "github.com/bufbuild/connect-go"
	gbp "github.com/libp2p/go-buffer-pool"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/net/http2"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/proto"
)

type LogsProxyServiceOpts struct {
	AddAttributes      map[string]string
	LiftAttributes     []string
	Endpoint           string
	InsecureSkipVerify bool
	HealthChecker      interface{ IsHealthy() bool }
}

type LogsProxyService struct {
	staticAttributes []*commonv1.KeyValue
	clients          map[string]logsv1connect.LogsServiceClient
	liftAttributes   map[string]struct{}
	healthChecker    interface{ IsHealthy() bool }
}

func NewLogsProxyService(opts LogsProxyServiceOpts) *LogsProxyService {
	lift := make(map[string]struct{})
	for _, attribute := range opts.LiftAttributes {
		lift[attribute] = struct{}{}
	}

	var staticAttributes []*commonv1.KeyValue
	for attribute, value := range opts.AddAttributes {
		staticAttributes = append(staticAttributes, &commonv1.KeyValue{
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
	if opts.Endpoint != "" {
		endpoint := opts.Endpoint
		// We have to strip the path component from our endpoint so gRPC can correctly setup its routing
		uri, err := url.Parse(endpoint)
		if err != nil {
			logger.Fatalf("Failed to parse endpoint: %v", err)
		}
		uri.Path = ""
		grpcEndpoint := uri.String()
		logger.Infof("gRPC endpoint: %s", grpcEndpoint)

		// Create our HTTP2 client with optional TLS configuration
		httpClient := http.DefaultClient
		if opts.InsecureSkipVerify && uri.Scheme == "https" {
			logger.Warnf("Using insecure TLS configuration")
			httpClient = &http.Client{
				Transport: &http2.Transport{
					TLSClientConfig: &tls.Config{InsecureSkipVerify: opts.InsecureSkipVerify},
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
					TLSClientConfig: &tls.Config{InsecureSkipVerify: opts.InsecureSkipVerify},
				},
			}
		}

		// Create our gRPC client used to upgrade from HTTP1 to HTTP2 via gRPC and proxy to the OTLP endpoint
		rpcClients[endpoint] = logsv1connect.NewLogsServiceClient(httpClient, grpcEndpoint, connect_go.WithGRPC())
	}

	return &LogsProxyService{
		staticAttributes: staticAttributes,
		liftAttributes:   lift,
		clients:          rpcClients,
		healthChecker:    opts.HealthChecker,
	}
}

func (s *LogsProxyService) Close() error {
	return nil
}

func (s *LogsProxyService) Open(_ context.Context) error {
	return nil
}

func (s *LogsProxyService) Handler(w http.ResponseWriter, r *http.Request) {
	m := metrics.RequestsReceived.MustCurryWith(prometheus.Labels{"path": "/logs"})
	defer r.Body.Close()

	if !s.healthChecker.IsHealthy() {
		m.WithLabelValues(strconv.Itoa(http.StatusTooManyRequests)).Inc()
		http.Error(w, "Overloaded. Retry later", http.StatusTooManyRequests)
		return
	}

	switch r.Header.Get("Content-Type") {
	case "application/x-protobuf":
		// We're receiving an OTLP protobuf, so we just need to
		// read the body, marshal into an OTLP protobuf, then
		// use gRPC to send the OTLP protobuf to the OTLP endpoint

		// Consume the request body and marshal into a protobuf
		b := gbp.Get(int(r.ContentLength))
		defer gbp.Put(b)

		n, err := io.ReadFull(r.Body, b)
		if err != nil {
			logger.Errorf("Failed to read request body: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			m.WithLabelValues(strconv.Itoa(http.StatusInternalServerError)).Inc()
			return
		}
		if n < int(r.ContentLength) {
			logger.Warnf("Short read %d < %d", n, r.ContentLength)
			w.WriteHeader(http.StatusInternalServerError)
			m.WithLabelValues(strconv.Itoa(http.StatusInternalServerError)).Inc()
			return
		}
		b = b[:n]

		msg := &v1.ExportLogsServiceRequest{}
		if err := proto.Unmarshal(b, msg); err != nil {
			logger.Errorf("Failed to unmarshal request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			m.WithLabelValues(strconv.Itoa(http.StatusBadRequest)).Inc()
			return
		}

		msg, err = modifyAttributes(msg, s.staticAttributes, s.liftAttributes)
		if err != nil {
			logger.Errorf("Failed to modify attributes: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			m.WithLabelValues(strconv.Itoa(http.StatusBadRequest)).Inc()
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
		g, gctx := errgroup.WithContext(r.Context())
		for endpoint, rpcClient := range s.clients {
			endpoint := endpoint
			rpcClient := rpcClient
			g.Go(func() error {
				resp, err := rpcClient.Export(gctx, connect_go.NewRequest(msg))
				if err != nil {
					logger.Errorf("Failed to send request: %v", err)
					metrics.LogsProxyFailures.WithLabelValues(endpoint).Inc()
					return err
				}
				// Partial failures are left to the caller to handle. If they want at-least-once
				// delivery, they should retry the request until it succeeds.
				if partial := resp.Msg.GetPartialSuccess(); partial != nil && partial.GetRejectedLogRecords() != 0 {
					logger.Errorf("Partial success: %s", partial.String())
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

		var (
			respBodyBytes []byte
			statusCode    int
		)
		if err := g.Wait(); err != nil {
			// Construct a partial success response with the maximum number of rejected records
			logger.Errorf("Failed to proxy request: %v", err)
			statusCode = httpStatusCodeForGRPCError(err)
			respBodyBytes, err = proto.Marshal(&v1.ExportLogsPartialSuccess{RejectedLogRecords: rejectedRecords})
			if err != nil {
				logger.Errorf("Failed to marshal response: %v", err)
				w.WriteHeader(http.StatusInternalServerError)
				m.WithLabelValues(strconv.Itoa(http.StatusInternalServerError)).Inc()
				return
			}
		} else {
			// The logs have been committed by the OTLP endpoint
			respBodyBytes, err = proto.Marshal(&v1.ExportLogsServiceResponse{})
			statusCode = http.StatusOK
			if err != nil {
				logger.Errorf("Failed to marshal response: %v", err)
				w.WriteHeader(http.StatusInternalServerError)
				m.WithLabelValues(strconv.Itoa(http.StatusInternalServerError)).Inc()
				return
			}
		}
		// Even in the case of a partial response, OTLP API requires us to send StatusOK
		w.Header().Add("Content-Type", "application/x-protobuf")
		w.WriteHeader(statusCode)
		w.Write(respBodyBytes)

		m.WithLabelValues(strconv.Itoa(statusCode)).Inc()

	case "application/json":
		// We're receiving JSON, so we need to unmarshal the JSON
		// into an OTLP protobuf, then use gRPC to send the OTLP
		// protobuf to the OTLP endpoint
		w.WriteHeader(http.StatusNotImplemented)
		m.WithLabelValues(strconv.Itoa(http.StatusNotImplemented)).Inc()

	default:
		logger.Errorf("Unsupported Content-Type: %s", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusBadRequest)
		m.WithLabelValues(strconv.Itoa(http.StatusBadRequest)).Inc()
	}
}

func modifyAttributes(msg *v1.ExportLogsServiceRequest, add []*commonv1.KeyValue, lift map[string]struct{}) (*v1.ExportLogsServiceRequest, error) {

	var (
		ok              bool
		database, table string
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
			msg.ResourceLogs[i].Resource = &resourcev1.Resource{
				Attributes: []*commonv1.KeyValue{},
			}
		}

		// Add any additional columns to the logs
		msg.ResourceLogs[i].Resource.Attributes = append(
			msg.ResourceLogs[i].Resource.Attributes,
			add...,
		)

		for j := range msg.ResourceLogs[i].ScopeLogs {

			for k := range msg.ResourceLogs[i].ScopeLogs[j].LogRecords {

				// Now lift attributes from the body of the log message
				if len(lift) > 0 {
					logRecord := msg.ResourceLogs[i].ScopeLogs[j].LogRecords[k]
					if logRecord.Body.GetKvlistValue() != nil {
						logRecord.Body.GetKvlistValue().Values = slices.DeleteFunc(logRecord.Body.GetKvlistValue().GetValues(), func(val *commonv1.KeyValue) bool {
							_, ok = lift[val.GetKey()]
							if ok {
								logRecord.Attributes = append(
									logRecord.Attributes,
									val,
								)
							}
							return ok
						})
					}
				}

				// We want to prevent sending logs to Ingestor that will ultimately be rejected due to lack
				// of routable metadata. While it might seem wasteful to perform this check on every log,
				// we're at least ammortizing the cost at the Collector level instead of in the hot-path
				// at Ingestor.
				database, table = otlp.KustoMetadata(msg.ResourceLogs[i].ScopeLogs[j].LogRecords[k])
				if database == "" || table == "" {
					return msg, errors.New("log is missing routing attributes")
				}
				metrics.LogsProxyReceived.WithLabelValues(database, table).Inc()
				metrics.LogKeys.WithLabelValues(database, table).Set(float64(len(msg.ResourceLogs[i].ScopeLogs[j].LogRecords[k].Body.GetKvlistValue().GetValues())))
				metrics.LogSize.WithLabelValues(database, table).Set(float64(proto.Size(msg.ResourceLogs[i].ScopeLogs[j].LogRecords[k].Body)))
			}
		}
	}

	return msg, nil
}

func httpStatusCodeForGRPCError(err error) int {
	// Differentiate between retryable and non-retryable errors
	// https://opentelemetry.io/docs/specs/otlp/#failures-1

	var connectErr *connect_go.Error
	if errors.As(err, &connectErr) {
		switch connectErr.Code() {
		case connect_go.CodeInvalidArgument:
			return http.StatusBadRequest
		case connect_go.CodeDataLoss:
			// while it might seem more natural to return a 500 here, the OTLP spec
			// states that only 502, 503 and 504 are to be interpretted as retryable.
			return http.StatusServiceUnavailable
		}
	}
	// Unknown error, so we'll return a generic 500
	return http.StatusInternalServerError
}
