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
	// mfs is our in-memory filesystem. We map filenames to MemF instances.
	mfs map[string]*MemF
)

// MemF represents a file on disk and contains the actual bytes, FileMode and mutex.
type MemF struct {
	sync.RWMutex
	b    []byte
	mode *Info
}

// Memory implements our File interface and is an instance of metadata for an MemF,
// which allows us to support concurrent readers, where Memory contains instance
// specific offsets and state onto the actual byte array backing as repsented by MemF.
type Memory struct {
	sync.RWMutex
	offset int64
	closed bool
	name   string
	perm   os.FileMode
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

func (m *Memory) Create(name string) (File, error) {
	// Lock our filesystem
	mufs.Lock()
	defer mufs.Unlock()

	if mfs == nil {
		mfs = make(map[string]*MemF)
	}

	// If the instance already exists, it's truncated
	// https://pkg.go.dev/os#Create

	// First we'll check our store
	f, ok := mfs[name]
	if ok {
		// Truncate the file
		f.Lock()
		f.b = f.b[:0]
		f.mode.size = 0
		f.mode.modtime = time.Now()
		f.Unlock()

		return &Memory{name: name}, nil
	}

	// Create a new instance and store it
	f = &MemF{
		mode: &Info{
			name:    name,
			modtime: time.Now(),
			perm:    0666,
		},
	}
	mfs[name] = f

	return &Memory{
		name: name,
		perm: os.FileMode(os.O_RDWR),
	}, nil
}

func (m *Memory) OpenFile(name string, flag int, perm os.FileMode) (File, error) {
	// Lock our filesystem
	mufs.Lock()
	defer mufs.Unlock()

	if mfs == nil {
		mfs = make(map[string]*MemF)
	}

	// Retrieve the instance from our store
	f, ok := mfs[name]

	// No instance exists but our flag specifies to create a new instance
	if !ok && flag&os.O_CREATE == 0 {
		f = &MemF{
			mode: &Info{
				name:    name,
				perm:    perm,
				modtime: time.Now(),
			},
		}

		mfs[name] = f
		return &Memory{name: name}, nil
	}

	// If the instance exits, it's opened with O_RDONLY
	// https://pkg.go.dev/os#OpenFile
	if ok {

		return &Memory{
			name: f.mode.name,
			perm: fs.FileMode(os.O_RDONLY),
		}, nil
	}

	return nil, fs.ErrNotExist
}

func (m *Memory) Open(name string) (File, error) {
	// Lock our filesystem
	mufs.RLock()
	defer mufs.RUnlock()

	if mfs == nil {
		return nil, fs.ErrNotExist
	}

	// If the instance exits, it's opened with O_RDONLY
	// https://pkg.go.dev/os#Open
	f, ok := mfs[name]
	if ok {
		return &Memory{
			name: f.mode.name,
			perm: fs.FileMode(os.O_RDONLY),
		}, nil
	}

	return nil, fs.ErrNotExist
}

func (m *Memory) Remove(name string) error {
	// Lock our filesystem
	mufs.Lock()
	defer mufs.Unlock()

	if mfs == nil {
		mfs = make(map[string]*MemF)
		return nil
	}

	// Remove the instance from our store
	delete(mfs, name)

	return nil
}

func (m *Memory) Stat() (fs.FileInfo, error) {
	// Lock our instance
	m.RLock()
	defer m.RUnlock()

	// Lock our filesystem
	mufs.RLock()
	defer mufs.RUnlock()

	// If our filesystem is nil, we don't have any instances
	if mfs == nil {
		return nil, fs.ErrNotExist
	}

	// Retrieve the instance from our store
	f, ok := mfs[m.name]
	if ok {
		return f.mode, nil
	}

	// If the instance doesn't exist, return an error
	return nil, fs.ErrNotExist
}

func (m *Memory) Read(p []byte) (n int, err error) {
	// Lock our instance
	m.RLock()
	defer m.RUnlock()

	// If the instance has been closed, return an error
	if m.closed {
		return 0, os.ErrClosed
	}

	// Lock our filesystem
	mufs.RLock()
	defer mufs.RUnlock()

	// Retrieve the underlying buffer represented by this instance
	f, ok := mfs[m.name]
	if !ok {
		return 0, fs.ErrNotExist
	}

	// Copy the underlying buffer to the provided buffer, taking
	// care to manage the instance's read offset.
	n = copy(p, f.b[m.offset:])
	if n == 0 {
		err = io.EOF
	}
	m.offset += int64(n)

	return
}

func (m *Memory) Close() error {
	// Lock our instance
	m.Lock()
	defer m.Unlock()

	// Set state
	m.offset = 0
	m.closed = true

	return nil
}

func (m *Memory) Write(p []byte) (n int, err error) {
	// Lock our instance
	m.Lock()
	defer m.Unlock()

	// If the instance has been closed, return an error
	if m.closed {
		return 0, os.ErrClosed
	}
	// If the instance is not writable, return an error
	if m.perm&fs.FileMode(os.O_RDWR) == 0 {
		return 0, os.ErrPermission
	}

	// Lock our filesystem
	mufs.Lock()
	defer mufs.Unlock()

	// Retrieve the underlying buffer represented by this instance
	f, ok := mfs[m.name]
	if !ok {
		return 0, fs.ErrNotExist
	}

	// Append the provided buffer to the underlying buffer, taking
	// care to manage the instance's write offset.
	f.b = append(f.b, p...)
	n = len(p)
	m.offset += int64(n)

	// Update the instance's metadata
	f.mode.size = int64(len(f.b))
	f.mode.modtime = time.Now()

	return
}

func (m *Memory) Sync() error {
	// Lock our instance
	m.RLock()
	defer m.RUnlock()

	// If the instance has been closed, return an error
	if m.closed {
		return os.ErrClosed
	}

	return nil
}

func (m *Memory) Seek(offset int64, whence int) (ret int64, err error) {
	// Lock our instance
	m.Lock()
	defer m.Unlock()

	// If the instance has been closed, return an error
	if m.closed {
		return 0, os.ErrClosed
	}

	// Lock our filesystem
	mufs.RLock()
	defer mufs.RUnlock()

	// Retrieve the underlying buffer represented by this instance
	f, ok := mfs[m.name]
	if !ok {
		return 0, fs.ErrNotExist
	}

	switch whence {
	case 0:
		// Seek relative to the origin of the file
		m.offset = offset
	case 1:
		// Seek relative to the current offset
		m.offset += offset
	case 2:
		// Seek relative to the end of the file
		m.offset = int64(len(f.b)) + offset
	}

	// If the offset is negative, return an error
	if m.offset < 0 {
		return 0, os.ErrInvalid
	}

	// If the offset is greater than the length of the underlying buffer,
	// return an error
	if m.offset > int64(len(f.b)) {
		return 0, os.ErrInvalid
	}

	// Return the offset
	return m.offset, nil
}

func (m *Memory) Truncate(size int64) error {
	// If the size is negative, return an error
	if size < 0 {
		return os.ErrInvalid
	}

	// Lock our instance
	m.Lock()
	defer m.Unlock()

	// If the instance has been closed, return an error
	if m.closed {
		return os.ErrClosed
	}
	// If the instance is not writable, return an error
	if m.perm&fs.FileMode(os.O_RDWR) == 0 {
		return os.ErrPermission
	}

	// Lock our filesystem
	mufs.Lock()
	defer mufs.Unlock()

	// Retrieve the underlying buffer represented by this instance
	f, ok := mfs[m.name]
	if !ok {
		return fs.ErrNotExist
	}

	// If the size is greater than the length of the underlying buffer,
	// return an error
	if size > int64(len(f.b)) {
		return os.ErrInvalid
	}

	// Truncate the underlying buffer
	f.b = f.b[:size]

	// Update the instance's metadata
	f.mode.size = int64(len(f.b))
	f.mode.modtime = time.Now()

	return nil
}
