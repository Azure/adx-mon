package file

import (
	"io/fs"
	"os"
)

type Disk struct {
	f *os.File
}

func (d *Disk) Create(name string) (File, error) {
	if d == nil {
		d = &Disk{}
	}
	var err error
	d.f, err = os.Create(name)
	return d, err
}

func (d *Disk) OpenFile(name string, flag int, perm os.FileMode) (File, error) {
	if d == nil {
		d = &Disk{}
	}
	var err error
	d.f, err = os.OpenFile(name, flag, perm)
	return d, err
}

func (d *Disk) Open(name string) (File, error) {
	if d == nil {
		d = &Disk{}
	}
	var err error
	d.f, err = os.Open(name)
	return d, err
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
