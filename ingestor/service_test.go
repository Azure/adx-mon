package ingestor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Azure/adx-mon/pkg/otlp"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/stretchr/testify/require"
)

type fakeHealthChecker struct {
	healthy bool
}

func (f *fakeHealthChecker) IsHealthy() bool {
	return f.healthy
}

func TestService_HandleTransfer_MissingFilename(t *testing.T) {
	s := &Service{
		health: &fakeHealthChecker{healthy: true},
	}

	body := bytes.NewReader([]byte{})
	req, err := http.NewRequest("POST", "http://localhost:8080/transfer", body)
	require.NoError(t, err)

	resp := httptest.NewRecorder()
	s.HandleTransfer(resp, req)
	require.Equal(t, http.StatusBadRequest, resp.Code, resp.Body.String())
}

func TestService_HandleTransfer_InvalidFilename(t *testing.T) {
	tests := []struct {
		name     string
		filename string
	}{
		{name: "missing extension", filename: "foo"},
		{name: "invalid extension", filename: "foo.bar"},
		{name: "invalid separators", filename: "foo.bar.wal"},
		{name: "too many separators", filename: "foo_bar_baz_bip.wal"},
		{name: "not enough separators", filename: "foo_bar.wal"},
		{name: "no filename", filename: ""},
		{name: "path traversal", filename: "../../foo_bar_baz.wal"},
		{name: "colon", filename: "DB_Metric:avg_123.wal"},
		{name: "period", filename: "DB.wal_Metricavg_123.wal"},
		{name: "unknown DB", filename: "Database_Metric_123.wal"},
	}

	s := &Service{
		health: &fakeHealthChecker{healthy: true},
		store:  &fakeStore{},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := bytes.NewReader([]byte{})
			req, err := http.NewRequest("POST", "http://localhost:8080/transfer", body)

			q := req.URL.Query()
			q.Add("filename", tt.filename)
			req.URL.RawQuery = q.Encode()
			require.NoError(t, err)

			resp := httptest.NewRecorder()
			s.HandleTransfer(resp, req)
			require.Equal(t, http.StatusBadRequest, resp.Code, resp.Body.String())
		})
	}
}

func TestService_HandleTransfer_Conflict(t *testing.T) {
	s := &Service{
		health: &fakeHealthChecker{healthy: true},
		databases: map[string]struct{}{
			"Database": {},
		},
		store: &fakeStore{
			segements: map[string]struct{}{
				"Database_Metric_123.wal": {},
			},
		},
	}

	body := bytes.NewReader([]byte{})
	req, err := http.NewRequest("POST", "http://localhost:8080/transfer", body)

	q := req.URL.Query()
	q.Add("filename", "Database_Metric_123.wal")
	req.URL.RawQuery = q.Encode()
	require.NoError(t, err)

	resp := httptest.NewRecorder()
	s.HandleTransfer(resp, req)
	require.Equal(t, http.StatusConflict, resp.Code, resp.Body.String())
}

type fakeStore struct {
	segements map[string]struct{}
}

func (f fakeStore) SegmentExists(filename string) bool {
	_, ok := f.segements[filename]
	return ok
}

func (f fakeStore) Open(ctx context.Context) error {
	panic("implement me")
}

func (f fakeStore) Close() error {
	panic("implement me")
}

func (f fakeStore) WriteTimeSeries(ctx context.Context, database string, ts []prompb.TimeSeries) error {
	panic("implement me")
}

func (f fakeStore) WriteOTLPLogs(ctx context.Context, database, table string, logs *otlp.Logs) error {
	panic("implement me")
}

func (f fakeStore) IsActiveSegment(path string) bool {
	panic("implement me")
}

func (f fakeStore) Import(filename string, body io.ReadCloser) (int, error) {
	return 0, fmt.Errorf("Import should not be called")
}
