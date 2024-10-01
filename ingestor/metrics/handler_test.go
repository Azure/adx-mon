package metrics

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/transform"
	"github.com/golang/snappy"
	"github.com/stretchr/testify/require"
)

type fakeRequestWriter struct {
	fn func(ctx context.Context, wr *prompb.WriteRequest) error
}

func (f *fakeRequestWriter) Write(ctx context.Context, wr *prompb.WriteRequest) error {
	return f.fn(ctx, wr)
}

func TestHandler_HandleReceive(t *testing.T) {
	var called bool
	writer := &fakeRequestWriter{
		fn: func(ctx context.Context, wr *prompb.WriteRequest) error {
			require.Equal(t, 1, len(wr.Timeseries))
			called = true
			return nil
		},
	}

	h := NewHandler(HandlerOpts{
		RequestTransformer: &transform.RequestTransformer{},
		RequestWriter:      writer,
		HealthChecker:      &fakeHealthChecker{healthy: true},
		Database:           "adxmetrics",
	})

	wr := &prompb.WriteRequest{
		Timeseries: []*prompb.TimeSeries{
			{
				Labels: []*prompb.Label{
					{
						Name:  []byte("foo"),
						Value: []byte("bar"),
					},
				},
				Samples: []*prompb.Sample{
					{
						Value:     1,
						Timestamp: 1,
					},
				},
			},
		},
	}

	b, err := wr.Marshal()
	require.NoError(t, err)

	encoded := snappy.Encode(nil, b)
	body := bytes.NewReader(encoded)

	req, err := http.NewRequest("POST", "http://localhost:8080/receive", body)
	require.NoError(t, err)

	resp := httptest.NewRecorder()
	h.HandleReceive(resp, req)
	require.Equal(t, http.StatusAccepted, resp.Code, resp.Body.String())
	require.True(t, called)

}

func TestHandler_HandleReceive_Unhealthy(t *testing.T) {
	var called bool
	writer := &fakeRequestWriter{
		fn: func(ctx context.Context, wr *prompb.WriteRequest) error {
			require.Equal(t, 1, len(wr.Timeseries))
			called = true
			return nil
		},
	}

	h := NewHandler(HandlerOpts{
		RequestTransformer: &transform.RequestTransformer{
			AllowedDatabase: map[string]struct{}{"adxmetrics": {}},
		},
		RequestWriter: writer,
		HealthChecker: &fakeHealthChecker{healthy: false},
		Database:      "adxmetrics",
	})

	wr := prompb.WriteRequest{
		Timeseries: []*prompb.TimeSeries{
			{
				Labels: []*prompb.Label{
					{
						Name:  []byte("foo"),
						Value: []byte("bar"),
					},
				},
				Samples: []*prompb.Sample{
					{
						Value:     1,
						Timestamp: 1,
					},
				},
			},
		},
	}

	b, err := wr.Marshal()
	require.NoError(t, err)

	encoded := snappy.Encode(nil, b)
	body := bytes.NewReader(encoded)

	req, err := http.NewRequest("POST", "http://localhost:8080/receive", body)
	require.NoError(t, err)

	resp := httptest.NewRecorder()
	h.HandleReceive(resp, req)
	require.Equal(t, http.StatusTooManyRequests, resp.Code, resp.Body.String())
	require.False(t, called)

}

func TestHandler_HandleReceive_AllowedDBs(t *testing.T) {
	var called bool
	writer := &fakeRequestWriter{
		fn: func(ctx context.Context, wr *prompb.WriteRequest) error {
			require.Equal(t, 0, len(wr.Timeseries))
			called = true
			return nil
		},
	}

	h := NewHandler(HandlerOpts{
		RequestTransformer: &transform.RequestTransformer{
			AllowedDatabase: map[string]struct{}{"adxmetrics": {}},
		},
		RequestWriter: writer,
		HealthChecker: &fakeHealthChecker{healthy: true},
		Database:      "adxmetrics",
	})

	wr := prompb.WriteRequest{
		Timeseries: []*prompb.TimeSeries{
			{
				Labels: []*prompb.Label{
					{
						Name:  []byte("foo"),
						Value: []byte("bar"),
					},
				},
				Samples: []*prompb.Sample{
					{
						Value:     1,
						Timestamp: 1,
					},
				},
			},
		},
	}

	b, err := wr.Marshal()
	require.NoError(t, err)

	encoded := snappy.Encode(nil, b)
	body := bytes.NewReader(encoded)

	req, err := http.NewRequest("POST", "http://localhost:8080/receive", body)
	require.NoError(t, err)

	resp := httptest.NewRecorder()
	h.HandleReceive(resp, req)
	require.Equal(t, http.StatusAccepted, resp.Code, resp.Body.String())
	require.False(t, called)
}

type fakeHealthChecker struct {
	healthy bool
}

func (f *fakeHealthChecker) IsHealthy() bool { return f.healthy }
