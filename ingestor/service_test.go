package ingestor

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

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
		{
			name:     "missing extension",
			filename: "foo",
		},
		{
			name:     "invalid extension",
			filename: "foo.bar",
		},
		{
			name:     "invalid separators",
			filename: "foo.bar.wal",
		},
		{
			name:     "too many separators",
			filename: "foo_bar_baz_bip.wal",
		},
		{
			name:     "not enough separators",
			filename: "foo_bar.wal",
		},
		{
			name:     "no filename",
			filename: "",
		},
		{
			name:     "path traversal",
			filename: "../../foo_bar_baz.wal",
		},
	}

	s := &Service{
		health: &fakeHealthChecker{healthy: true},
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
