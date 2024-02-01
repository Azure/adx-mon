package wal

import (
	"bytes"
	"io"
	"os"
)

type SegmentMerger struct {
	files []*os.File
	mr    io.Reader
}

func NewSegmentMerger(paths ...string) (io.ReadCloser, error) {
	var (
		readers []io.Reader
		files   []*os.File
	)

	// Each segment file starts with segment header.  We want to merge all the segments into a single
	// reader, so we need to include the segment header first and then just the block date for the rest.
	readers = append(readers, bytes.NewReader(segmentMagic[:]))

	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}

		// skipReader skips the first 8 bytes of the segment file which is the segment file header.
		readers = append(readers, &skipReader{r: f})
		files = append(files, f)
	}

	return &SegmentMerger{
		files: files,
		mr:    io.MultiReader(readers...),
	}, nil
}

func (m *SegmentMerger) Read(p []byte) (n int, err error) {
	return m.mr.Read(p)
}

func (m *SegmentMerger) Close() error {
	for _, f := range m.files {
		if err := f.Close(); err != nil {
			return err
		}
	}
	return nil
}

type skipReader struct {
	r       io.Reader
	skipped bool
}

func (sr *skipReader) Read(p []byte) (n int, err error) {
	if !sr.skipped {
		// Skip the first 8 bytes which is the segment file header.
		_, err = io.CopyN(io.Discard, sr.r, 8)
		if err != nil {
			return
		}
		sr.skipped = true
	}

	// Read the rest
	return sr.r.Read(p)
}
