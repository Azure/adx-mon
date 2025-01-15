package remote

import (
	"context"

	"github.com/Azure/adx-mon/pkg/prompb"
)

type RemoteWriteClient interface {
	Write(ctx context.Context, wr *prompb.WriteRequest) error
	CloseIdleConnections()
}

type RequestWriter interface {
	// Write writes the time series to the correct peer.
	Write(ctx context.Context, wr *prompb.WriteRequest) error
}

func NopCloser(r RequestWriter) RemoteWriteClient {
	return &nopCloser{r}
}

type nopCloser struct {
	r RequestWriter
}

func (n *nopCloser) Write(ctx context.Context, wr *prompb.WriteRequest) error {
	return n.r.Write(ctx, wr)
}

func (n *nopCloser) CloseIdleConnections() {}
