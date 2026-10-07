// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package adx

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/Azure/adx-mon/pkg/testutils/kustainer"
	"github.com/Azure/adx-mon/schema"
	"github.com/Azure/azure-kusto-go/azkustodata"
	kustoerrors "github.com/Azure/azure-kusto-go/azkustodata/errors"
	azkustoingest "github.com/Azure/azure-kusto-go/azkustoingest"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

// newStreamingHTTPError builds an error the same way the SDK does for a failed streaming request: the HTTP error is
// formatted into a string by Conn.StreamIngest.
func newStreamingHTTPError(status string, code int, body string) error {
	inner := kustoerrors.HTTP(kustoerrors.OpIngestStream, status, code, io.NopCloser(strings.NewReader(body)),
		"error from Kusto endpoint, With db: Metrics, table: CpuUsage, mappingName: CpuUsage_123, clientRequestId: KGC.executeStreaming;8d1b")
	return kustoerrors.ES(kustoerrors.OpIngestStream, kustoerrors.KHTTPError, "streaming ingestion failed: endpoint(%s): %s",
		"https://cluster.kusto.windows.net/v1/rest/ingest/Metrics/CpuUsage?streamFormat=Csv", inner)
}

func kustoBody(code, message string) string {
	return fmt.Sprintf(`{"error":{"code":"BadRequest","message":"Request is invalid and cannot be executed.","@type":"Kusto.Common.Svc.Exceptions.%s","@message":%q,"@permanent":true}}`, code, message)
}

func TestClassifyStreamingError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want streamingFailure
	}{
		{name: "nil", err: nil, want: streamingRetry},
		{name: "context canceled", err: context.Canceled, want: streamingRetry},
		{name: "context deadline", err: fmt.Errorf("upload: %w", context.DeadlineExceeded), want: streamingRetry},
		{
			name: "network error",
			err: kustoerrors.ES(kustoerrors.OpIngestStream, kustoerrors.KHTTPError, "streaming ingestion failed: endpoint(%s): %s", "https://c",
				kustoerrors.E(kustoerrors.OpIngestStream, kustoerrors.KHTTPError, fmt.Errorf("dial tcp 10.0.0.1:443: connect: connection refused"))),
			want: streamingRetry,
		},
		{name: "throttled status", err: newStreamingHTTPError("429 Too Many Requests", 429, `{"error":{"code":"Too many requests"}}`), want: streamingThrottled},
		{name: "throttled code", err: newStreamingHTTPError("400 Bad Request", 400, kustoBody("General_ThrottledIngestion", "Throttled ingestion")), want: streamingThrottled},
		{name: "too large status", err: newStreamingHTTPError("413 Request Entity Too Large", 413, ""), want: streamingTooLarge},
		{name: "too large code", err: newStreamingHTTPError("400 Bad Request", 400, kustoBody("Stream_InputStreamTooLarge", "Input stream too large")), want: streamingTooLarge},
		{
			name: "streaming policy disabled",
			err:  newStreamingHTTPError("400 Bad Request", 400, kustoBody("StreamingIngestionPolicyNotEnabledException", "Streaming ingestion policy is not enabled for the table")),
			want: streamingUnavailable,
		},
		{
			name: "streaming maintenance",
			err:  newStreamingHTTPError("503 Service Unavailable", 503, kustoBody("ServiceUnavailable_StreamingIngestionDatabaseUnderMaintenance", "Database under maintenance")),
			want: streamingUnavailable,
		},
		{name: "streaming unavailable 503", err: newStreamingHTTPError("503 Service Unavailable", 503, `{"error":{"@message":"Streaming ingestion is disabled for the cluster"}}`), want: streamingUnavailable},
		{name: "mapping not found", err: newStreamingHTTPError("400 Bad Request", 400, kustoBody("BadRequest_MappingReferenceWasNotFound", "Mapping reference was not found")), want: streamingUnavailable},
		{name: "table not found", err: newStreamingHTTPError("404 Not Found", 404, `{"error":{"@message":"Table 'CpuUsage' was not found"}}`), want: streamingUnavailable},
		{name: "forbidden", err: newStreamingHTTPError("403 Forbidden", 403, `{"error":{"@message":"Principal is not authorized"}}`), want: streamingUnavailable},
		{name: "bad data", err: newStreamingHTTPError("400 Bad Request", 400, kustoBody("Stream_WrongNumberOfFields", "Inconsistent number of fields")), want: streamingPermanent},
		{name: "server error", err: newStreamingHTTPError("500 Internal Server Error", 500, kustoBody("General_InternalServerError", "Internal error")), want: streamingRetry},
		{name: "unavailable without streaming", err: newStreamingHTTPError("503 Service Unavailable", 503, `{"error":{"@message":"Service is busy"}}`), want: streamingRetry},
		{name: "gateway timeout", err: newStreamingHTTPError("504 Gateway Timeout", 504, ""), want: streamingRetry},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, classifyStreamingError(tt.err), "%v", tt.err)
		})
	}
}

