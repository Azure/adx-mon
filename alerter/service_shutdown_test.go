package alerter

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCloseCleansUpAfterHTTPShutdownTimeout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	require.NoError(t, ln.Close())
	oldGrace := httpShutdownGrace
	httpShutdownGrace = 20 * time.Millisecond
	defer func() { httpShutdownGrace = oldGrace }()

	l, err := NewService(&AlerterOpts{Dev: true, Port: port, Concurrency: 1, KustoEndpoints: map[string]string{"fake": "http://fake.endpoint"}})
	require.NoError(t, err)
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	l.alertHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
	})
	require.NoError(t, l.Open(context.Background()))
	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", fmt.Sprint(port)), 50*time.Millisecond)
		if err != nil {
			return false
		}
		conn.Close()
		return true
	}, 2*time.Second, time.Millisecond)
	t.Cleanup(func() {
		if l.httpServer != nil {
			_ = l.httpServer.Close()
		}
	})
	url := fmt.Sprintf("http://127.0.0.1:%d/alerts", port)
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
		}
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not start")
	}
	err = l.Close()
	require.Error(t, err)
	require.True(t, errors.Is(err, context.DeadlineExceeded), "shutdown timeout should be returned")
	conn, dialErr := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", fmt.Sprint(port)), 100*time.Millisecond)
	if dialErr == nil {
		conn.Close()
		t.Fatal("server still accepts connections after failed shutdown")
	}
	<-requestDone
}
