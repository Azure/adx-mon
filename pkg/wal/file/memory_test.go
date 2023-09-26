package file_test

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/Azure/adx-mon/pkg/wal/file"
	"github.com/stretchr/testify/require"
)

func TestMemory(t *testing.T) {
	tests := []struct {
		Name            string
		StorageProvider file.File
	}{
		{
			Name:            "Disk",
			StorageProvider: &file.Disk{},
		},
		{
			Name:            "Memory",
			StorageProvider: &file.Memory{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			fp := filepath.Join(t.TempDir(), "foo")
			f, err := tt.StorageProvider.Create(fp)
			require.NoError(t, err)

			n, err := f.Write([]byte("test"))
			require.NoError(t, err)
			require.Equal(t, 4, n)

			fi, err := f.Stat()
			require.NoError(t, err)
			require.Equal(t, int64(4), fi.Size())
			require.Equal(t, filepath.Base(fp), fi.Name())
			require.Equal(t, false, fi.IsDir())

			b := make([]byte, 10)
			_, err = f.Read(b)
			require.EqualError(t, err, "EOF")

			offset, err := f.Seek(0, io.SeekStart)
			require.NoError(t, err)
			require.Equal(t, int64(0), offset)

			n, err = f.Read(b)
			require.NoError(t, err)
			require.Equal(t, 4, n)
			require.Equal(t, "test", string(b[:n]))

			_, err = f.Read(b)
			require.EqualError(t, err, "EOF")

			n, err = f.Write([]byte("test"))
			require.NoError(t, err)
			require.Equal(t, 4, n)

			offset, err = f.Seek(0, io.SeekStart)
			require.NoError(t, err)
			require.Equal(t, int64(0), offset)

			n, err = f.Read(b)
			require.NoError(t, err)
			require.Equal(t, 8, n)
			require.Equal(t, "testtest", string(b[:n]))

			fi, err = f.Stat()
			require.NoError(t, err)
			require.Equal(t, int64(8), fi.Size())

			require.NoError(t, f.Truncate(0))
			fi, err = f.Stat()
			require.NoError(t, err)
			require.Equal(t, int64(0), fi.Size())

			require.NoError(t, f.Close())
		})
	}
}

func BenchmarkMemoryWrite(b *testing.B) {
	b.ReportAllocs()
	var (
		m   *file.Memory
		buf = []byte("test")
	)
	f, _ := m.Create("foo")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.Write(buf)
	}
}

func BenchmarkMemoryRead(b *testing.B) {
	b.ReportAllocs()
	var (
		m   *file.Memory
		buf = []byte("test")
	)
	f, _ := m.Create("foo")
	f.Write(buf)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.Read(buf)
		f.Seek(0, io.SeekStart)
	}
}