func TestClassifyStreamingError_IgnoresStreamingInRequestContext(t *testing.T) {
	// The SDK's message always mentions streaming and the request id contains "executeStreaming", so a generic bad
	// request must not be mistaken for streaming being unavailable.
	err := newStreamingHTTPError("400 Bad Request", 400, `{"error":{"@message":"Bad request"}}`)
	require.Contains(t, strings.ToLower(err.Error()), "streaming")
	require.Equal(t, streamingPermanent, classifyStreamingError(err))
}

func TestKustoResponse(t *testing.T) {
	err := newStreamingHTTPError("429 Too Many Requests", 429, `{"error":{"@message":"slow down"}}`)
	status, body := kustoResponse(err.Error())
	require.Equal(t, 429, status)
	require.Contains(t, body, "slow down")
	require.NotContains(t, body, "executeStreaming")

	status, body = kustoResponse("some other error")
	require.Zero(t, status)
	require.Empty(t, body)
}

func TestStreamingFailureString(t *testing.T) {
	for f, want := range map[streamingFailure]string{
		streamingRetry:       "retry",
		streamingThrottled:   "throttled",
		streamingTooLarge:    "too_large",
		streamingUnavailable: "unavailable",
		streamingPermanent:   "permanent",
		streamingFailure(99): "unknown",
	} {
		require.Equal(t, want, f.String())
	}
}

func TestClassifyStreamingError_Kustainer(t *testing.T) {
	testutils.IntegrationTest(t)

	ctx := context.Background()
	k, err := kustainer.Run(ctx, "mcr.microsoft.com/azuredataexplorer/kustainer-linux:latest", kustainer.WithStarted())
	testcontainers.CleanupContainer(t, k)
	require.NoError(t, err)

	client, err := azkustodata.New(azkustodata.NewConnectionStringBuilder(k.ConnectionUrl()))
	require.NoError(t, err)
	defer client.Close()
	s := NewSyncer(client, "NetDefaultDB", schema.DefaultMetricsMapping, PromMetrics)
	require.NoError(t, s.EnsureTable("CpuUsage", schema.DefaultMetricsMapping))

	streaming, err := azkustoingest.NewStreaming(azkustodata.NewConnectionStringBuilder(k.ConnectionUrl()),
		azkustoingest.WithDefaultDatabase("NetDefaultDB"), azkustoingest.WithDefaultTable("CpuUsage"))
	require.NoError(t, err)
	defer streaming.Close()

	// A missing mapping is reported by the engine as an entity not found error, as when a new mapping has not yet
	// propagated to streaming ingestion.
	_, err = streaming.FromReader(ctx, strings.NewReader("2024-01-01T00:00:00Z,1,{},1.0\n"),
		azkustoingest.IngestionMappingRef("MissingMapping", azkustoingest.CSV))
	require.Error(t, err)
	status, _ := kustoResponse(err.Error())
	require.Equal(t, 400, status)
	require.Equal(t, streamingUnavailable, classifyStreamingError(err))
}
