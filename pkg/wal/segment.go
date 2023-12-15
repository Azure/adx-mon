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
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	pkgfile "github.com/Azure/adx-mon/pkg/file"
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

	// encoder and decoder pools are used for compressing and decompressing blocks
	encoders []*zstd.Encoder
	decoders []*zstd.Decoder

	// ringPool is a pool of ring buffers used for queuing writes to segments.  This allows these to be
	// re-used across segments.  We allow up to 10000 ring buffers to be allocated to match the max number of
	// tables allowed in Kusto.
	ringPool = pool.NewGeneric(10000, func(sz int) interface{} {
		return ring.NewBuffer(sz)
	})

	bwPool = pool.NewGeneric(10000, func(sz int) interface{} {
		return bufio.NewWriterSize(nil, DefaultIOBufSize)
	})

	ErrSegmentClosed = errors.New("segment closed")
)

func init() {
	var err error
	idgen, err = flake.New()
	if err != nil {
		panic(err)
	}

	SetEncoderPoolSize(16)
	SetDecoderPoolSize(16)
}

// SetEncoderPoolSize sets the size of the encoder pool.
func SetEncoderPoolSize(sz int) {
	encoders = make([]*zstd.Encoder, sz)
	for i := 0; i < len(encoders); i++ {
		encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest))
		if err != nil {
			panic(err)
		}
		encoders[i] = encoder
	}
}

// SetDecoderPoolSize sets the size of the decoder pool.
func SetDecoderPoolSize(sz int) {
	decoders = make([]*zstd.Decoder, sz)
	for i := 0; i < len(decoders); i++ {
		decoder, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(0))
		if err != nil {
			panic(err)
		}
		decoders[i] = decoder
	}
}

type Segment interface {
	Append(ctx context.Context, buf []byte) error
	Write(ctx context.Context, buf []byte, opts ...WriteOption) error
	Bytes() ([]byte, error)
	Close() error
	ID() string
	Size() (int64, error)
	CreatedAt() time.Time
	Reader() (io.ReadCloser, error)
	Path() string

	Iterator() (Iterator, error)
	Info() (SegmentInfo, error)
	Flush() error
}

type Iterator interface {
	Next() (bool, error)
	Value() []byte
	Metadata() (SampleType, uint16)
	Close() error
	Verify() (int, error)
}

type segment struct {
	// id is the time-ordered ID and allows for segment files to be sorted lexicographically and in time order of
	// creating.
	id        string
	createdAt time.Time
	path      string
	prefix    string

	wg sync.WaitGroup
	mu sync.RWMutex

	// w is the underlying segment file on disk
	w *os.File

	// bw is a buffered writer for w that if flushed to disk in batches.
	bw *bufio.Writer
	cw *pkgfile.CountingWriter

	// encodeBuf is a buffer used for compressing blocks before writing to file.
	encodeBuf []byte
	lenBuf    [12]byte
	encoder   *zstd.Encoder

	// metadata about the segment contents
	sampleType  uint16
	sampleCount uint16

	closing chan struct{}
	closed  bool

	// ringBuf is a circular buffer that queues writes to allow for large IO batches to file.
	ringBuf *ring.Buffer

	// appendCh is channel used to append raw blocks to the segment.
	appendCh chan ring.Entry
	flushCh  chan chan error
}

func NewSegment(dir, prefix string) (Segment, error) {
	flakeId := idgen.NextId()

	createdAt, err := flakeutil.ParseFlakeID(flakeId.String())
	if err != nil {
		return nil, err
	}

	fileName := fmt.Sprintf("%s_%s.wal", prefix, flakeId.String())
	if !fs.ValidPath(fileName) {
		return nil, fmt.Errorf("invalid segment filename: %s", fileName)
	}
	path := filepath.Join(dir, fileName)
	fw, err := os.Create(path)
	if err != nil {
		return nil, err
	}

	cw := pkgfile.NewCountingWriter(fw)

	bf := bwPool.Get(0).(*bufio.Writer)
	bf.Reset(cw)

	f := &segment{
		id:        flakeId.String(),
		prefix:    prefix,
		createdAt: createdAt.UTC(),
		path:      path,
		w:         fw,
		bw:        bf,
		cw:        cw,

		closing:  make(chan struct{}),
		ringBuf:  ringPool.Get(DefaultRingSize).(*ring.Buffer),
		encoder:  encoders[rand.Intn(len(encoders))],
		appendCh: make(chan ring.Entry, DefaultRingSize),
		flushCh:  make(chan chan error),
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

	prefix := fileName[:i]
	id := fileName[i+1:]

	createdAt, err := flakeutil.ParseFlakeID(id)
	if err != nil {
		return nil, err
	}

	fd, err := os.OpenFile(path, os.O_APPEND|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open segment: %s: %fp", path, err)
	}

	stat, err := fd.Stat()
	if err != nil {
		return nil, err
	}

	cw := pkgfile.NewCountingWriter(fd)
	cw.SetWritten(stat.Size())

	bf := bufio.NewWriterSize(fd, DefaultIOBufSize)
	bf.Reset(cw)

	f := &segment{
		id:        id,
		prefix:    prefix,
		createdAt: createdAt,
		path:      path,

		w:        fd,
		bw:       bf,
		cw:       cw,
		closing:  make(chan struct{}),
		ringBuf:  ring.NewBuffer(DefaultRingSize),
		encoder:  encoders[rand.Intn(len(encoders))],
		appendCh: make(chan ring.Entry, DefaultRingSize),
		flushCh:  make(chan chan error),
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
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrSegmentClosed
	}

	return NewSegmentReader(s.Path())
}

// CreateAt returns the time when the segment was created.
func (s *segment) CreatedAt() time.Time {
	return s.createdAt
}

// Size returns the current size of the segment file on file.
func (s *segment) Size() (int64, error) {
	return s.cw.BytesWritten(), nil
}

// ID returns the ID of the segment.
func (s *segment) ID() string {
	return s.id
}

func (s *segment) Info() (SegmentInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return SegmentInfo{}, ErrSegmentClosed
	}

	sz, err := s.Size()
	if err != nil {
		return SegmentInfo{}, err
	}

	return SegmentInfo{
		Prefix:    s.prefix,
		Ulid:      s.id,
		Size:      sz,
		CreatedAt: s.createdAt,
		Path:      s.path,
	}, nil
}

