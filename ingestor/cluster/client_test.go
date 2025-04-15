package cluster

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClient_Write_Success(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "POST", r.Method)
		require.Equal(t, "/transfer", r.URL.Path)
		require.Equal(t, "filename=testfile", r.URL.RawQuery)
		require.Equal(t, "text/csv", r.Header.Get("Content-Type"))
		require.Equal(t, "adx-mon", r.Header.Get("User-Agent"))
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client, err := NewClient(ClientOpts{
		Timeout:            time.Second,
		InsecureSkipVerify: true,
		DisableGzip:        true,
	})
	require.NoError(t, err)

	err = client.Write(context.Background(), server.URL, "testfile", strings.NewReader("testdata"))
	require.NoError(t, err)
}

func TestClient_Write_BadRequest(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request error"))
	}))
	defer server.Close()

	client, err := NewClient(ClientOpts{
		Timeout:            time.Second,
		InsecureSkipVerify: true,
		DisableGzip:        true,
	})
	require.NoError(t, err)

	err = client.Write(context.Background(), server.URL, "testfile", strings.NewReader("testdata"))
	require.Error(t, err)
	require.IsType(t, &ErrBadRequest{}, err)
	require.Contains(t, err.Error(), "bad request error")
	require.True(t, errors.Is(err, ErrBadRequest{}))
}

func TestClient_Write_TooManyRequests(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client, err := NewClient(ClientOpts{
		Timeout:            time.Second,
		InsecureSkipVerify: true,
		DisableGzip:        true,
	})
	require.NoError(t, err)

	err = client.Write(context.Background(), server.URL, "testfile", strings.NewReader("testdata"))
	require.Error(t, err)
	require.Equal(t, ErrPeerOverloaded, err)
	require.True(t, errors.Is(err, ErrPeerOverloaded))
}

func TestClient_Write_SegmentExists(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	defer server.Close()

	client, err := NewClient(ClientOpts{
		Timeout:            time.Second,
		InsecureSkipVerify: true,
		DisableGzip:        true,
	})
	require.NoError(t, err)

	err = client.Write(context.Background(), server.URL, "testfile", strings.NewReader("testdata"))
	require.Error(t, err)
	require.Equal(t, ErrSegmentExists, err)
	require.True(t, errors.Is(err, ErrSegmentExists))
}

func TestClient_Write_HTTPError(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	client, err := NewClient(ClientOpts{
		Timeout:            time.Second,
		InsecureSkipVerify: true,
		DisableGzip:        true,
	})
	require.NoError(t, err)

	err = client.Write(context.Background(), server.URL, "testfile", strings.NewReader("testdata"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "internal server error")
}

func TestClient_Write_DisableGzip_False(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "POST", r.Method)
		require.Equal(t, "/transfer", r.URL.Path)
		require.Equal(t, "filename=testfile", r.URL.RawQuery)
		require.Equal(t, "gzip", r.Header.Get("Content-Encoding")) // Ensure gzip encoding is set

		// Decompress gzip data
		gzipReader, err := gzip.NewReader(r.Body)
		require.NoError(t, err)
		defer gzipReader.Close()

		var decompressed bytes.Buffer
		_, err = io.Copy(&decompressed, gzipReader)
		require.NoError(t, err)
		require.Equal(t, "testdata", decompressed.String()) // Ensure data is compressed and matches
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client, err := NewClient(ClientOpts{
		Timeout:     time.Second,
		DisableGzip: false,
	})
	require.NoError(t, err)

	err = client.Write(context.Background(), server.URL, "testfile", strings.NewReader("testdata"))
	require.NoError(t, err)
}
