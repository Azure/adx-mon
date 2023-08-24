package wal

import (
	"bufio"
	"bytes"
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
	"github.com/Azure/adx-mon/pkg/pool"
	"github.com/Azure/adx-mon/pkg/ring"
	"github.com/davidnarayan/go-flake"
	"github.com/klauspost/compress/zstd"
)

const (
	// DefaultIOBufSize is the default buffer size for bufio.Writer.
	DefaultIOBufSize = 128 * 1024

	// DefaultRingSize is the default size of the ring buffer.
	DefaultRingSize = 1024
)

var (
	idgen *flake.Flake

	// encoder and decoder are used for compressing and decompressing blocks
	encoder, _ = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest))
	decoder, _ = zstd.NewReader(nil)

	// ringPool is a pool of ring buffers used for queuing writes to segments.  This allows these to be
	// re-used across segments.  We allow up to 10000 ring buffers to be allocated to match the max number of
	// tables allowed in Kusto.
	ringPool = pool.NewGeneric(10000, func(sz int) interface{} {
		return ring.NewBuffer(sz)
	})
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
	Size() (int64, error)
	CreatedAt() time.Time
	Reader() (io.ReadCloser, error)
	Path() string

	Iterator() (Iterator, error)
}

type Iterator interface {
	Next() (bool, error)
	Value() []byte
	Close() error
}

type segment struct {
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

	// encodeBuf is a buffer used for compressing blocks before writing to disk.
	encodeBuf []byte
	lenBuf    [8]byte

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

	bf := bufio.NewWriterSize(fw, DefaultIOBufSize)

	f := &segment{
		id:        flakeId.String(),
		createdAt: createdAt.UTC(),
		path:      path,
		w:         fw,
		bw:        bf,

		closing: make(chan struct{}),
		ringBuf: ringPool.Get(DefaultRingSize).(*ring.Buffer),
	}

	f.wg.Add(1)
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
	i := strings.LastIndex(fileName, "_")

	id := fileName[i+1:]

	createdAt, err := flakeutil.ParseFlakeID(id)
	if err != nil {
		return nil, err
	}

	fd, err := os.OpenFile(path, os.O_APPEND|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open segment: %s: %w", path, err)
	}

	bf := bufio.NewWriterSize(fd, DefaultIOBufSize)

	f := &segment{
		id:        id,
		createdAt: createdAt,
		path:      path,

		w:       fd,
		bw:      bf,
		closing: make(chan struct{}),
		ringBuf: ring.NewBuffer(DefaultRingSize),
	}

	if err := f.repair(); err != nil {
		return nil, err
	}

	f.wg.Add(1)
	go f.flusher()

	return f, nil
}

// Path returns the path on disk of the segment.
func (s *segment) Path() string {
	return s.path
}

// Reader returns an io.Reader for the segment.  The Reader returns segment data automatically handling segment
// blocks and validation.
func (s *segment) Reader() (io.ReadCloser, error) {
	return NewSegmentReader(s.Path())
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
	return NewSegmentIterator(f)
}

// Write writes buf to the segment.
func (s *segment) Write(ctx context.Context, buf []byte) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry := s.ringBuf.Reserve()
	defer s.ringBuf.Release(entry)

	entry.Value = append(entry.Value[:0], buf...)

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
	f, err := s.Reader()
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return io.ReadAll(f)
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
	ringPool.Put(s.ringBuf)
	s.ringBuf = nil

	if err := s.w.Sync(); errors.Is(err, os.ErrClosed) {
		return nil
	} else if err != nil {
		return err
	}

	return s.w.Close()
}

// repair truncates the last bytes in the segment if they are missing, corrupted or have extra data.  This
// repairs any corrupted segment that may not have fully flushed to disk safely.
func (s *segment) repair() error {
	buf := make([]byte, 0, 4096)

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

		if err == io.EOF {
			return nil
		}

		if err != nil || n != 8 {
			logger.Warn("Repairing segment %s, missing block header, truncating at %d", s.path, lastGoodIdx)
			return s.truncate(int64(lastGoodIdx))
		}

		blockLen := binary.BigEndian.Uint32(lenCrcBuf[:4])
		if uint32(cap(buf)) < blockLen {
			buf = make([]byte, 0, blockLen)
		}

		crc := binary.BigEndian.Uint32(lenCrcBuf[4:8])

		n, err = s.w.Read(buf[:blockLen])
		idx += n
		if err != nil {
			return err
		}

		if uint32(n) != blockLen {
			logger.Warn("Repairing segment %s, short block, truncating at %d", s.path, lastGoodIdx)
			return s.truncate(int64(lastGoodIdx))
		}

		if crc32.ChecksumIEEE(buf[:blockLen]) != crc {
			logger.Warn("Repairing segment %s, checksum failed, truncating at %d", s.path, lastGoodIdx)
			return s.truncate(int64(lastGoodIdx))
		}

		lastGoodIdx = idx
	}
}

func (s *segment) flusher() {
	defer s.wg.Done()

	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()

	blockBuf := bytes.NewBuffer(make([]byte, 0, 4*1024))
	for {
		select {
		case req := <-s.ringBuf.Queue():

			blockBuf.Reset()
			err := s.blockWrite(blockBuf, req.Value)
			select {
			case req.ErrCh <- err:
			default:
			}

			s.flushQueue(blockBuf)

			s.flushBlock(blockBuf, req)

		case <-t.C:
			if err := s.bw.Flush(); err != nil {
				logger.Error("Failed to flush writer for segment: %s: %s", s.path, err)
			}
		case <-s.closing:
			blockBuf.Reset()
			s.flushQueue(blockBuf)

			req := &ring.Entry{ErrCh: make(chan error, 1)}
			s.flushBlock(blockBuf, req)

			if err := s.bw.Flush(); err != nil {
				logger.Error("Failed to flush writer for segment: %s: %s", s.path, err)
			}

			if err := s.w.Sync(); err != nil {
				logger.Error("Failed to sync segment: %s: %s", s.path, err)
			}

			select {
			case err := <-req.ErrCh:
				if err != nil {
					logger.Error("Failed to flush block when closing segment: %s", err)
				}
			default:
			}

			return
		}
	}
}

func (s *segment) flushBlock(blockBuf *bytes.Buffer, req *ring.Entry) {
	if blockBuf.Len() == 0 {
		req.ErrCh <- nil
		return
	}

	s.encodeBuf = encoder.EncodeAll(blockBuf.Bytes(), s.encodeBuf[:0])

	err := s.blockWrite(s.bw, s.encodeBuf)

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
	}

}

// blockWrite writes length and CRC32 prefixed block to w
func (s *segment) blockWrite(w io.Writer, buf []byte) error {
	if len(buf) == 0 {
		return nil
	}

	binary.BigEndian.PutUint32(s.lenBuf[:4], uint32(len(buf)))
	binary.BigEndian.PutUint32(s.lenBuf[4:8], crc32.ChecksumIEEE(buf))
	n, err := w.Write(s.lenBuf[:8])
	if err != nil {
		return err
	} else if n != 8 {
		return io.ErrShortWrite
	}

	n, err = w.Write(buf)
	if err != nil {
		return err
	} else if n != len(buf) {
		return io.ErrShortWrite
	}
	return nil
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
