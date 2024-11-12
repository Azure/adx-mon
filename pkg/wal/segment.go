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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	flakeutil "github.com/Azure/adx-mon/pkg/flake"
	"github.com/Azure/adx-mon/pkg/logger"
	adxsync "github.com/Azure/adx-mon/pkg/sync"
	"github.com/klauspost/compress/s2"
	gbp "github.com/libp2p/go-buffer-pool"
)

var (
	// blockHdrMagic is the magic number for the block header.  If this is not present as the first 2 bytes of a block,
	// then extra metadata is not present.
	blockHdrMagic = [2]byte{0xAA, 0xAA} // 10101010 10101010 in binary

	// segmentMagic is the magic number for the segment file.  If this is not present as the first 6 bytes of a segment
	// then the file is not a valid segment file.
	segmentMagic   = [8]byte{'A', 'D', 'X', 'W', 'A', 'L'}
	segmentVersion = byte(1)
)

type SegmentOption func(s *segment)

func WithFlushIntervale(d time.Duration) SegmentOption {
	return func(s *segment) {
		if d.Seconds() > 0 {
			s.flushInterval = d
		}
	}
}

type Segment interface {
	Append(ctx context.Context, buf []byte) error
	Write(ctx context.Context, buf []byte, opts ...WriteOptions) error
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
	// Repair truncates the last bytes in the segment if they are missing, corrupted or have extra data.  This
	// repairs any corrupted segment that may not have fully flushed to disk safely.  The segment is truncated
	// from the first block that is found to be corrupt.
	Repair() error
}

type Iterator interface {
	Next() (bool, error)
	Value() []byte
	Close() error
	// Verify ensures the Iterator can iterate over a continuous series of blocks without error.
	Verify() (int, error)
	Metadata() (SampleType, uint32)
}

type segment struct {
	// id is the time-ordered ID and allows for segment files to be sorted lexicographically and in time order of
	// creating.
	id        string
	createdAt time.Time
	path      string
	prefix    string
	filePos   uint64

	wg sync.WaitGroup
	mu *adxsync.CountingRWMutex

	// w is the underlying segment file on disk
	w *os.File

	// bw is a buffered writer for w that if flushed to disk in batches.
	bw *bufio.Writer

	closing chan struct{}
	closed  bool

	// sample metadata
	sampleType  uint16
	sampleCount uint16

	flushCh       chan chan error
	flushInterval time.Duration
}

func NewSegment(dir, prefix string, opts ...SegmentOption) (Segment, error) {
	flakeId := idgen.NextId()

	createdAt, err := flakeutil.ParseFlakeID(flakeId.String())
	if err != nil {
		return nil, err
	}

	fileName := fmt.Sprintf("%s_%s.wal", prefix, flakeId.String())
	if !fs.ValidPath(fileName) {
		return nil, fmt.Errorf("invalid segment filename: %s", fileName)
	}
	// Ensure the filename is just the base name, with no path separators within it
	baseName := filepath.Base(fileName)
	if baseName != fileName {
		return nil, fmt.Errorf("invalid segment filename: %s", fileName)
	}

	path := filepath.Join(dir, fileName)
	fw, err := os.Create(path)
	if err != nil {
		return nil, err
	}

	if n, err := fw.Write(segmentMagic[:]); err != nil {
		return nil, err
	} else if n != 8 {
		return nil, io.ErrShortWrite
	}

	bf := bwPool.Get(DefaultIOBufSize).(*bufio.Writer)
	bf.Reset(fw)

	f := &segment{
		id:        flakeId.String(),
		prefix:    prefix,
		createdAt: createdAt.UTC(),
		path:      path,
		w:         fw,
		bw:        bf,
		filePos:   8,

		closing:       make(chan struct{}),
		flushCh:       make(chan chan error),
		flushInterval: 100 * time.Millisecond,
		mu:            adxsync.NewCountingRWMutex(5),
	}

	for _, opt := range opts {
		opt(f)
	}

	f.wg.Add(1)
	go f.flusher()
	return f, nil
}

