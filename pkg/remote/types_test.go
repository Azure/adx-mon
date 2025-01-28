package remote

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/stretchr/testify/require"
)

type mockRemoteWriteClient struct {
	err error

	callCount int
	mut       sync.Mutex
}

func (m *mockRemoteWriteClient) Write(ctx context.Context, wr *prompb.WriteRequest) error {
	m.mut.Lock()
	m.callCount++
	m.mut.Unlock()
	return m.err
}

func (m *mockRemoteWriteClient) CloseIdleConnections() {}

func (m *mockRemoteWriteClient) CallCount() int {
	m.mut.Lock()
	defer m.mut.Unlock()
	return m.callCount
}

func TestWriteRequest(t *testing.T) {
	ctx := context.Background()
	wr := &prompb.WriteRequest{}

	t.Run("no errors", func(t *testing.T) {
		clientOne := &mockRemoteWriteClient{err: nil}
		clientTwo := &mockRemoteWriteClient{err: nil}
		err := WriteRequest(ctx, []RemoteWriteClient{clientOne, clientTwo}, wr)
		require.NoError(t, err)
		require.Equal(t, 1, clientOne.CallCount())
		require.Equal(t, 1, clientTwo.CallCount())
	})

	t.Run("one error", func(t *testing.T) {
		clientOne := &mockRemoteWriteClient{err: nil}
		clientTwo := &mockRemoteWriteClient{err: io.EOF}
		err := WriteRequest(ctx, []RemoteWriteClient{clientOne, clientTwo}, wr)
		require.Error(t, err)
		require.True(t, errors.Is(err, io.EOF))
		require.Equal(t, 1, clientOne.CallCount())
		require.Equal(t, 1, clientTwo.CallCount())
	})

	t.Run("multiple errors", func(t *testing.T) {
		clientOne := &mockRemoteWriteClient{err: io.ErrUnexpectedEOF}
		clientTwo := &mockRemoteWriteClient{err: io.EOF}
		err := WriteRequest(ctx, []RemoteWriteClient{clientOne, clientTwo}, wr)
		require.Error(t, err)
		require.True(t, errors.Is(err, io.EOF))
		require.True(t, errors.Is(err, io.ErrUnexpectedEOF))
		require.Equal(t, 1, clientOne.CallCount())
		require.Equal(t, 1, clientTwo.CallCount())
	})
}
