package collector

import (
	"context"
	"fmt"
	"testing"

	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/stretchr/testify/require"
)

func TestScraperOpts_RequestTransformer(t *testing.T) {
	opts := &ScraperOpts{
		DefaultDropMetrics: true,
	}

	tr := opts.RequestTransformer()
	require.True(t, tr.DefaultDropMetrics)
}

func TestScraper_sendBatch(t *testing.T) {
	tests := []struct {
		name         string
		writeRequest *prompb.WriteRequest
		opts         *ScraperOpts
	}{
		{
			name:         "TestEmptyWriteRequest",
			writeRequest: &prompb.WriteRequest{},
			opts: &ScraperOpts{
				Endpoints:    []string{"http://fake:1234"},
				RemoteClient: &fakeClient{expectedSamples: 0},
			},
		},
		{
			name: "TestValidWriteRequest",
			opts: &ScraperOpts{
				Endpoints:    []string{"http://fake:1234"},
				RemoteClient: &fakeClient{expectedSamples: 1},
			},
			writeRequest: &prompb.WriteRequest{
				Timeseries: []prompb.TimeSeries{
					{
						Labels: []prompb.Label{
							{Name: []byte("testLabel"), Value: []byte("testValue")},
						},
						Samples: []prompb.Sample{
							{Value: 1, Timestamp: 123456789},
						},
					},
				},
			},
		},
		{
			name: "TestDefaultDropMetrics",
			opts: &ScraperOpts{
				DefaultDropMetrics: true,
				Endpoints:          []string{"http://fake:1234"},
				RemoteClient:       &fakeClient{expectedSamples: 0},
			},
			writeRequest: &prompb.WriteRequest{
				Timeseries: []prompb.TimeSeries{
					{
						Labels: []prompb.Label{
							{Name: []byte("testLabel"), Value: []byte("testValue")},
						},
						Samples: []prompb.Sample{
							{Value: 1, Timestamp: 123456789},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewScraper(tt.opts)
			err := s.sendBatch(context.Background(), tt.writeRequest)
			require.NoError(t, err)
			if tt.opts.RemoteClient.(*fakeClient).expectedSamples > 0 {
				require.True(t, tt.opts.RemoteClient.(*fakeClient).called)
			}
		})
	}
}

type fakeClient struct {
	expectedSamples int
	called          bool
}

func (f *fakeClient) Write(ctx context.Context, endpoint string, wr *prompb.WriteRequest) error {
	f.called = true
	if len(wr.Timeseries) != f.expectedSamples {
		return fmt.Errorf("expected %d samples, got %d", f.expectedSamples, len(wr.Timeseries))
	}
	return nil
}

func (f *fakeClient) CloseIdleConnections() {}
