package file

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type MemoryProvider struct {
	// Note that simulating a filesystem here is only necessary due to the way
	// wal hands off to segments. Deletions of segments are managed upstream,
	// which would seem like a problem since os.Remove is being used in those
	// cases; however, we're only utilizing this path for Collector wrt logs,
	// and Collector _is_ handling deletions through the file.Remove method.
	mufs sync.RWMutex
	mfs  map[string]*Memory
}

func (p *MemoryProvider) Create(name string) (File, error) {
	p.mufs.Lock()
	defer p.mufs.Unlock()

	if p.mfs == nil {
		p.mfs = make(map[string]*Memory)
	}

	// If the instance already exists, it's truncated
	// https://pkg.go.dev/os#Create

	// First we'll check our store
	f, ok := p.mfs[name]
	if ok {
		f.Lock()
		f.b = nil
		f.mode.size = 0
		f.mode.modtime = time.Now()
		f.Unlock()

		return f, nil
	}

	// Create a new instance and store it
	f = &Memory{
		mode: &Info{
			name:    name,
			modtime: time.Now(),
			perm:    0666,
		},
		fp: p,
	}
	p.mfs[name] = f

	return f, nil
}

func (p *MemoryProvider) OpenFile(name string, flag int, perm os.FileMode) (File, error) {
	p.mufs.Lock()
	defer p.mufs.Unlock()

	if p.mfs == nil {
		p.mfs = make(map[string]*Memory)
	}

	// Retrieve the instance from our store
	f, ok := p.mfs[name]

	// No instance exists but our flag specifies to create a new instance
	if !ok && flag&os.O_CREATE == 0 {
		f = &Memory{
			mode: &Info{
				name:    name,
				perm:    perm,
				modtime: time.Now(),
			},
			fp: p,
		}

		p.mfs[name] = f
		return f, nil
	}

	if ok {
		f.Lock()
		f.closed = false
		f.offset = 0
		f.mode.perm = perm
		f.Unlock()

		p.mfs[name] = f
		return f, nil
	}

	return nil, fs.ErrNotExist
}

func (p *MemoryProvider) Open(name string) (File, error) {
	p.mufs.Lock()
	defer p.mufs.Unlock()

	if p.mfs == nil {
		p.mfs = make(map[string]*Memory)
	}

	// If the instance exits, it's opened with O_RDONLY
	// https://pkg.go.dev/os#Open
	f, ok := p.mfs[name]
	if ok {
		f.Lock()
		f.closed = false
		f.offset = 0
		f.mode.perm = fs.FileMode(os.O_RDONLY)
		f.Unlock()

		p.mfs[name] = f
		return f, nil
	}

	return nil, fs.ErrNotExist
}

func (p *MemoryProvider) Remove(name string) error {
	p.mufs.Lock()
	defer p.mufs.Unlock()

	if p.mfs == nil {
		return nil
	}

	delete(p.mfs, name)

	return nil
}

type Memory struct {
	fp *MemoryProvider

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

func (m *Memory) Remove(name string) error {
	m.fp.Remove(name)
	return nil
}

func (m *Memory) Stat() (fs.FileInfo, error) {
	m.RLock()
	defer m.RUnlock()

	return m.mode, nil
}

func (m *Memory) Read(p []byte) (n int, err error) {
	m.Lock()
	defer m.Unlock()

	if m.closed {
		return 0, os.ErrClosed
	}

	n = copy(p, m.b[m.offset:])
	if n == 0 {
		err = io.EOF
	}
	m.offset += int64(n)

	return
}

func (m *Memory) Close() error {
	m.Lock()
	defer m.Unlock()

	m.offset = 0
	m.closed = true

	return nil
}

func (m *Memory) Write(p []byte) (n int, err error) {
	m.Lock()
	defer m.Unlock()

	if m.closed {
		return 0, os.ErrClosed
	}

	if m.mode.perm&0x200 != 0 {
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
	if m.mode.perm&0x200 != 0 {
		return os.ErrPermission
	}

	m.b = m.b[:size]
	m.mode.size = int64(len(m.b))
	m.mode.modtime = time.Now()
	return nil
}
