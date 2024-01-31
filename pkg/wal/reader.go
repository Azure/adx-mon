package wal

import (
	"io"
	"os"
)

func NewSegmentReader(path string) (*SegmentReader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	iter, err := NewSegmentIterator(f)
	if err != nil {
		return nil, err
	}
	return &SegmentReader{f: f, iter: iter}, nil
}

type SegmentReader struct {
	f    *os.File
	iter Iterator

	buf []byte
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

	n = copy(p, s.buf)
	s.buf = s.buf[n:]
	return n, nil
}

func (s *SegmentReader) Close() error {
	return s.f.Close()
}

func (s *SegmentReader) SampleMetadata() (t SampleType, sampleCount uint16) {
	return s.iter.Metadata()
}
