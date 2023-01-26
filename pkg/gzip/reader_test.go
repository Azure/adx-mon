package gzip

import (
	"bytes"
	"compress/gzip"
	"github.com/stretchr/testify/require"
	"io"
	"os"
	"testing"
)

func TestNewCompressReader(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(dir+"/test.txt", []byte("hello world"), 0644))
	f, err := os.Open(dir + "/test.txt")
	require.NoError(t, err)
	defer f.Close()
	r := NewCompressReader(f)

	fz, err := os.OpenFile(dir+"/test.txt.gz", os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	io.Copy(fz, r)

	f, err = os.Open(dir + "/test.txt.gz")
	require.NoError(t, err)
	defer f.Close()
	gr, err := gzip.NewReader(f)
	require.NoError(t, err)

	b := bytes.NewBuffer(make([]byte, 0, 1024))

	io.Copy(b, gr)

	require.Equal(t, "hello world", b.String())
}
