package reader

import (
	"io"
)

// CounterReader wraps an io.ReadCloser and counts the bytes read.
type CounterReader struct {
	rc    io.ReadCloser
	count int64
}

// NewCounterReader creates a new CounterReader.
func NewCounterReader(rc io.ReadCloser) *CounterReader {
	return &CounterReader{rc: rc}
}

// Read reads from the wrapped io.ReadCloser and counts the bytes read.
func (cr *CounterReader) Read(p []byte) (int, error) {
	n, err := cr.rc.Read(p)
	cr.count += int64(n)
	return n, err
}

// Close closes the wrapped io.ReadCloser.
func (cr *CounterReader) Close() error {
	return cr.rc.Close()
}

// Count returns the total number of bytes read.
func (cr *CounterReader) Count() int64 {
	return cr.count
}
