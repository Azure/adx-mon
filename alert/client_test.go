package alert_test

import (
	"context"
	"encoding/json"
	"github.com/Azure/adx-mon/alert"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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
