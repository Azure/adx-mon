package ingestor

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/wal"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

var transferCodes = []string{"202", "400", "413", "423", "429", "500"}

// transferRequestsCounted returns how many /transfer requests requests_received_total counts for each code.
func transferRequestsCounted() map[string]float64 {
	counts := make(map[string]float64)
	for _, code := range transferCodes {
		m := &dto.Metric{}
		if err := metrics.RequestsReceived.WithLabelValues("/transfer", code).Write(m); err != nil {
			panic(err)
		}
		counts[code] = m.GetCounter().GetValue()
	}
	return counts
}

func countedSince(before map[string]float64) map[string]float64 {
	delta := make(map[string]float64)
	for code, v := range transferRequestsCounted() {
		if d := v - before[code]; d != 0 {
			delta[code] = d
		}
	}
	return delta
}

// transferOutcomeCases are transfers that end in each of the handler's responses.
func transferOutcomeCases() map[string]struct {
	service  *Service
	filename string
	header   http.Header
	status   int
} {
	store := func(err error) *Service {
		return &Service{
			health: &fakeHealthChecker{healthy: true},
			store: &fakeStore{importFn: func(_ string, body io.ReadCloser) (int, error) {
				n, _ := io.Copy(io.Discard, body)
				return int(n), err
			}},
			databases: map[string]struct{}{"testdb": {}},
		}
	}
	const valid = "testdb_Table_2f2c74ffa19e6000.wal"
	return map[string]struct {
		service  *Service
		filename string
		header   http.Header
		status   int
	}{
		"stored":           {store(nil), valid, nil, http.StatusAccepted},
		"dropped prefix":   {func() *Service { s := store(nil); s.dropFilePrefixes = []string{"testdb_Table"}; return s }(), valid, nil, http.StatusAccepted},
		"unhealthy":        {func() *Service { s := store(nil); s.health = &fakeHealthChecker{}; return s }(), valid, nil, http.StatusTooManyRequests},
		"missing filename": {store(nil), "", nil, http.StatusBadRequest},
		"invalid filename": {store(nil), "nodb_Table_2f2c74ffa19e6000.wal", nil, http.StatusBadRequest},
		"invalid gzip":     {store(nil), valid, http.Header{"Content-Encoding": {"gzip"}}, http.StatusBadRequest},
		"too large":        {store(&wal.SegmentTooLargeError{Limit: 10}), valid, nil, http.StatusRequestEntityTooLarge},
		"table overloaded": {store(&wal.TableOverloadedError{Reason: "bytes"}), valid, nil, http.StatusTooManyRequests},
		"disk full":        {store(wal.ErrMaxDiskUsageExceeded), valid, nil, http.StatusTooManyRequests},
		"segment locked":   {store(wal.ErrSegmentLocked), valid, nil, http.StatusLocked},
		"checksum":         {store(errors.New("block checksum verification failed")), valid, nil, http.StatusBadRequest},
		"other error":      {store(errors.New("boom")), valid, nil, http.StatusInternalServerError},
	}
}

// requests_received_total counts every /transfer response by its code.
func TestHandleTransferCountsRequestsByCode(t *testing.T) {
	for name, tc := range transferOutcomeCases() {
		t.Run(name, func(t *testing.T) {
			before := transferRequestsCounted()
			r := httptest.NewRequest("POST", "/transfer?filename="+tc.filename, bytes.NewReader([]byte("data")))
			for k, v := range tc.header {
				r.Header[k] = v
			}
			resp := httptest.NewRecorder()
			tc.service.HandleTransfer(resp, r)
			require.Equal(t, tc.status, resp.Code)

			require.Equal(t, map[string]float64{strconv.Itoa(tc.status): 1}, countedSince(before))
		})
	}
}

func transfersReceived(sender, code string) float64 {
	m := &dto.Metric{}
	if err := metrics.IngestorTransfersReceived.WithLabelValues(sender, code).Write(m); err != nil {
		panic(err)
	}
	return m.GetCounter().GetValue()
}

// transfers_received_total counts every /transfer response by its sender and code.
func TestHandleTransferCountsTransfersBySenderAndCode(t *testing.T) {
	for name, tc := range transferOutcomeCases() {
		for sender, header := range map[string]string{"collector": "", "peer": "true"} {
			t.Run(name+"/"+sender, func(t *testing.T) {
				code := strconv.Itoa(tc.status)
				before := transfersReceived(sender, code)
				r := httptest.NewRequest("POST", "/transfer?filename="+tc.filename, bytes.NewReader([]byte("data")))
				for k, v := range tc.header {
					r.Header[k] = v
				}
				if header != "" {
					r.Header.Set(cluster.PeerTransferHeader, header)
				}
				resp := httptest.NewRecorder()
				tc.service.HandleTransfer(resp, r)
				require.Equal(t, tc.status, resp.Code)
				require.Equal(t, before+1, transfersReceived(sender, code))
			})
		}
	}
}

func TestStatusRecorder(t *testing.T) {
	t.Run("first code wins", func(t *testing.T) {
		resp := httptest.NewRecorder()
		s := &statusRecorder{ResponseWriter: resp, code: http.StatusOK}
		s.WriteHeader(http.StatusLocked)
		s.WriteHeader(http.StatusAccepted)
		require.Equal(t, http.StatusLocked, s.code)
	})
	t.Run("write without a header is 200", func(t *testing.T) {
		resp := httptest.NewRecorder()
		s := &statusRecorder{ResponseWriter: resp, code: http.StatusOK}
		_, err := s.Write([]byte("x"))
		require.NoError(t, err)
		s.WriteHeader(http.StatusTeapot)
		require.Equal(t, http.StatusOK, s.code)
	})
	t.Run("unwrap", func(t *testing.T) {
		resp := httptest.NewRecorder()
		require.Same(t, resp, (&statusRecorder{ResponseWriter: resp}).Unwrap())
	})
}
