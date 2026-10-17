package alert

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewClient_UsesConfiguredTransport(t *testing.T) {
	defaults := http.DefaultTransport.(*http.Transport)
	maxIdleConns := defaults.MaxIdleConns
	maxConnsPerHost := defaults.MaxConnsPerHost
	maxIdleConnsPerHost := defaults.MaxIdleConnsPerHost
	responseHeaderTimeout := defaults.ResponseHeaderTimeout
	idleConnTimeout := defaults.IdleConnTimeout

	timeout := 5 * time.Second
	c, err := NewClient(timeout)
	require.NoError(t, err)

	tr, ok := c.httpClient.Transport.(*http.Transport)
	require.True(t, ok, "http client must use a *http.Transport")
	require.NotSame(t, http.DefaultTransport, tr, "transport must be a dedicated clone")

	require.Equal(t, 100, tr.MaxIdleConns)
	require.Equal(t, 100, tr.MaxConnsPerHost)
	require.Equal(t, 100, tr.MaxIdleConnsPerHost)
	require.Equal(t, timeout, tr.ResponseHeaderTimeout)
	require.Equal(t, time.Minute, tr.IdleConnTimeout)
	require.Equal(t, timeout, c.httpClient.Timeout)

	require.Equal(t, maxIdleConns, defaults.MaxIdleConns)
	require.Equal(t, maxConnsPerHost, defaults.MaxConnsPerHost)
	require.Equal(t, maxIdleConnsPerHost, defaults.MaxIdleConnsPerHost)
	require.Equal(t, responseHeaderTimeout, defaults.ResponseHeaderTimeout)
	require.Equal(t, idleConnTimeout, defaults.IdleConnTimeout)
}