// Iterator returns an iterator to read values written to the segment.  Creating an iterator on a segment that is
// still being written is not supported.
func (s *segment) Iterator() (Iterator, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrSegmentClosed
	}

	f, err := os.Open(s.path)
	if err != nil {
		return nil, err
	}
	return NewSegmentIterator(f)
}

// Append appends a raw blocks to the segment.  This is used for appending blocks that have already been compressed.
// Misuse of this func could lead to data corruption.  In general, you probably want to use Write instead.
func (s *segment) Append(ctx context.Context, buf []byte) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return ErrSegmentClosed
	}

	iter, err := NewSegmentIterator(io.NopCloser(bytes.NewReader(buf)))
	if err != nil {
		return err
	}

	// Verify the block is valid before appending
	if n, err := iter.Verify(); err != nil {
		return err
	} else if n == 0 {
		return nil
	}

	entry := ring.Entry{Value: buf, ErrCh: make(chan error, 1)}
	s.appendCh <- entry
	select {
	case err := <-entry.ErrCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Write writes buf to the segment.
func (s *segment) Write(ctx context.Context, buf []byte, options ...WriteOption) error {
	for _, opt := range options {
		opt(s)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return ErrSegmentClosed
	}

	entry := s.ringBuf.Reserve()
	defer s.ringBuf.Release(entry)

	// Entries are re-used, so we need to reset the error channel before enqueuing the entry to prevent exiting
	// this func and adding the entry back to the ringbuffer before it's actually be flushed.
	select {
	case <-entry.ErrCh:
	default:
	}

	if cap(entry.Value) < len(buf) {
		entry.Value = make([]byte, 0, len(buf))
	}
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

func (s *segment) Flush() error {
	doneCh := make(chan error)
	s.flushCh <- doneCh
	return <-doneCh
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

	s.encoder = nil
	bwPool.Put(s.bw)
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
		lenCrcBuf        [12]byte
	)
	for {
		// Read the block length
		n, err := s.w.Read(lenCrcBuf[:12])
		idx += n

		if err == io.EOF {
			return nil
		}

		if err != nil || n != 12 {
			logger.Warnf("Repairing segment %s, missing block header, truncating at %d", s.path, lastGoodIdx)
			return s.truncate(int64(lastGoodIdx))
		}

		blockLen := binary.BigEndian.Uint32(lenCrcBuf[:4])
		if uint32(cap(buf)) < blockLen {
			buf = make([]byte, 0, blockLen)
		}

		crc := binary.BigEndian.Uint32(lenCrcBuf[4:8])
		s.sampleType = binary.BigEndian.Uint16(lenCrcBuf[8:10])
		s.sampleCount += binary.BigEndian.Uint16(lenCrcBuf[10:12])

		n, err = s.w.Read(buf[:blockLen])
		idx += n
		if err != nil {
			return err
		}

		if uint32(n) != blockLen {
			logger.Warnf("Repairing segment %s, short block, truncating at %d", s.path, lastGoodIdx)
			return s.truncate(int64(lastGoodIdx))
		}

		if crc32.ChecksumIEEE(buf[:blockLen]) != crc {
			logger.Warnf("Repairing segment %s, checksum failed, truncating at %d", s.path, lastGoodIdx)
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
		case req := <-s.appendCh:
			err := s.appendBlocks(req.Value)
			select {
			case req.ErrCh <- err:
			default:
			}

		case doneCh := <-s.flushCh:
			err := s.bw.Flush()
			if err != nil {
				logger.Errorf("Failed to flush writer for segment: %s: %s", s.path, err)
			}
			doneCh <- err
		case <-t.C:
			if err := s.bw.Flush(); err != nil {
				logger.Errorf("Failed to flush writer for segment: %s: %s", s.path, err)
			}
		case <-s.closing:
			blockBuf.Reset()
			s.flushQueue(blockBuf)

			req := &ring.Entry{ErrCh: make(chan error, 1)}
			s.flushBlock(blockBuf, req)

			if err := s.bw.Flush(); err != nil {
				logger.Errorf("Failed to flush writer for segment: %s: %s", s.path, err)
			}

			select {
			case err := <-req.ErrCh:
				if err != nil {
					logger.Errorf("Failed to flush block when closing segment: %s", err)
				}
			default:
			}

			return
		}
	}
}

func (s *segment) appendBlocks(val []byte) error {
	n, err := s.bw.Write(val)
	if err != nil {
		return err
	} else if n != len(val) {
		return io.ErrShortWrite
	}
	return nil
}

func (s *segment) flushBlock(blockBuf *bytes.Buffer, req *ring.Entry) {
	if blockBuf.Len() == 0 {
		req.ErrCh <- nil
		return
	}

	s.encodeBuf = s.encoder.EncodeAll(blockBuf.Bytes(), s.encodeBuf[:0])

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
	binary.BigEndian.PutUint16(s.lenBuf[8:10], s.sampleType)
	binary.BigEndian.PutUint16(s.lenBuf[10:12], s.sampleCount)
	n, err := w.Write(s.lenBuf[:12])
	if err != nil {
		return err
	} else if n != 12 {
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
	s.cw.SetWritten(ofs)

	return nil
}
