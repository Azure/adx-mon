package wal_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	flakeutil "github.com/Azure/adx-mon/pkg/flake"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/davecgh/go-spew/spew"
	"github.com/davidnarayan/go-flake"
	"github.com/stretchr/testify/require"
)

func TestNewSegment(t *testing.T) {
	tests := []struct {
		Name string
	}{
		{Name: "Disk"},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			dir := t.TempDir()
			s, err := wal.NewSegment(dir, "Foo")
			require.NoError(t, err)
			require.NoError(t, s.Write(context.Background(), []byte("test")))

			require.NotEmpty(t, s.Path())
			epoch := s.ID()

			s, err = wal.Open(s.Path())
			require.NoError(t, err)
			require.NoError(t, s.Write(context.Background(), []byte("test2")))

			require.NotEmpty(t, s.Path())
			require.Equal(t, epoch, s.ID())
		})
	}
}

func TestSegment_CreatedAt(t *testing.T) {
	tests := []struct {
		Name string
	}{
		{Name: "Disk"},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {

			dir := t.TempDir()

			idgen, err := flake.New()
			require.NoError(t, err)

			id := idgen.NextId()
			createdAt, err := flakeutil.ParseFlakeID(id.String())
			require.NoError(t, err)

			path := filepath.Join(dir, fmt.Sprintf("Foo_%s.wal", id.String()))
			f, err := os.Create(path)
			require.NoError(t, err)
			require.NoError(t, f.Close())

			seg, err := wal.Open(path)
			require.NoError(t, err)

			require.Equal(t, createdAt, seg.CreatedAt())
		})
	}
}

func TestSegment_Corrupted(t *testing.T) {
	tests := []struct {
		Name string
	}{
		{Name: "Disk"},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {

			dir := t.TempDir()
			s, err := wal.NewSegment(dir, "Foo")
			require.NoError(t, err)
			require.NoError(t, s.Write(context.Background(), []byte("test")))
			require.NoError(t, s.Close())

			f, err := os.OpenFile(s.Path(), os.O_APPEND|os.O_RDWR, 0600)
			require.NoError(t, err)
			_, err = f.Write([]byte("a,"))
			require.NoError(t, err)

			s, err = wal.Open(s.Path())

			b, err := s.Bytes()
			require.NoError(t, err)
			require.Equal(t, uint8('t'), b[len(b)-1])
		})
	}
}

func TestSegment_Corrupted_BigFile(t *testing.T) {
	tests := []struct {
		Name string
	}{
		{Name: "Disk"},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {

			dir := t.TempDir()
			s, err := wal.NewSegment(dir, "Foo")
			require.NoError(t, err)
			require.NoError(t, s.Write(context.Background(), []byte("test")))
			require.NoError(t, s.Close())

			f, err := os.OpenFile(s.Path(), os.O_APPEND|os.O_RDWR, 0600)
			require.NoError(t, err)
			defer f.Close()

			str := strings.Repeat("a", 8092)
			_, err = f.Write([]byte(fmt.Sprintf("%s,", str)))
			require.NoError(t, err)

			s, err = wal.Open(s.Path())
			defer s.Close()

			b, err := s.Bytes()
			require.NoError(t, err)
			require.Equal(t, uint8('t'), b[len(b)-1])

			require.NoError(t, s.Write(context.Background(), []byte("test")))
			b, err = s.Bytes()
			require.NoError(t, err)
			require.Equal(t, "test", string(b[len(b)-4:]))
		})
	}
}

func TestSegment_Iterator(t *testing.T) {
	tests := []struct {
		Name string
	}{
		{Name: "Disk"},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			dir := t.TempDir()
			s, err := wal.NewSegment(dir, "Foo")
			require.NoError(t, err)
			require.NoError(t, s.Write(context.Background(), []byte("test")))
			require.NoError(t, s.Write(context.Background(), []byte("test1")))
			require.NoError(t, s.Write(context.Background(), []byte("test2")))
			require.NoError(t, s.Close())

			s, err = wal.Open(s.Path())
			require.NoError(t, err)
			iter, err := s.Iterator()
			require.NoError(t, err)

			next, err := iter.Next()
			require.NoError(t, err)
			require.True(t, next)
			require.Equal(t, []byte("test"), iter.Value())

			next, err = iter.Next()
			require.NoError(t, err)
			require.True(t, next)
			require.Equal(t, []byte("test1"), iter.Value())

			next, err = iter.Next()
			require.NoError(t, err)
			require.True(t, next)
			require.Equal(t, []byte("test2"), iter.Value())

			next, err = iter.Next()
			require.ErrorIs(t, err, io.EOF)
			require.False(t, next)
		})
	}
}

func TestSegment_LargeSegments(t *testing.T) {
	tests := []struct {
		Name string
	}{
		{Name: "Disk"},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			dir := t.TempDir()
			s, err := wal.NewSegment(dir, "Foo")
			require.NoError(t, err)
			for i := 0; i < 100000; i++ {
				require.NoError(t, s.Write(context.Background(), []byte(fmt.Sprintf("test%d %s\n", i, strings.Repeat("a", 1024)))))
			}
			require.NoError(t, s.Close())

			f, err := os.Open(s.Path())
			require.NoError(t, err)
			defer f.Close()

			iter, err := wal.NewSegmentIterator(f)
			require.NoError(t, err)
			for {
				next, err := iter.Next()
				if err == io.EOF {
					break
				}
				require.NoError(t, err)
				if !next {
					break
				}
			}

			s, err = wal.Open(s.Path())
			require.NoError(t, err)

			b, err := s.Bytes()
			require.NoError(t, err)
			require.Equal(t, 100000, bytes.Count(b, []byte("\n")))
		})
	}
}

