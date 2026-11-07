package wal

import (
	"bytes"
	"io"
	"os"
)

type Option func(*SegmentReader)

func WithSkipHeader(r *SegmentReader) {
	r.skipHeader = true
}

func NewSegmentReader(path string, opts ...Option) (*SegmentReader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	iter, err := NewSegmentIterator(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	r := &SegmentReader{f: f, path: path, iter: iter}
	for _, opt := range opts {
		opt(r)
	}
	return r, nil
}

type SegmentReader struct {
	f    *os.File
	path string
	iter Iterator

	buf        []byte
	skipHeader bool

	// eof is set once the segment is read to its end.  The file is closed and the iterator's buffers released then,
	// so a reader that is done does not hold them while later readers of the same upload are read.
	eof        bool
	sampleType SampleType
	samples    uint32
	// closed is set by Close.
	closed bool
}

func (s *SegmentReader) Read(p []byte) (n int, err error) {
	if s.eof {
		return 0, io.EOF
	}
	if len(s.buf) > 0 {
		n := copy(p, s.buf)
		s.buf = s.buf[n:]
		return n, nil
	}

	next, err := s.iter.Next()
	if err != nil {
		return 0, err
	}

	if !next {
		s.release()
		return 0, io.EOF
	}

	s.buf = s.iter.Value()

	var idx int
	if s.skipHeader {
		idx = bytes.IndexByte(s.buf, '\n') + 1
	}

	n = copy(p, s.buf[idx:])
	s.buf = s.buf[idx+n:]
	return n, nil
}

// release closes the segment's file and drops the iterator and its buffers once the segment is read to its end.  The
// sample metadata is kept.
func (s *SegmentReader) release() {
	s.sampleType, s.samples = s.iter.Metadata()
	s.eof = true
	_ = s.iter.Close()
	s.iter = nil
	s.buf = nil
}

// Close closes the segment's file.  It returns nil if the reader already closed it at the end of the segment.
func (s *SegmentReader) Close() error {
	if s.closed {
		return os.ErrClosed
	}
	s.closed = true
	if s.eof {
		return nil
	}
	return s.f.Close()
}

func (s *SegmentReader) SampleMetadata() (t SampleType, sampleCount uint32) {
	if s.eof {
		return s.sampleType, s.samples
	}
	return s.iter.Metadata()
}

func (s *SegmentReader) Path() string {
	return s.path
}
