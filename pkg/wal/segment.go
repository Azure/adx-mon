package wal

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	flakeutil "github.com/Azure/adx-mon/pkg/flake"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/ring"
	"github.com/davidnarayan/go-flake"
)

const (
	// DefaultIOBufSize is the default buffer size for bufio.Writer.
	DefaultIOBufSize = 8 * 1024

	// DefaultRingSize is the default size of the ring buffer.
	DefaultRingSize = 1024

	// DefaultFlushThreshold is the size of uncompressed, buffered writes that will be flushed to disk if exceeded.
	DefaultFlushThreshold = 1024 * 1024
)

var (
	idgen *flake.Flake
)

func init() {
	var err error
	idgen, err = flake.New()
	if err != nil {
		panic(err)
	}
}

type Segment interface {
	Write(ctx context.Context, buf []byte) error
	Bytes() ([]byte, error)
	Close() error
	ID() string
	Name() string
	Size() (int64, error)
	CreatedAt() time.Time
	Reader() (io.Reader, error)
	Path() string

	Iterator() (Iterator, error)
}

type Iterator interface {
	Next() (bool, error)
	Value() []byte
	Close() error
}

type segment struct {
	// name is the first part of the segment file name.  Segments with the same name are group together.
	name string
	// id is the time-ordered ID and allows for segment files to be sorted lexicographically and in time order of
	// creating.
	id        string
	createdAt time.Time
	path      string

	wg sync.WaitGroup
	mu sync.RWMutex

	// w is the underlying segment file on disk
	w *os.File

	// bw is a buffered writer for w that if flushed to disk in batches.
	bw *bufio.Writer

	// gw is the writer used for compressing blocks.
	gw    *gzip.Writer
	gzbuf *bytes.Buffer

	closing chan struct{}
	closed  bool

	// ringBuf is a circular buffer that queues writes to allow for large IO batches to disk.
	ringBuf *ring.Buffer
}

func NewSegment(dir, prefix string) (Segment, error) {
	flakeId := idgen.NextId()

	createdAt, err := flakeutil.ParseFlakeID(flakeId.String())
	if err != nil {
		return nil, err
	}

	fileName := fmt.Sprintf("%s_%s.wal", prefix, flakeId.String())
	path := filepath.Join(dir, fileName)
	fw, err := os.Create(path)
	if err != nil {
		return nil, err
	}

	fields := strings.Split(prefix, "_")

	bf := bufio.NewWriterSize(fw, DefaultIOBufSize)
	gzbuf := bytes.NewBuffer(make([]byte, 0, 1024))

	f := &segment{
		name:      fields[0],
		id:        flakeId.String(),
		createdAt: createdAt.UTC(),
		path:      path,
		w:         fw,
		bw:        bf,
		gw:        gzip.NewWriter(gzbuf),
		gzbuf:     gzbuf,

		closing: make(chan struct{}),
		ringBuf: ring.NewBuffer(DefaultRingSize),
	}

	go f.flusher()
	return f, nil
}

func Open(path string) (Segment, error) {
	ext := filepath.Ext(path)
	if ext != ".wal" {
		return nil, fmt.Errorf("invalid segment filename: %s", path)
	}

	fileName := filepath.Base(path)
	fileName = strings.TrimSuffix(fileName, filepath.Ext(fileName))
	fields := strings.Split(fileName, "_")

	if len(fields) != 2 {
		return nil, fmt.Errorf("invalid segment filename: %s", path)
	}

	name := fields[0]
	id := fields[1]

	createdAt, err := flakeutil.ParseFlakeID(id)
	if err != nil {
		return nil, err
	}

	fd, err := os.OpenFile(path, os.O_APPEND|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open segment: %s: %w", path, err)
	}

	bf := bufio.NewWriterSize(fd, DefaultIOBufSize)

	gzbuf := bytes.NewBuffer(make([]byte, 0, 1024))

	f := &segment{
		name:      name,
		id:        id,
		createdAt: createdAt,
		path:      path,

		w:       fd,
		bw:      bf,
		gw:      gzip.NewWriter(gzbuf),
		gzbuf:   gzbuf,
		closing: make(chan struct{}),
		ringBuf: ring.NewBuffer(DefaultRingSize),
	}

	if err := f.repair(); err != nil {
		return nil, err
	}

	go f.flusher()

	return f, nil
}

// Path returns the path on disk of the segment.
func (s *segment) Path() string {
	return s.path
}