func TestSegment_Closed(t *testing.T) {
	tests := []struct {
		Name string
	}{
		{Name: "Disk"},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			dir := t.TempDir()
			s, err := wal.NewSegment(dir, "Foo")
			require.NoError(t, err)
			require.NoError(t, s.Close())
			require.Equal(t, s.Write(context.Background(), []byte("test")), wal.ErrSegmentClosed)
			require.Equal(t, s.Append(context.Background(), []byte("test")), wal.ErrSegmentClosed)

			_, err = s.Iterator()
			require.Equal(t, err, wal.ErrSegmentClosed)

			_, err = s.Reader()
			require.Equal(t, err, wal.ErrSegmentClosed)

			_, err = s.Info()
			require.Equal(t, err, wal.ErrSegmentClosed)
		})
	}
}

func TestSegment_Size(t *testing.T) {
	tests := []struct {
		Name string
	}{
		{Name: "Disk"},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			dir := t.TempDir()
			s, err := wal.NewSegment(dir, "Foo")
			require.NoError(t, err)

			for i := 0; i < 100; i++ {
				require.NoError(t, s.Write(context.Background(), []byte(strings.Repeat("a", rand.Intn(8*1024)))))
			}

			// Force all buffered writes to disk.
			require.NoError(t, s.Flush())

			sz, err := s.Size()
			require.NoError(t, err)
			info, err := s.Info()
			require.NoError(t, err)
			require.NoError(t, s.Close())

			f, err := os.Open(s.Path())
			require.NoError(t, err)

			stat, err := f.Stat()
			require.NoError(t, err)

			fsSz := stat.Size()

			require.Equal(t, fsSz, sz)
			require.Equal(t, fsSz, info.Size)
		})
	}
}

func TestSegment_Append(t *testing.T) {
	tests := []struct {
		Name string
	}{
		{Name: "Disk"},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			dir := t.TempDir()
			s, err := wal.NewSegment(dir, "Foo")
			require.NoError(t, err)
			require.NoError(t, s.Write(context.Background(), []byte("test")))
			require.NoError(t, s.Write(context.Background(), []byte("test1")))
			require.NoError(t, s.Flush())
			require.NoError(t, s.Close())
			f, err := os.Open(s.Path())
			require.NoError(t, err)
			b, err := io.ReadAll(f)
			require.NoError(t, err)

			s, err = wal.NewSegment(dir, "Foo")
			require.NoError(t, err)
			require.NoError(t, s.Append(context.Background(), b))
			require.NoError(t, s.Append(context.Background(), b))
			require.NoError(t, s.Flush())

			f, err = os.Open(s.Path())
			require.NoError(t, err)
			iter, err := wal.NewSegmentIterator(f)
			require.NoError(t, err)

			next, err := iter.Next()
			require.NoError(t, err)
			require.True(t, next)
			require.Equal(t, []byte("test"), iter.Value())

			next, err = iter.Next()
			require.NoError(t, err)
			require.True(t, next)
			require.Equal(t, []byte("test1"), iter.Value())

			next, err = iter.Next()
			require.NoError(t, err)
			require.True(t, next)
			require.Equal(t, []byte("test"), iter.Value())

			next, err = iter.Next()
			require.NoError(t, err)
			require.True(t, next)
			require.Equal(t, []byte("test1"), iter.Value())

			next, err = iter.Next()
			require.ErrorIs(t, err, io.EOF)
			require.False(t, next)
		})
	}
}

func TestSegment_Append_Corrupted(t *testing.T) {
	tests := []struct {
		Name string
	}{
		{Name: "Disk"},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			dir := t.TempDir()
			s, err := wal.NewSegment(dir, "Foo")
			require.NoError(t, err)
			require.Error(t, s.Append(context.Background(), []byte("test")))
			require.NoError(t, s.Close())
		})
	}
}

func TestSegment_Write(t *testing.T) {
	tests := []struct {
		Name string
	}{
		{Name: "Disk"},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			dir := t.TempDir()

			s, err := wal.NewSegment(dir, "Foo")
			require.NoError(t, err)
			require.NoError(t, s.Write(context.Background(), []byte("test")))
			require.NoError(t, s.Write(context.Background(), []byte("test1")))
			require.NoError(t, s.Flush())

			b, err := s.Bytes()
			require.NoError(t, err)
			spew.Dump(b)
			println(s.Path())

			f, err := os.Open(s.Path())
			require.NoError(t, err)
			iter, err := wal.NewSegmentIterator(f)
			require.NoError(t, err)
			defer f.Close()

			next, err := iter.Next()
			require.NoError(t, err)
			require.True(t, next)
			require.Equal(t, []byte("test"), iter.Value())

			next, err = iter.Next()
			require.NoError(t, err)
			require.True(t, next)
			require.Equal(t, []byte("test1"), iter.Value())

			next, err = iter.Next()
			require.ErrorIs(t, err, io.EOF)
			require.False(t, next)
		})
	}
}

func BenchmarkSegment_Write(b *testing.B) {
	dir := b.TempDir()
	s, err := wal.NewSegment(dir, "Foo")
	require.NoError(b, err)
	defer s.Close()
	buf := []byte("test")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		require.NoError(b, s.Write(context.Background(), buf))
	}
}
