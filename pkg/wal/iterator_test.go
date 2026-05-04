package wal_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"io"
	"os"
	"testing"

	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/klauspost/compress/s2"
	"github.com/stretchr/testify/require"
)

type errReader struct {
	err error
}

func (e *errReader) Read(_ []byte) (int, error) {
	return 0, e.err
}

func (e *errReader) Close() error {
	return nil
}

func TestSegmentIterator_Verify(t *testing.T) {

	tests := []struct {
		Name string
	}{
		{Name: "Disk"},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			dir := t.TempDir()

			iter, err := wal.NewSegmentIterator(io.NopCloser(bytes.NewBuffer([]byte("ADXWAL  testtest1"))))
			require.NoError(t, err)
			defer iter.Close()

			n, err := iter.Verify()
			require.Error(t, err)
			require.Equal(t, 0, n)

			s, err := wal.NewSegment(dir, "Foo")
			require.NoError(t, err)
			n, err = s.Write(context.Background(), []byte("test"))
			require.NoError(t, err)
			require.True(t, n > 0)
			n, err = s.Write(context.Background(), []byte("test1"))
			require.NoError(t, err)
			require.True(t, n > 0)
			require.NoError(t, s.Flush())

			f, err := os.Open(s.Path())
			require.NoError(t, err)
			iter, err = wal.NewSegmentIterator(f)
			require.NoError(t, err)
			defer iter.Close()
			defer f.Close()

			n, err = iter.Verify()
			require.NoError(t, err)
			require.Equal(t, 2, n)

		})
	}
}

func TestSegmentIterator_Verify_TrailingBytes(t *testing.T) {

	tests := []struct {
		Name string
	}{
		{Name: "Disk"},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			dir := t.TempDir()

			iter, err := wal.NewSegmentIterator(io.NopCloser(bytes.NewBuffer([]byte("ADXWAL  testtest1"))))
			require.NoError(t, err)
			defer iter.Close()

			n, err := iter.Verify()
			require.Error(t, err)
			require.Equal(t, 0, n)

			s, err := wal.NewSegment(dir, "Foo")
			require.NoError(t, err)
			n, err = s.Write(context.Background(), []byte("test"))
			require.NoError(t, err)
			require.True(t, n > 0)
			n, err = s.Write(context.Background(), []byte("test1"))
			require.NoError(t, err)
			require.True(t, n > 0)
			require.NoError(t, s.Flush())

			f, err := os.OpenFile(s.Path(), os.O_RDWR, 0644)
			require.NoError(t, err)

			// Seek to the end of the file
			_, err = f.Seek(0, io.SeekEnd)
			require.NoError(t, err)

			// Append some trailing 0 bytes
			_, err = f.Write(make([]byte, 1))
			require.NoError(t, err)
			_, err = f.Seek(0, io.SeekStart)

			// We should be able to iterate over the two good blocks and stop when we hit
			// the trailing zeros.
			iter, err = wal.NewSegmentIterator(f)
			require.NoError(t, err)
			defer iter.Close()
			defer f.Close()

			next, err := iter.Next()
			require.NoError(t, err)
			require.True(t, next)

			next, err = iter.Next()
			require.NoError(t, err)
			require.True(t, next)

			next, err = iter.Next()
			require.NoError(t, err)
			require.False(t, next)

			_, err = f.Seek(0, io.SeekStart)
			require.NoError(t, err)

			// Create a new iterator and check that Verify returns succeeds as it does
			// have a bad block at the end that could be repaired, but is ignored.
			iter, err = wal.NewSegmentIterator(f)
			require.NoError(t, err)
			defer iter.Close()
			defer f.Close()
			n, err = iter.Verify()
			require.NoError(t, err)
			require.Equal(t, 2, n)

		})
	}
}

