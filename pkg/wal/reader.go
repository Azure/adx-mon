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
		return nil, err
	}
	r := &SegmentReader{f: f, iter: iter}
	for _, opt := range opts {
		opt(r)
	}
	return r, nil
}

type SegmentReader struct {
	f    *os.File
	iter Iterator

	buf        []byte
	skipHeader bool
}

func (s *SegmentReader) Read(p []byte) (n int, err error) {
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

func (s *SegmentReader) Close() error {
	return s.f.Close()
}

func (s *SegmentReader) SampleMetadata() (t SampleType, sampleCount uint32) {
	return s.iter.Metadata()
}

func (s *SegmentReader) Path() string {
	return s.f.Name()
}
