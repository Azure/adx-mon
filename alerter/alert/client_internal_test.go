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

func TestNewFanoutClient_IsolatesPerEndpointTransports(t *testing.T) {
	fcDefault, err := NewFanoutClient([]string{"http://host.example:8080/a", "http://host.example:8080/b"})
	require.NoError(t, err)
	require.Len(t, fcDefault.queues, 2)
	require.NotNil(t, fcDefault.queues[0].ownClient)
	require.NotNil(t, fcDefault.queues[1].ownClient)
	require.NotSame(t, fcDefault.queues[0].ownClient, fcDefault.queues[1].ownClient)

	tr0 := fcDefault.queues[0].ownClient.httpClient.Transport.(*http.Transport)
	tr1 := fcDefault.queues[1].ownClient.httpClient.Transport.(*http.Transport)
	require.NotSame(t, tr0, tr1)
	require.Equal(t, 100, tr0.MaxConnsPerHost)
	require.Equal(t, 100, tr1.MaxConnsPerHost)
	require.NoError(t, fcDefault.Close())

	fcHigh, err := NewFanoutClient(
		[]string{"http://host.example:8080/a", "http://host.example:8080/b"},
		WithConcurrency(150),
	)
	require.NoError(t, err)
	trHigh0 := fcHigh.queues[0].ownClient.httpClient.Transport.(*http.Transport)
	trHigh1 := fcHigh.queues[1].ownClient.httpClient.Transport.(*http.Transport)
	require.NotSame(t, trHigh0, trHigh1)
	require.Equal(t, 150, trHigh0.MaxConnsPerHost)
	require.Equal(t, 150, trHigh1.MaxConnsPerHost)
	require.NoError(t, fcHigh.Close())
}
