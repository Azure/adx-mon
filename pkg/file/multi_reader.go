package file

import (
	"io"
	"os"
)

type MultiReader struct {
	files   []*os.File
	readers []io.Reader
	mr      io.Reader
}

func NewMultiReader(paths ...string) (io.ReadCloser, error) {
	var (
		files   []*os.File
		readers []io.Reader
	)

	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}

		files = append(files, f)
		readers = append(readers, f)
	}

	return &MultiReader{
		files:   files,
		readers: readers,
		mr:      io.MultiReader(readers...),
	}, nil
}

func (m *MultiReader) Read(p []byte) (n int, err error) {
	return m.mr.Read(p)
}

func (m *MultiReader) Close() error {
	for _, f := range m.files {
		if err := f.Close(); err != nil {
			return err
		}
	}
	return nil
}
