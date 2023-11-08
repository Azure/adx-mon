package otlp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"time"

	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	commonv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/common/v1"
	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/ingestor/transform"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/otlp"
	"github.com/Azure/adx-mon/pkg/pool"
	"github.com/Azure/adx-mon/pkg/tlv"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/adx-mon/pkg/wal/file"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/proto"
)

var (
	csvWriterPool = pool.NewGeneric(1000, func(sz int) interface{} {
		return transform.NewCSVWriter(bytes.NewBuffer(make([]byte, 0, sz)), nil)
	})
)

// LogsTransferHandler implements an HTTP handler that receives OTLP logs, marshals them as CSV to our wal and transfers them to HTTP endpoins (e.g. Ingestor).
func LogsTransferHandler(ctx context.Context, endpoints []string, insecureSkipVerify bool, addAttributes map[string]string) http.HandlerFunc {

	log := slog.Default().With(
		slog.Group(
			"handler",
			slog.String("protocol", "otlp"),
		),
	)

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

	mp := &file.MemoryProvider{}
	rpcClients := make(map[string]*cluster.Client)
	for _, endpoint := range endpoints {
		uri, err := url.Parse(endpoint)
		if err != nil {
			logger.Fatalf("Failed to parse endpoint: %v", err)
		}
		uri.Path = "/transfer"
		if logger.IsDebug() {
			logger.Debugf("Adding endpoint: %s", uri.String())
		}

		client, _ := cluster.NewClient(time.Minute, insecureSkipVerify, mp)
		rpcClients[uri.String()] = client
	}
	bufs := pool.NewBytes(1024 * 1024)

	return func(w http.ResponseWriter, r *http.Request) {
		m := metrics.RequestsReceived.MustCurryWith(prometheus.Labels{"path": "/v1/logs"})
		defer r.Body.Close()

		switch r.Header.Get("Content-Type") {
		case "application/x-protobuf":

			// Consume the request body and marshal into a protobuf
			b := bufs.Get(int(r.ContentLength))
			defer bufs.Put(b)

			n, err := io.ReadFull(r.Body, b)
			if err != nil {
				log.Error("Failed to read request body", "Error", err)
				w.WriteHeader(http.StatusInternalServerError)
				m.WithLabelValues(strconv.Itoa(http.StatusInternalServerError)).Inc()
				return
			}
			if n < int(r.ContentLength) {
				log.Warn("Short read")
				w.WriteHeader(http.StatusBadRequest)
				m.WithLabelValues(strconv.Itoa(http.StatusBadRequest)).Inc()
				return
			}
			if logger.IsDebug() {
				log.Debug("Received request body", "Bytes", n)
			}

			msg := &v1.ExportLogsServiceRequest{}
			if err := proto.Unmarshal(b, msg); err != nil {
				log.Error("Failed to unmarshal request body", "Error", err)
				w.WriteHeader(http.StatusBadRequest)
				m.WithLabelValues(strconv.Itoa(http.StatusBadRequest)).Inc()
				return
			}

			// Serialize the logs into a WAL, grouped by Kusto destination database and table
			serialized, err := serialize(ctx, msg, add, mp, log)
			defer func() {
				// Clean up the WALs
				for _, s := range serialized {
					if err := mp.Remove(s.Path); err != nil {
						log.Error("Failed to remove WAL", "Filename", s.Path, "Error", err)
					}
				}
			}()
			if isUserError(err) {
				log.Error("Failed to serialize logs with user error", "Error", err)
				w.WriteHeader(http.StatusBadRequest)
				m.WithLabelValues(strconv.Itoa(http.StatusBadRequest)).Inc()
				return
			}
			if err != nil {
				log.Error("Failed to serialize logs with internal error", "Error", err)
				w.WriteHeader(http.StatusInternalServerError)
				m.WithLabelValues(strconv.Itoa(http.StatusInternalServerError)).Inc()
				return
			}

			// Create our RPC request and send it
			g, gctx := errgroup.WithContext(ctx)
			for endpoint, rpcClient := range rpcClients {
				endpoint := endpoint
				rpcClient := rpcClient
				g.Go(func() error {
					for _, s := range serialized {
						f, err := mp.Open(s.Path)
						if err != nil {
							log.Error("Failed to open WAL", "Filename", s, "Error", err)
							return err
						}
						defer f.Close()

						filename := filepath.Base(s.Path)

						if err := rpcClient.Write(gctx, endpoint, filename, f); err != nil {
							log.Error("Failed to send logs", "Endpoint", endpoint, "Error", err)
							return err
						}
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
				log.Error("Failed to proxy request", "Error", err)
				statusCode = httpStatusCodeForGRPCError(err)
				respBodyBytes, err = proto.Marshal(&v1.ExportLogsPartialSuccess{RejectedLogRecords: int64(len(msg.GetResourceLogs()))})
				if err != nil {
					log.Error("Failed to marshal response", "Error", err)
					w.WriteHeader(http.StatusInternalServerError)
					m.WithLabelValues(strconv.Itoa(http.StatusInternalServerError)).Inc()
					return
				}
			} else {
				// The logs have been committed by the OTLP endpoint
				respBodyBytes, err = proto.Marshal(&v1.ExportLogsServiceResponse{})
				statusCode = http.StatusOK
				if err != nil {
					log.Error("Failed to marshal response", "Error", err)
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

			// We only want to increment the number of logs sent if we
			// are returning a successful response to the client
			if statusCode == http.StatusOK {
				for _, s := range serialized {
					metrics.LogsProxyUploaded.WithLabelValues(s.Database, s.Table).Add(float64(s.Logs))
				}
			}

		case "application/json":
			// We're receiving JSON, so we need to unmarshal the JSON
			// into an OTLP protobuf, then use gRPC to send the OTLP
			// protobuf to the OTLP endpoint
			w.WriteHeader(http.StatusUnsupportedMediaType)
			m.WithLabelValues(strconv.Itoa(http.StatusUnsupportedMediaType)).Inc()

		default:
			logger.Errorf("Unsupported Content-Type: %s", r.Header.Get("Content-Type"))
			w.WriteHeader(http.StatusUnsupportedMediaType)
			m.WithLabelValues(strconv.Itoa(http.StatusUnsupportedMediaType)).Inc()
		}
	}
}

type serialized struct {
	Path     string
	Database string
	Table    string
	Logs     int
}

func serialize(ctx context.Context, req *v1.ExportLogsServiceRequest, add []*commonv1.KeyValue, mp *file.MemoryProvider, log *slog.Logger) ([]serialized, error) {
	var s []serialized

	enc := csvWriterPool.Get(8 * 1024).(*transform.CSVWriter)
	defer csvWriterPool.Put(enc)

	grouped := otlp.Group(req, add, log)
	for _, group := range grouped {
		metrics.LogsProxyReceived.WithLabelValues(group.Database, group.Table).Add(float64(len(group.Logs)))
		w, err := wal.NewWAL(wal.WALOpts{StorageProvider: mp, Prefix: fmt.Sprintf("%s_%s", group.Database, group.Table)})
		if err != nil {
			return nil, fmt.Errorf("failed to create wal: %w", err)
		}
		if err := w.Open(ctx); err != nil {
			return nil, fmt.Errorf("failed to open wal: %w", err)
		}
		enc.Reset()
		if err := enc.MarshalCSV(group); err != nil {
			return nil, fmt.Errorf("failed to marshal csv: %w", err)
		}
		// Add our TLV metadata
		b := enc.Bytes()
		tNumLogs := tlv.New(otlp.LogsTotalTag, []byte(strconv.Itoa(len(group.Logs))))
		tPayloadSize := tlv.New(tlv.PayloadTag, []byte(strconv.Itoa(len(b))))
		if err := w.Write(ctx, append(tlv.Encode(tNumLogs, tPayloadSize), b...)); err != nil {
			return nil, fmt.Errorf("failed to write to wal: %w", err)
		}
		s = append(s, serialized{Path: w.Path(), Database: group.Database, Table: group.Table, Logs: len(group.Logs)})
		if err := w.Close(); err != nil {
			return nil, fmt.Errorf("failed to close wal: %w", err)
		}
	}

	return s, nil
}

var (
	ErrMissingKustoMetadata = errors.New("missing kusto metadata")
	ErrMalformedLogs        = errors.New("malformed log records")
)

func isUserError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, ErrMissingKustoMetadata) {
		return true
	}

	if errors.Is(err, ErrMalformedLogs) {
		return true
	}

	return false
}