func TestSegmentIterator_Next_ShortReadCases(t *testing.T) {
	tests := []struct {
		name    string
		reader  io.ReadCloser
		payload []byte
		err     string
	}{
		{
			name:    "empty header at tail returns false nil",
			payload: []byte("ADXWAL  "),
		},
		{
			name:    "short header at tail returns false nil",
			payload: append([]byte("ADXWAL  "), 1, 2, 3),
		},
		{
			name:    "almost complete header at tail returns false nil",
			payload: append([]byte("ADXWAL  "), 1, 2, 3, 4, 5, 6, 7),
		},
		{
			name:   "header read returns other error",
			reader: io.NopCloser(io.MultiReader(bytes.NewReader([]byte("ADXWAL  ")), &errReader{err: io.ErrClosedPipe})),
			err:    io.ErrClosedPipe.Error(),
		},
		{
			name: "missing block payload returns false nil",
			payload: appendSegmentHeader(
				[]byte("ADXWAL  "),
				5,
				crc32.ChecksumIEEE([]byte("abcde")),
			),
		},
		{
			name: "partial block payload returns false nil",
			payload: append(
				appendSegmentHeader(
					[]byte("ADXWAL  "),
					5,
					crc32.ChecksumIEEE([]byte("abcde")),
				),
				'a', 'b',
			),
		},
		{
			name: "checksum mismatch returns false nil",
			payload: append(
				appendSegmentHeader(
					[]byte("ADXWAL  "),
					5,
					crc32.ChecksumIEEE([]byte("abcde"))+1,
				),
				[]byte("abcde")...,
			),
		},
		{
			name:   "payload read returns other error",
			reader: io.NopCloser(io.MultiReader(bytes.NewReader(appendSegmentHeader([]byte("ADXWAL  "), 5, crc32.ChecksumIEEE([]byte("abcde")))), &errReader{err: io.ErrClosedPipe})),
			err:    io.ErrClosedPipe.Error(),
		},
		{
			name:    "s2 decode error returns false nil",
			payload: append(appendSegmentHeader([]byte("ADXWAL  "), 3, crc32.ChecksumIEEE([]byte("bad"))), []byte("bad")...),
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			reader := tt.reader
			if reader == nil {
				reader = io.NopCloser(bytes.NewReader(tt.payload))
			}

			iter, err := wal.NewSegmentIterator(reader)
			require.NoError(t, err)
			defer iter.Close()

			next, err := iter.Next()
			if tt.err == "" {
				require.NoError(t, err)
				require.False(t, next)
				return
			}

			require.False(t, next)
			require.EqualError(t, err, tt.err)
		})
	}
}

func TestSegmentIterator_Next_ZeroLengthBlockStopsIteration(t *testing.T) {
	iter, err := wal.NewSegmentIterator(io.NopCloser(bytes.NewReader(appendSegmentHeader([]byte("ADXWAL  "), 0, 0))))
	require.NoError(t, err)
	defer iter.Close()

	next, err := iter.Next()
	require.NoError(t, err)
	require.False(t, next)
}

func TestSegmentIterator_Next_BlockWithoutSampleMetadata(t *testing.T) {
	encoded := s2.EncodeBetter(nil, []byte("plain-value"))
	payload := append(appendSegmentHeader([]byte("ADXWAL  "), uint32(len(encoded)), crc32.ChecksumIEEE(encoded)), encoded...)

	iter, err := wal.NewSegmentIterator(io.NopCloser(bytes.NewReader(payload)))
	require.NoError(t, err)
	defer iter.Close()

	next, err := iter.Next()
	require.NoError(t, err)
	require.True(t, next)
	require.Equal(t, []byte("plain-value"), iter.Value())

	st, count := iter.Metadata()
	require.Equal(t, wal.UnknownSampleType, st)
	require.Zero(t, count)
}

func appendSegmentHeader(dst []byte, blockLen uint32, crc uint32) []byte {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint32(buf[:4], blockLen)
	binary.BigEndian.PutUint32(buf[4:], crc)
	return append(dst, buf...)
}
