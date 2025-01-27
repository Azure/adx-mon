package remote

import (
	"context"
	"sync"

	"github.com/Azure/adx-mon/pkg/prompb"
	"go.uber.org/multierr"
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

func WriteRequest(ctx context.Context, clients []RemoteWriteClient, wr *prompb.WriteRequest) error {
	var wg sync.WaitGroup
	wg.Add(len(clients))
	errs := make(chan error, len(clients))
	for _, client := range clients {
		go func(client RemoteWriteClient) {
			defer wg.Done()
			errs <- client.Write(ctx, wr)
		}(client)
	}

	wg.Wait()
	close(errs)

	var err error
	for e := range errs {
		if e != nil {
			err = multierr.Append(err, e)
		}
	}
	return err
}
