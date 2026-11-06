package http

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMaybeCloseConnection(t *testing.T) {
	// Reset the counter before each test
	counter.Store(0)

	t.Run("does not close connection before threshold", func(t *testing.T) {
		for range closeEvery - 1 {
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
		counter.Store(closeEvery - 1)
		MaybeCloseConnection(resp, req)

		require.True(t, req.Close)
		require.Equal(t, "close", resp.Header().Get("Connection"))
	})
}
