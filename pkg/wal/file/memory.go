package file

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	// Note that simulating a filesystem here is only necessary due to the way
	// wal hands off to segments. Deletions of segments are managed upstream,
	// which would seem like a problem since os.Remove is being used in those
	// cases; however, we're only utilizing this path for Collector wrt logs,
	// and Collector _is_ handling deletions through the file.Remove method.
	mufs sync.RWMutex
	mfs  map[string]*Memory
)

type Memory struct {
	sync.RWMutex
	b      []byte
	mode   *Info
	offset int64
	closed bool
}

type Info struct {
	name    string
	size    int64
	modtime time.Time
	perm    os.FileMode
	dir     bool
}

func (i Info) Name() string {
	return filepath.Base(i.name)
}

func (i Info) Size() int64 {
	return i.size
}

func (i Info) Mode() os.FileMode {
	return i.perm
}

func (i Info) ModTime() time.Time {
	return i.modtime
}

func (i Info) IsDir() bool {
	return i.dir
}

func (i Info) Sys() any {
	return nil
}

func store(m *Memory) {
	if mfs == nil {
		mfs = make(map[string]*Memory)
	}

	mfs[m.mode.name] = m
}

func load(name string) *Memory {
	if mfs == nil {
		return nil
	}

	return mfs[name]
}

func (m *Memory) Create(name string) (File, error) {
	// If the instance already exists, it's truncated
	// https://pkg.go.dev/os#Create

	// First we'll check our "filesystem"
	mufs.RLock()
	v := load(name)
	if v != nil {
		err := v.Truncate(0)
		mufs.RUnlock()
		return v, err
	}
	mufs.RUnlock()

	// Next we'll check if this instance has already been created
	m.RLock()
	if m.mode != nil && m.mode.name == name {
		m.RUnlock()
		err := m.Truncate(0)
		return m, err
	}
	m.RUnlock()

	// Create a new instance and store it
	m = &Memory{
		mode: &Info{
			name:    name,
			modtime: time.Now(),
			perm:    0666,
		},
	}

	mufs.Lock()
	store(m)
	mufs.Unlock()

	return m, nil
}

func (m *Memory) OpenFile(name string, flag int, perm os.FileMode) (File, error) {
	// If the instance exits, it's opened with O_RDONLY
	// https://pkg.go.dev/os#OpenFile
	mufs.Lock()
	v := load(name)
	if v != nil {
		v.Lock()
		mufs.Unlock()
		v.closed = false
		v.offset = 0
		v.mode.perm = fs.FileMode(os.O_RDONLY)
		v.Unlock()

		return v, nil
	}
	mufs.Unlock()

	// If the instance exists and flag O_CREATE is passed, a new instance is created
	m.Lock()
	if flag&os.O_CREATE == 0 {
		m = &Memory{
			mode: &Info{
				name:    name,
				perm:    perm,
				modtime: time.Now(),
			},
		}
	} else {
		m.Unlock()
		return nil, fs.ErrNotExist
	}

	m.mode.perm = perm
	m.closed = false
	m.Unlock()

	return m, nil
}

func (m *Memory) Open(name string) (File, error) {
	// If the instance exits, it's opened with O_RDONLY
	// https://pkg.go.dev/os#Open
	mufs.Lock()
	v := load(name)
	if v != nil {
		v.Lock()
		mufs.Unlock()
		v.closed = false
		v.mode.perm = fs.FileMode(os.O_RDONLY)
		v.offset = 0
		v.Unlock()

		return v, nil
	}
	mufs.Unlock()

	return nil, fs.ErrNotExist
}

func (m *Memory) Remove(name string) error {
	mufs.Lock()
	delete(mfs, name)
	mufs.Unlock()

	return nil
}

func (m *Memory) Stat() (fs.FileInfo, error) {
	m.RLock()
	defer m.RUnlock()

	return m.mode, nil
}

func (m *Memory) Read(p []byte) (n int, err error) {
	m.RLock()

	if m.closed {
		m.RUnlock()

		return 0, os.ErrClosed
	}

	n = copy(p, m.b[m.offset:])
	if n == 0 {
		err = io.EOF
	}
	m.RUnlock()

	m.Lock()
	m.offset += int64(n)
	m.Unlock()

	return
}

func (m *Memory) Close() error {
	m.Lock()
	m.offset = 0
	m.closed = true
	m.Unlock()

	return nil
}

func (m *Memory) Write(p []byte) (n int, err error) {
	m.Lock()
	defer m.Unlock()

	if m.closed {
		return 0, os.ErrClosed
	}
	if m.mode.perm&fs.FileMode(os.O_RDWR) == 0 {
		return 0, os.ErrPermission
	}

	m.b = append(m.b, p...)
	n = len(p)
	m.offset += int64(n)
	m.mode.size = int64(len(m.b))
	m.mode.modtime = time.Now()
	return
}

func (m *Memory) Sync() error {
	m.RLock()
	defer m.RUnlock()

	if m.closed {
		return os.ErrClosed
	}

	return nil
}

func (m *Memory) Seek(offset int64, whence int) (ret int64, err error) {
	m.Lock()
	defer m.Unlock()

	if m.closed {
		return 0, os.ErrClosed
	}

	switch whence {
	case 0:
		m.offset = offset
	case 1:
		m.offset += offset
	case 2:
		m.offset = int64(len(m.b)) + offset
	}
	return m.offset, nil
}

func (m *Memory) Truncate(size int64) error {
	m.Lock()
	defer m.Unlock()

	if m.closed {
		return os.ErrClosed
	}
	if m.mode.perm&fs.FileMode(os.O_RDWR) == 0 {
		return os.ErrPermission
	}

	m.b = m.b[:size]
	m.mode.size = int64(len(m.b))
	m.mode.modtime = time.Now()
	return nil
}
