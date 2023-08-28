package wal_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	flakeutil "github.com/Azure/adx-mon/pkg/flake"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/davidnarayan/go-flake"
	"github.com/stretchr/testify/require"
)

func TestNewSegment(t *testing.T) {
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
}

func TestSegment_CreatedAt(t *testing.T) {
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
}

func TestSegment_Corrupted(t *testing.T) {
	dir := t.TempDir()
	s, err := wal.NewSegment(dir, "Foo")
	require.NoError(t, err)
	require.NoError(t, s.Write(context.Background(), []byte("test")))
	require.NoError(t, s.Close())

	f, err := os.OpenFile(s.Path(), os.O_APPEND|os.O_RDWR, 0600)
	require.NoError(t, err)
	_, err = f.WriteString("a,")
	require.NoError(t, err)

	s, err = wal.Open(s.Path())
	require.NoError(t, err)

	b, err := s.Bytes()
	require.NoError(t, err)
	require.Equal(t, uint8('t'), b[len(b)-1])
}

func TestSegment_Corrupted_BigFile(t *testing.T) {
	dir := t.TempDir()
	s, err := wal.NewSegment(dir, "Foo")
	require.NoError(t, err)
	require.NoError(t, s.Write(context.Background(), []byte("test")))
	require.NoError(t, s.Close())

	f, err := os.OpenFile(s.Path(), os.O_APPEND|os.O_RDWR, 0600)
	require.NoError(t, err)
	str := strings.Repeat("a", 8092)
	_, err = f.WriteString(fmt.Sprintf("%s,", str))
	require.NoError(t, err)

	s, err = wal.Open(s.Path())
	require.NoError(t, err)

	b, err := s.Bytes()
	require.NoError(t, err)
	require.Equal(t, uint8('t'), b[len(b)-1])

	require.NoError(t, s.Write(context.Background(), []byte("test")))
	b, err = s.Bytes()
	require.NoError(t, err)
	require.Equal(t, "test", string(b[len(b)-4:]))
}

func TestSegment_Iterator(t *testing.T) {
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
}

func TestSegment_LargeSegments(t *testing.T) {
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
