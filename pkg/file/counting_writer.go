package file

import (
	"io"
	"sync/atomic"
)

type CountingWriter struct {
	writer       io.Writer
	bytesWritten int64
}

// New creates a new writer that wraps w.  The wrapping writer counts
// the number of bytes written to the wrapped writer.
func NewCountingWriter(w io.Writer) *CountingWriter {
	return &CountingWriter{
		writer:       w,
		bytesWritten: 0,
	}
}

func (w *CountingWriter) Write(b []byte) (int, error) {
	n, err := w.writer.Write(b)
	atomic.AddInt64(&w.bytesWritten, int64(n))
	return n, err
}

// BytesWritten returns the number of bytes that were written to the wrapped writer.
func (w *CountingWriter) BytesWritten() int64 {
	return atomic.LoadInt64(&w.bytesWritten)
}

func (w *CountingWriter) SetWritten(size int64) {
	atomic.StoreInt64(&w.bytesWritten, size)
}