// Name returns the segment name.
func (s *segment) Name() string {
	return s.name
}

// Reader returns an io.Reader for the segement.
func (s *segment) Reader() (io.Reader, error) {
	return os.Open(s.path)
}

// CreateAt returns the time when the segment was created.
func (s *segment) CreatedAt() time.Time {
	return s.createdAt
}

// Size returns the current size of the segment file on disk.
func (s *segment) Size() (int64, error) {
	stat, err := os.Stat(s.path)
	if err != nil {
		return 0, err
	}
	return stat.Size(), nil
}

// ID returns the ID of the segment.
func (s *segment) ID() string {
	return s.id
}

// Iterator returns an iterator to read values written to the segment.  Creating an iterator on a segment that is
// still being written is not supported.
func (s *segment) Iterator() (Iterator, error) {
	f, err := os.Open(s.path)
	if err != nil {
		return nil, err
	}
	return &blockIterator{f: f, buf: make([]byte, 0, 4096)}, nil
}

// Write writes buf to the segment.
func (s *segment) Write(ctx context.Context, buf []byte) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry := s.ringBuf.Reserve()
	defer s.ringBuf.Release(entry)

	entry.Value = buf
	s.ringBuf.Enqueue(entry)

	select {
	case err := <-entry.ErrCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Bytes returns full segment file as byte slice.
func (s *segment) Bytes() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.ReadFile(s.w.Name())
}

// Close closes the segment for writing.
func (s *segment) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	// Close the channel without holding the lock so goroutines can exit cleanly
	close(s.closing)
	s.closed = true

	// Wait for flusher goroutine to flush any in-flight writes
	s.wg.Wait()

	s.bw = nil
	s.ringBuf = nil

	if err := s.w.Sync(); errors.Is(err, os.ErrClosed) {
		return nil
	} else if err != nil {
		return err
	}

	return s.w.Close()
}

// repair truncates the last bytes in the segment if they don't are missing, corrupted or have extra data.  This
// repairs any corrupted segment that may not have fully flushed to disk safely.
func (s *segment) repair() error {
	buf := make([]byte, 4096)

	if _, err := s.w.Seek(0, io.SeekStart); err != nil {
		return err
	}

	var (
		lastGoodIdx, idx int
		lenCrcBuf        [8]byte
	)
	for {
		// Read the block length
		n, err := s.w.Read(lenCrcBuf[:8])
		idx += n

		if err != nil || n != 8 {
			return s.truncate(int64(lastGoodIdx))
		}

		blockLen := binary.BigEndian.Uint32(lenCrcBuf[:4])
		if uint32(cap(buf)) < blockLen {
			buf = make([]byte, blockLen)
		}

		crc := binary.BigEndian.Uint32(lenCrcBuf[4:8])

		n, err = s.w.Read(buf[:blockLen])
		idx += n
		if err != nil {
			return err
		}

		if uint32(n) != blockLen {
			return s.truncate(int64(lastGoodIdx))
		}

		if crc32.ChecksumIEEE(buf[:blockLen]) != crc {
			return s.truncate(int64(lastGoodIdx))
		}

		lastGoodIdx = idx
	}
}

func (s *segment) flusher() {
	s.wg.Add(1)
	defer s.wg.Done()

	blockBuf := bytes.NewBuffer(make([]byte, 4*1024))
	for {
		select {
		case req := <-s.ringBuf.Queue():

			blockBuf.Reset()
			err := s.blockWrite(blockBuf, req.Value)
			select {
			case req.ErrCh <- err:
			default:
			}

			var n int
			for len(s.ringBuf.Queue()) > 0 {
				req = <-s.ringBuf.Queue()

				err := s.blockWrite(blockBuf, req.Value)
				select {
				case req.ErrCh <- err:
				default:
				}
				n++

				if blockBuf.Len() >= DefaultFlushThreshold {
					break
				}
			}

			s.flushBlock(blockBuf, req)
		case <-s.closing:
			blockBuf.Reset()
			s.flushQueue(blockBuf)

			req := &ring.Entry{ErrCh: make(chan error)}
			s.flushBlock(blockBuf, req)

			select {
			case err := <-req.ErrCh:
				logger.Error("Failed to flush block when closing segment: %s", err)
			default:
			}

			return
		}
	}
}

