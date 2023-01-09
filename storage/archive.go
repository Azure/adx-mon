package storage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Archive interface {
	io.WriteCloser
	Path() string
	Table() string
}

type Archiver interface {
	Archive(s Segment) (Archive, error)
}

type fileArchive struct {
	path    string
	f       *os.File
	version string
	table   string
}

func (f *fileArchive) Table() string {
	return f.table
}

func (f *fileArchive) Path() string {
	return f.path
}

func (f *fileArchive) Write(p []byte) (n int, err error) {
	return f.f.Write(p)
}

func (f *fileArchive) Close() error {
	return f.f.Close()
}

type fileArchiver struct{}

func (f *fileArchiver) Archive(s Segment) (Archive, error) {
	path := s.Path()
	fileName := filepath.Base(path)
	fields := strings.Split(fileName, "_")

	fileName = strings.TrimSuffix(fileName, filepath.Ext(path))
	fileName = fmt.Sprintf("%s.csv.gz", fileName)

	path = filepath.Join(filepath.Dir(path), fileName)

	fd, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open archive %s: %w", path, err)
	}

	return &fileArchive{
		f:       fd,
		path:    path,
		table:   fields[0],
		version: fields[1],
	}, nil
}
