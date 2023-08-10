package metrics

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/samples"
	"github.com/golang/snappy"
	"github.com/stretchr/testify/require"
)

type fakeRequestWriter struct {
	samples.Writer
	t      *testing.T
	called bool
}

func (f *fakeRequestWriter) Write(ctx context.Context, s interface{}) error {
	f.t.Helper()
	wr, ok := s.(prompb.WriteRequest)
	if !ok {
		f.t.Fatalf("unexpected type %T", s)
	}
	require.Equal(f.t, 1, len(wr.Timeseries))
	f.called = true
	return nil
}

func TestHandler_HandleReceive(t *testing.T) {
	writer := &fakeRequestWriter{t: t}

	h := NewHandler(HandlerOpts{
		DropLabels:    nil,
		DropMetrics:   nil,
		SeriesCounter: nil,
		RequestWriter: writer,
	})

	wr := prompb.WriteRequest{
		Timeseries: []prompb.TimeSeries{
			{
				Labels: []prompb.Label{
					{
						Name:  []byte("foo"),
						Value: []byte("bar"),
					},
				},
				Samples: []prompb.Sample{
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
	require.True(t, writer.called)

}