func (s *segment) flushBlock(blockBuf *bytes.Buffer, req *ring.Entry) {
	s.gzbuf.Reset()
	s.gw.Reset(s.gzbuf)

	_, err := s.gw.Write(blockBuf.Bytes())
	select {
	case req.ErrCh <- err:
	default:
	}

	err = s.gw.Flush()
	select {
	case req.ErrCh <- err:
	default:
	}

	err = s.blockWrite(s.bw, blockBuf.Bytes())
	select {
	case req.ErrCh <- err:
	default:
	}

	err = s.bw.Flush()
	select {
	case req.ErrCh <- err:
	default:
	}
}

func (s *segment) flushQueue(w io.Writer) {
	for len(s.ringBuf.Queue()) > 0 {
		req := <-s.ringBuf.Queue()

		err := s.blockWrite(w, req.Value)
		select {
		case req.ErrCh <- err:
		default:
		}
		continue
	}

}

// blockWrite writes length and CRC32 prefixed block to w
func (s *segment) blockWrite(w io.Writer, buf []byte) error {
	if len(buf) == 0 {
		return nil
	}

	var lenBuf [8]byte
	binary.BigEndian.PutUint32(lenBuf[:4], uint32(len(buf)))
	binary.BigEndian.PutUint32(lenBuf[4:8], crc32.ChecksumIEEE(buf))
	_, err := w.Write(lenBuf[:8])
	if err != nil {
		return err
	}

	_, err = w.Write(buf)
	return err
}

func (s *segment) truncate(ofs int64) error {
	if err := s.w.Truncate(ofs); err != nil {
		return err
	}
	if err := s.w.Sync(); err != nil {
		return err
	}

	return nil
}

// blockIterator is an iterator for a segment file.  It allows reading back values written to the segment in the
// same order they were written.
type blockIterator struct {
	// f is the underlying segment file on disk.
	f *os.File

	// n is the index into buf that allows iterating through values in a block.
	n int

	// buf is last read block from disk.  This block holds multiple values corresponding
	// to segment writes.
	buf []byte

	// value is the current value that is returned for the iterator.
	value []byte

	// lenCrcBuf is a temp buffer to re-use for extracting the 8 byte (4 len, 4 crc) values
	// when iterating.
	lenCrcBuf [8]byte
}

func (b *blockIterator) Next() (bool, error) {
	// Each block may have multiple entries corresponding to each call to Write
	if b.n < len(b.buf) {
		return b.nextValue()
	}

	// Read the block length and CRC
	n, err := b.f.Read(b.lenCrcBuf[:8])
	if err == io.EOF {
		return false, err
	} else if err != nil || n != 8 {
		return false, fmt.Errorf("short read: expected 8, got %d", n)
	}

	// Extract the block length and expand the read buffer if it is too small.
	blockLen := binary.BigEndian.Uint32(b.lenCrcBuf[:4])
	if uint32(cap(b.buf)) < blockLen {
		b.buf = make([]byte, blockLen)
	}

	// Extract the CRC value for the block
	crc := binary.BigEndian.Uint32(b.lenCrcBuf[4:8])

	// Read the expected block length bytes
	n, err = b.f.Read(b.buf[:blockLen])
	if err != nil {
		return false, err
	}

	// Make sure we actually read the number of bytes we were expecting.
	if uint32(n) != blockLen {
		return false, fmt.Errorf("short block read: expected %d, got %d", blockLen, n)
	}

	// Validate the block checksum matches still
	if crc32.ChecksumIEEE(b.buf[:blockLen]) != crc {
		return false, fmt.Errorf("block checksum verification failed")
	}

	// Setup internal iterator indexing on this block.
	b.buf = b.buf[:n]
	b.n = 0

	// Unwrap the first value in this block.
	return b.nextValue()
}

func (b *blockIterator) nextValue() (bool, error) {
	blockLen := binary.BigEndian.Uint32(b.buf[b.n : b.n+4])
	crc := binary.BigEndian.Uint32(b.buf[b.n+4 : b.n+8])

	if int(blockLen) > len(b.buf[b.n+8:]) {
		return false, fmt.Errorf("short block read: expected %d, got %d", blockLen, len(b.buf[b.n+8:]))
	}

	value := b.buf[b.n+8 : b.n+8+int(blockLen)]

	if crc32.ChecksumIEEE(value) != crc {
		return false, fmt.Errorf("block checksum verification failed")
	}

	b.value = value
	b.n += 8 + int(blockLen)

	return true, nil
}

func (b *blockIterator) Value() []byte {
	return b.value
}

func (b *blockIterator) Close() error {
	return b.f.Close()
}
