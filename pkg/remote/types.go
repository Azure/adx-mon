package remote

import (
	"context"

	"github.com/Azure/adx-mon/pkg/prompb"
)

type RemoteWriteClient interface {
	Write(ctx context.Context, endpoint string, wr *prompb.WriteRequest) error
	CloseIdleConnections()
}
