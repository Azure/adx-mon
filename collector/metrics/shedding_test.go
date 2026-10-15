package metrics

import (
	"bytes"
	"context"
	"errors"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/remote"
	"github.com/Azure/adx-mon/storage"
	"github.com/Azure/adx-mon/transform"
	"github.com/golang/snappy"
	"github.com/stretchr/testify/require"
	"go.uber.org/multierr"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRemoteWriteMixedSheddingResponse(t *testing.T) {
	for _, failure := range []bool{false, true} {
		err := error(&storage.WriteOutcome{Accepted: 1, Rejected: 1})
		code := http.StatusAccepted
		if failure {
			err = multierr.Append(err, errors.New("disk failure"))
			code = http.StatusInternalServerError
		}
		h := NewHandler(HandlerOpts{RequestTransformer: &transform.RequestTransformer{}, HealthChecker: &fakeHealthChecker{healthy: true}, RequestWriters: []remote.RemoteWriteClient{&fakeRequestWriter{fn: func(context.Context, *prompb.WriteRequest) error { return err }}}})
		r := &prompb.WriteRequest{Timeseries: []*prompb.TimeSeries{{Samples: []*prompb.Sample{{Value: 1}}}}}
		b, e := r.Marshal()
		require.NoError(t, e)
		resp := httptest.NewRecorder()
		h.HandleReceive(resp, httptest.NewRequest("POST", "/receive", bytes.NewReader(snappy.Encode(nil, b))))
		require.Equal(t, code, resp.Code)
	}
}
