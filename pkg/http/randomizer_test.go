package http

import (
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMaybeCloseConnection(t *testing.T) {
	// Reset the counter before each test
	atomic.StoreUint64(&counter, 0)

	t.Run("does not close connection before threshold", func(t *testing.T) {
		for i := 0; i < closeEvery-1; i++ {
			req := httptest.NewRequest("GET", "/", nil)
			resp := httptest.NewRecorder()

			MaybeCloseConnection(resp, req)

			require.False(t, req.Close)
			require.NotEqual(t, "close", resp.Header().Get("Connection"))
		}
	})

	t.Run("closes connection at threshold", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		resp := httptest.NewRecorder()

		// Simulate reaching the threshold
		atomic.StoreUint64(&counter, closeEvery-1)
		MaybeCloseConnection(resp, req)

		require.True(t, req.Close)
		require.Equal(t, "close", resp.Header().Get("Connection"))
	})
}
