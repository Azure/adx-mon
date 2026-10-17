package alert_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/adx-mon/alerter/alert"
	"github.com/stretchr/testify/require"
)

func TestClient_Create(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		// Test request parameters
		require.Equal(t, req.URL.String(), "/alerts")
		require.Equal(t, req.Method, "POST")
		require.Equal(t, req.Header.Get("Content-Type"), "application/json")
		require.Equal(t, req.Header.Get("User-Agent"), "adx-mon-alerter")

		b, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		defer req.Body.Close()

		var a alert.Alert
		require.NoError(t, json.Unmarshal(b, &a))
		require.Equal(t, "summary", a.Summary)
		require.Equal(t, "title", a.Title)
		require.Equal(t, 1, a.Severity)
		require.Equal(t, "MDM://Platform", a.Destination)
		require.Equal(t, "description", a.Description)

		rw.WriteHeader(http.StatusCreated)

	}))
	// Close the server when test finishes
	defer server.Close()

	c, err := alert.NewClient(time.Second)
	require.NoError(t, err)

	err = c.Create(context.Background(), server.URL+"/alerts", alert.Alert{
		Summary:     "summary",
		Title:       "title",
		Severity:    1,
		Destination: "MDM://Platform",
		Description: "description",
	})
	require.NoError(t, err)
}

func TestClient_Create_BadRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusBadRequest)

	}))
	// Close the server when test finishes
	defer server.Close()

	c, err := alert.NewClient(time.Second)
	require.NoError(t, err)

	err = c.Create(context.Background(), server.URL+"/alerts", alert.Alert{
		Summary:     "summary",
		Title:       "title",
		Severity:    1,
		Destination: "MDM://Platform",
		Description: "description",
	})
	require.Errorf(t, err, "write failed: 400 Bad Request")
}

// BenchmarkClient_Create_Concurrent sends alerts from parallel goroutines through one
// client against a local server that answers 201. It reports the number of new TCP
// connections opened per operation so pooling changes are visible alongside allocations.
func BenchmarkClient_Create_Concurrent(b *testing.B) {
	var newConns atomic.Int64
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		io.Copy(io.Discard, req.Body)
		rw.WriteHeader(http.StatusCreated)
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			newConns.Add(1)
		}
	}
	server.Start()
	defer server.Close()

	c, err := alert.NewClient(time.Second)
	require.NoError(b, err)

	a := alert.Alert{
		Summary:     "summary",
		Title:       "title",
		Severity:    1,
		Destination: "MDM://Platform",
		Description: "description",
	}

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := c.Create(context.Background(), server.URL+"/alerts", a); err != nil {
				b.Error(err)
				return
			}
		}
	})
	b.StopTimer()

	b.ReportMetric(float64(newConns.Load())/float64(b.N), "conns/op")
}

func TestClient_Create_TooManyRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusTooManyRequests)
	}))
	// Close the server when test finishes
	defer server.Close()

	c, err := alert.NewClient(time.Second)
	require.NoError(t, err)

	err = c.Create(context.Background(), server.URL+"/alerts", alert.Alert{
		Summary:     "summary",
		Title:       "title",
		Severity:    1,
		Destination: "MDM://Platform",
		Description: "description",
	})
	require.ErrorIs(t, err, alert.ErrTooManyRequests)
}
