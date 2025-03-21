package reader_test

import (
	"io"
	"strings"
	"testing"

	"github.com/Azure/adx-mon/pkg/reader"
	"github.com/stretchr/testify/require"
)

func TestCounterReader_Read(t *testing.T) {
	data := "Hello, World!"
	r := strings.NewReader(data)
	cr := reader.NewCounterReader(io.NopCloser(r))

	buf := make([]byte, 5)
	n, err := cr.Read(buf)
	require.NoError(t, err)
	require.Equal(t, 5, n)
	require.Equal(t, "Hello", string(buf[:n]))
	require.Equal(t, int64(5), cr.Count())

	n, err = cr.Read(buf)
	require.NoError(t, err)
	require.Equal(t, 5, n)
	require.Equal(t, ", Wor", string(buf[:n]))
	require.Equal(t, int64(10), cr.Count())

	n, err = cr.Read(buf)
	require.NoError(t, err)
	require.Equal(t, 3, n)
	require.Equal(t, "ld!", string(buf[:n]))
	require.Equal(t, int64(13), cr.Count())
}

func TestCounterReader_Read_Empty(t *testing.T) {
	data := ""
	r := strings.NewReader(data)
	cr := reader.NewCounterReader(io.NopCloser(r))

	buf := make([]byte, 5)
	n, err := cr.Read(buf)
	require.Error(t, io.EOF, err)
	require.Equal(t, 0, n)
	require.Equal(t, int64(0), cr.Count())
}

func TestCounterReader_Read_Partial(t *testing.T) {
	data := "Hi"
	r := strings.NewReader(data)
	cr := reader.NewCounterReader(io.NopCloser(r))

	buf := make([]byte, 5)
	n, err := cr.Read(buf)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.Equal(t, "Hi", string(buf[:n]))
	require.Equal(t, int64(2), cr.Count())
}
