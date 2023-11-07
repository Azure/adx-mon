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

	Remove(name string) error

	Sync() error
	Seek(offset int64, whence int) (ret int64, err error)
	Truncate(size int64) error
	ReadDir(i int) ([]os.DirEntry, error)
}
