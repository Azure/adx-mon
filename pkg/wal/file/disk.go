package file

import (
	"io/fs"
	"os"
)

type DiskProvider struct{}

func (d *DiskProvider) Create(name string) (File, error) {
	df := &Disk{}
	var err error
	df.f, err = os.Create(name)
	return df, err
}

func (d *DiskProvider) OpenFile(name string, flag int, perm os.FileMode) (File, error) {
	df := &Disk{}
	var err error
	df.f, err = os.OpenFile(name, flag, perm)
	return df, err
}

func (d *DiskProvider) Open(name string) (File, error) {
	df := &Disk{}
	var err error
	df.f, err = os.Open(name)
	return df, err
}

type Disk struct {
	f *os.File
}

func (d *Disk) Stat() (fs.FileInfo, error) {
	return d.f.Stat()
}

func (d *Disk) Read(p []byte) (int, error) {
	return d.f.Read(p)
}

func (d *Disk) Close() error {
	return d.f.Close()
}

func (d *Disk) Remove(name string) error {
	return os.Remove(name)
}

func (d *Disk) Write(p []byte) (int, error) {
	return d.f.Write(p)
}

func (d *Disk) Sync() error {
	return d.f.Sync()
}

func (d *Disk) Seek(offset int64, whence int) (ret int64, err error) {
	return d.f.Seek(offset, whence)
}

func (d *Disk) Truncate(size int64) error {
	return d.f.Truncate(size)
}