// IsSegment returns true if the file is a valid segment file.
func IsSegment(path string) bool {
	ext := filepath.Ext(path)
	if ext != ".wal" {
		return false
	}

	ff, err := os.Open(path)
	if err != nil {
		return false
	}
	defer ff.Close()

	// First 8 bytes are the header and 6 bytes is the segmentMagic number.  The remaining two bytes are not used
	// yet but can indicate version or other information in the future.
	var magicBuf [8]byte
	if n, err := ff.Read(magicBuf[:]); err != nil && !errors.Is(err, io.EOF) {
		return false
	} else if n != 8 || !bytes.Equal(magicBuf[:6], segmentMagic[:6]) {
		return false
	}

	return true
}

func Open(path string) (Segment, error) {
	if !IsSegment(path) {
		return nil, fmt.Errorf("invalid segment file: %s", path)
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

	bf := bwPool.Get(DefaultIOBufSize).(*bufio.Writer)
	bf.Reset(fd)

	f := &segment{
		id:        id,
		prefix:    prefix,
		createdAt: createdAt,
		path:      path,
		filePos:   uint64(stat.Size()),

		w:             fd,
		bw:            bf,
		closing:       make(chan struct{}),
		flushCh:       make(chan chan error),
		flushInterval: 100 * time.Millisecond,
		mu:            adxsync.NewCountingRWMutex(5),
	}

	if err := f.Repair(); err != nil {
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
	return int64(atomic.LoadUint64(&s.filePos)), nil
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

	if s.mu.TryLock() {
		defer s.mu.Unlock()
	} else {
		return ErrSegmentLocked
	}

	if s.closed {
		return ErrSegmentClosed
	}

	// Strip off the header and append the block to the segment
	n, err := s.appendBlocks(buf[8:])
	if err != nil {
		return err
	}
	atomic.AddUint64(&s.filePos, uint64(n))
	return nil
}

// Write writes buf to the segment.
func (s *segment) Write(ctx context.Context, buf []byte, opts ...WriteOptions) error {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return ErrSegmentClosed
	}
	s.mu.RUnlock()

	written, err := s.blockWrite(s.bw, buf, opts...)
	if err != nil {
		return err
	}

	atomic.AddUint64(&s.filePos, uint64(written))
	return err
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

	if err := s.bw.Flush(); err != nil {
		return err
	}

	bwPool.Put(s.bw)
	s.bw = nil

	if err := s.w.Sync(); errors.Is(err, os.ErrClosed) {
		return nil
	} else if err != nil {
		return err
	}

	return s.w.Close()
}

// Repair truncates the last bytes in the segment if they are missing, corrupted or have extra data.  This
// repairs any corrupted segment that may not have fully flushed to disk safely.
func (s *segment) Repair() error {
	buf := make([]byte, 0, 4096)

	if _, err := s.w.Seek(8, io.SeekStart); err != nil {
		return err
	}

	var (
		lastGoodIdx, idx = 8, 8
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
			logger.Warnf("Repairing segment %s, missing block header, truncating at %d", s.path, lastGoodIdx)
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
			logger.Warnf("Repairing segment %s, unexected error %s, truncating at %d", s.path, err, lastGoodIdx)
			return s.truncate(int64(lastGoodIdx))
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

	t := time.NewTicker(s.flushInterval)
	defer t.Stop()

	for {
		select {
		case doneCh := <-s.flushCh:
			var err error
			if s.mu.TryLock() {
				err = s.bw.Flush()
				s.mu.Unlock()
			} else {
				err = fmt.Errorf("segment locked")
			}
			if err != nil {
				logger.Errorf("Failed to flush writer for segment: %s: %s", s.path, err)
			}
			doneCh <- err
		case <-s.closing:
			return
		case <-t.C:
			if s.mu.TryLock() {
				if err := s.bw.Flush(); err != nil {
					logger.Errorf("Failed to flush writer for segment: %s: %s", s.path, err)
				}
				s.mu.Unlock()
			}
		}
	}
}

func (s *segment) appendBlocks(val []byte) (int, error) {
	n, err := s.bw.Write(val)
	if err != nil {
		return 0, err
	} else if n != len(val) {
		return n, io.ErrShortWrite
	}
	return n, nil
}

// blockWrite writes length and CRC32 prefixed block to w
func (s *segment) blockWrite(w io.Writer, buf []byte, opts ...WriteOptions) (int, error) {
	if len(buf) == 0 {
		return 0, nil
	}

	// Each block is constructed as follows:
	//
	// The block is prefixed with a 4 byte length and 4 byte CRC32 checksum which is used
	// to verify the block's integrity.
	//
	// The block body is a snappy encoded byte array consisting of a 2 byte magic number,
	// 1 byte version, 1 byte type, 4 byte count, and N bytes of value. The magic number is used to identify the
	// block as a valid block.  The version is used to identify the version of the block.  The type is used to
	// identify the type of the block.  The count is used to identify the number of
	// samples in the block.  The value is the actual data of the block uncompressed.
	//

	// ┌───────────┬─────────┬───────────┬───────────┬───────────┬───────────┬───────────┐
	// │    Len    │   CRC   │   Magic   │  Version  │   Type    │   Count   │   Value   │
	// │  4 bytes  │ 4 bytes │  2 bytes  │  1 byte   │  1 byte   │  4 bytes  │  N bytes  │
	// └───────────┴─────────┴───────────┴───────────┴───────────┴───────────┴───────────┘
	// ┌─────────────────────┬───────────────────────────────────────────────────────────┐
	// │       Header        │                           Block                           │
	// └─────────────────────┴───────────────────────────────────────────────────────────┘

	// The block header is 8 bytes long and consists of the length and CRC32 checksum of the block.
	// The other 8 bytes are for the magic number, version, type, and count of the block.
	// We use one slice from the buffer pool that is large enough to store the metadata headers
	// and the compressed value.

	blockBuf := gbp.Get(8 + len(buf))
	defer gbp.Put(blockBuf)

	// Copy the magic number and version to the buffer
	copy(blockBuf[0:2], blockHdrMagic[:])
	blockBuf[2] = segmentVersion

	// Default to unknown, no count data for the block
	blockBuf[3] = byte(UnknownSampleType)
	binary.BigEndian.PutUint32(blockBuf[4:8], 0)

	// Apply the WriteOptions to set sample type and count
	for _, opt := range opts {
		opt(blockBuf[3:8])
	}

	// Append the block value to the buffer
	copy(blockBuf[8:], buf)

	// We need a separate buffer build the block header and value
	b := gbp.Get(8 + s2.MaxEncodedLen(len(blockBuf)))
	defer gbp.Put(b)

	// Encode the block header and value
	compressedBytes := s2.EncodeBetter(b[8:], blockBuf)

	binary.BigEndian.PutUint32(b[0:4], uint32(len(compressedBytes)))
	binary.BigEndian.PutUint32(b[4:8], crc32.ChecksumIEEE(compressedBytes))

	b = b[:8+len(compressedBytes)]

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return 0, ErrSegmentClosed
	}

	n, err := w.Write(b)
	if err != nil {
		return 0, err
	} else if n != len(b) {
		return 0, io.ErrShortWrite
	}

	return n, nil
}

func (s *segment) truncate(ofs int64) error {
	if err := s.w.Truncate(ofs); err != nil {
		return err
	}
	if err := s.w.Sync(); err != nil {
		return err
	}
	atomic.StoreUint64(&s.filePos, uint64(ofs))

	return nil
}

func WithSampleMetadata(t SampleType, count uint32) WriteOptions {
	return func(b []byte) {
		if len(b) < 5 {
			return
		}
		b[0] = byte(t)
		binary.BigEndian.PutUint32(b[1:5], count)
	}
}

func SampleMetadata(b []byte) (t SampleType, count uint32) {
	if len(b) < 5 {
		return
	}
	t = SampleType(b[0])
	count = binary.BigEndian.Uint32(b[1:5])

	return
}

func HasSampleMetadata(b []byte) bool {
	return bytes.Equal(b[0:2], blockHdrMagic[:])
}
