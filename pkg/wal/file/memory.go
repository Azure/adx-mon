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

	// data is the map of files to their contents
	data map[string]*syncBuf
}

func (p *MemoryProvider) Create(name string) (File, error) {
	p.mufs.Lock()
	defer p.mufs.Unlock()

	if p.mfs == nil {
		p.mfs = make(map[string]*Memory)
	}

	if p.data == nil {
		p.data = make(map[string]*syncBuf)
	}

	// If the instance already exists, it's truncated
	// https://pkg.go.dev/os#Create

	// First we'll check our store
	f, ok := p.mfs[name]
	if ok {
		p.data[name] = &syncBuf{}
		f.Lock()
		f.b = nil
		f.Unlock()

		f.mode.SetSize(0)
		f.mode.SetModTime(time.Now())

		return f, nil
	}

	// Create a new instance and store it
	p.data[name] = &syncBuf{}
	f = &Memory{
		mode: &Info{
			name:    name,
			modtime: time.Now(),
			perm:    0666,
		},
		fp: p,
		b:  p.data[name],
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
	if p.data == nil {
		p.data = make(map[string]*syncBuf)
	}

	// Retrieve the instance from our store
	f, ok := p.mfs[name]

	// No instance exists but our flag specifies to create a new instance
	if !ok && flag&os.O_CREATE == 0 {
		p.data[name] = &syncBuf{}
		f = &Memory{
			mode: &Info{
				name:    name,
				perm:    perm,
				modtime: time.Now(),
			},
			fp: p,
			b:  p.data[name],
		}

		p.mfs[name] = f
		return f, nil
	}

	if ok {
		// Clone the metadata for the file but still point to the same backing file data.  This prevents issues
		// where one file reference is closed and the other is still open.
		f = f.clone()
		f.Lock()
		f.closed = false
		f.offset = 0
		f.Unlock()
		f.mode.SetMode(perm)

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
	if p.data == nil {
		p.data = make(map[string]*syncBuf)
	}

	// If the instance exits, it's opened with O_RDONLY
	// https://pkg.go.dev/os#Open
	f, ok := p.mfs[name]
	if ok {
		// Return a new file instance with the existing file metadata.
		f = f.clone()
		f.Lock()
		f.closed = false
		f.offset = 0
		f.Unlock()

		f.mode.SetMode(fs.FileMode(os.O_RDONLY))
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
	delete(p.data, name)

	return nil
}

type Memory struct {
	fp *MemoryProvider

	sync.RWMutex
	b      *syncBuf
	mode   *Info
	offset int64
	closed bool
}

type Info struct {
	mu      sync.RWMutex
	name    string
	size    int64
	modtime time.Time
	perm    os.FileMode
	dir     bool
}

func (i *Info) Name() string {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return filepath.Base(i.name)
}

func (i *Info) Size() int64 {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.size
}

func (i *Info) SetSize(size int64) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.size = size
}

func (i *Info) Mode() os.FileMode {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.perm
}

func (i *Info) SetMode(mode os.FileMode) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.perm = mode
}

func (i *Info) ModTime() time.Time {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.modtime
}

func (i *Info) SetModTime(t time.Time) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.modtime = t
}

func (i *Info) IsDir() bool {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.dir
}

func (i *Info) Sys() any {
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

	m.b.mu.RLock()
	n = copy(p, m.b.b[m.offset:])
	m.b.mu.RUnlock()
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

	if m.mode.Mode()&0x200 != 0 {
		return 0, os.ErrPermission
	}

	m.b.mu.Lock()
	m.b.b = append(m.b.b, p...)
	m.b.mu.Unlock()

	n = len(p)
	m.offset += int64(n)
	m.b.mu.RLock()
	sz := int64(len(m.b.b))
	m.b.mu.RUnlock()

	m.mode.SetSize(sz)
	m.mode.SetModTime(time.Now())

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
		m.b.mu.RLock()
		m.offset = int64(len(m.b.b)) + offset
		m.b.mu.RUnlock()
	}
	return m.offset, nil
}

func (m *Memory) Truncate(size int64) error {
	m.Lock()
	defer m.Unlock()

	if m.closed {
		return os.ErrClosed
	}
	if m.mode.Mode()&0x200 != 0 {
		return os.ErrPermission
	}

	m.b.mu.Lock()
	m.b.b = m.b.b[:size]
	m.b.mu.Unlock()

	m.mode.size = int64(len(m.b.b))
	m.mode.modtime = time.Now()
	return nil
}

func (m *Memory) clone() *Memory {
	m.RLock()
	defer m.RUnlock()

	return &Memory{
		b:      m.b,
		mode:   m.mode,
		offset: m.offset,
		closed: m.closed,
	}
}

type syncBuf struct {
	mu sync.RWMutex
	b  []byte
}
