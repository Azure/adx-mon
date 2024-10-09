package promremote

import (
	"context"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/stretchr/testify/require"
)

func TestSendBatchWithValidData(t *testing.T) {
	client := &MockClient{}
	proxy := NewRemoteWriteProxy(client, []string{"http://example.com"}, 10, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := proxy.Open(ctx)
	require.NoError(t, err)
	defer proxy.Close()

	wr := &prompb.WriteRequest{
		Timeseries: []*prompb.TimeSeries{
			{
				Labels: []*prompb.Label{
					{
						Name: []byte("test"), Value: []byte("value"),
					},
				},
				Samples: []*prompb.Sample{
					{
						Timestamp: time.Now().Unix(), Value: 1.0},
				},
			},
		},
	}

	err = proxy.Write(ctx, wr)
	require.NoError(t, err)
}

func TestSendBatchWithEmptyBatch(t *testing.T) {
	client := &MockClient{}
	proxy := NewRemoteWriteProxy(client, []string{"http://example.com"}, 1, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := proxy.Open(ctx)
	require.NoError(t, err)
	defer proxy.Close()

	wr := &prompb.WriteRequest{
		Timeseries: []*prompb.TimeSeries{
			{
				Labels: []*prompb.Label{
					{Name: []byte("test"), Value: []byte("value")},
				},
				Samples: []*prompb.Sample{
					{Timestamp: time.Now().Unix(), Value: 1.0},
				},
			},
			{
				Samples: []*prompb.Sample{
					{Timestamp: time.Now().Unix(), Value: 1.0},
				},
			},
		},
	}

	err = proxy.Write(ctx, wr)
	require.NoError(t, err)

}

type MockClient struct{}

func (m *MockClient) CloseIdleConnections() {}

func (m *MockClient) Write(ctx context.Context, endpoint string, wr *prompb.WriteRequest) error {
	return nil
}
