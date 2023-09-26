package file

import (
	"io"
	"io/fs"
	"os"
)

// A light os.File interface
type File interface {
	fs.File
	io.Writer

	Create(name string) (File, error)
	OpenFile(name string, flag int, perm os.FileMode) (File, error)
	Open(name string) (File, error)
	Remove(name string) error

	Sync() error
	Seek(offset int64, whence int) (ret int64, err error)
	Truncate(size int64) error
}
