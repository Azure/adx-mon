package storage

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Azure/adx-mon/logger"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pool"
	"github.com/Azure/adx-mon/prompb"
	"github.com/Azure/adx-mon/ring"
	"github.com/Azure/adx-mon/transform"
	"github.com/davidnarayan/go-flake"
)

var (
	idgen *flake.Flake

	csvWriterPool = pool.NewGeneric(1024, func(sz int) interface{} {
		return transform.NewCSVWriter(bytes.NewBuffer(make([]byte, 0, sz)))
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
	Write(ctx context.Context, ts []prompb.TimeSeries) error
	Bytes() ([]byte, error)
	Close() error
	Table() string
	Epoch() string
	Size() (int64, error)
	CreatedAt() time.Time
	Reader() (io.Reader, error)
	Path() string
}

type writeReq struct {
	errCh chan error
	b     []byte
}

type segment struct {
	epoch     string
	table     string
	createdAt time.Time
	path      string
	hostname  string

	mu         sync.RWMutex
	w          *os.File
	bw         *bufio.Writer
	size       int64
	closing    chan struct{}
	closed     bool
	flushCount uint64
	wg         sync.WaitGroup

	ringBuf *ring.Buffer
}

func NewSegment(dir, prefix string) (Segment, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return nil, err
	}

	epoch := idgen.NextId()

	fileName := fmt.Sprintf("%s_%s.csv", prefix, epoch.String())
	path := filepath.Join(dir, fileName)
	fw, err := os.Create(path)
	if err != nil {
		return nil, err
	}

	fields := strings.Split(prefix, "_")

	bf := bufio.NewWriterSize(fw, 1024*1024)
	f := &segment{
		hostname:  hostname,
		createdAt: time.Now().UTC(),
		table:     fields[0],
		epoch:     epoch.String(),
		path:      path,
		w:         fw,
		bw:        bf,
		closing:   make(chan struct{}),
		ringBuf:   ring.NewBuffer(10000),
	}

	go f.flusher()
	return f, nil
}

func OpenSegment(path string) (Segment, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return nil, err
	}

	fileName := filepath.Base(path)
	fileName = strings.TrimSuffix(fileName, filepath.Ext(fileName))
	fields := strings.Split(fileName, "_")

	if len(fields) != 2 {
		return nil, fmt.Errorf("invalid segment filename: %s", path)
	}

	table := fields[0]
	epoch := fields[1]

	num, err := strconv.ParseInt(epoch, 16, 64)
	if err != nil {
		return nil, err
	}
	num = num >> (flake.HostBits + flake.SequenceBits)
	createdAt := flake.Epoch.Add(time.Duration(num) * time.Millisecond)

	fd, err := os.OpenFile(path, os.O_APPEND|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open segment: %s: %w", path, err)
	}

	sz, err := fd.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}

	bf := bufio.NewWriterSize(fd, 1024*1024)

	f := &segment{
		hostname:  hostname,
		epoch:     epoch,
		createdAt: createdAt,
		path:      path,
		table:     table,
		w:         fd,
		bw:        bf,
		size:      sz,
		closing:   make(chan struct{}),
		ringBuf:   ring.NewBuffer(10000),
	}

	if err := f.repair(); err != nil {
		return nil, err
	}

	go f.flusher()

	return f, nil
}

func (s *segment) Path() string {
	return s.path
}

func (s *segment) Reader() (io.Reader, error) {
	r, err := os.Open(s.path)
	if err != nil {
		return nil, fmt.Errorf("open reader: %s: %w", s.path, err)
	}
	return r, nil
}

func (s *segment) CreatedAt() time.Time {
	return s.createdAt
}

func (s *segment) Size() (int64, error) {
	stat, err := os.Stat(s.path)
	if err != nil {
		return 0, err
	}
	return stat.Size(), nil
}

func (s *segment) Epoch() string {
	return s.epoch
}

func (s *segment) Table() string {
	return s.table
}

func (s *segment) Write(ctx context.Context, ts []prompb.TimeSeries) error {
	enc := csvWriterPool.Get(8 * 1024).(*transform.CSVWriter)
	defer csvWriterPool.Put(enc)
	enc.Reset()

	for _, v := range ts {
		metrics.SamplesStored.WithLabelValues(s.hostname).Add(float64(len(v.Samples)))

		if err := enc.MarshalCSV(v); err != nil {
			return err
		}
	}

	entry := s.ringBuf.Reserve()
	defer s.ringBuf.Release(entry)

	entry.Value = enc.Bytes()
	s.ringBuf.Enqueue(entry)

	select {
	case err := <-entry.ErrCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

func (s *segment) Bytes() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.ReadFile(s.w.Name())
}

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

// repair truncates removes the last bytes in the segment that follow a newline.  This repairs any
// corrupted segment that may not have fully flushed to disk safely.
func (s *segment) repair() error {
	buf := make([]byte, 4096)

	sz, err := s.w.Stat()
	if err != nil {
		return err
	}

	idx := sz.Size() - int64(cap(buf))
	if idx < 0 {
		idx = 0
		buf = buf[:sz.Size()]
	}

	var done bool
	for {

		n, err := s.w.ReadAt(buf, idx)
		if err != nil {
			return err
		}
		buf = buf[:n]

		var lastNewLine int
		for i := len(buf) - 1; i >= 0; i-- {
			if buf[i] == '\n' {
				lastNewLine = i
				if err := s.w.Truncate(idx + int64(lastNewLine+1)); err != nil {
					return err
				}

				if err := s.w.Sync(); err != nil {
					return err
				}

				_, err := s.w.Seek(0, io.SeekEnd)
				return err
			}
		}

		if done {
			return nil
		}

		if idx < int64(cap(buf)) {
			buf = buf[:idx]
			idx = 0
			done = true
			continue
		}

		idx -= int64(len(buf))
		buf = buf[:cap(buf)]
	}
}

func (s *segment) flusher() {
	s.wg.Add(1)
	t := time.NewTicker(10 * time.Millisecond)
	defer t.Stop()

	defer s.wg.Done()

	for {
		select {
		case req := <-s.ringBuf.Queue():

			_, err := s.bw.Write(req.Value)
			select {
			case req.ErrCh <- err:
			default:
			}

			var n int
			for len(s.ringBuf.Queue()) > 0 {
				req = <-s.ringBuf.Queue()
				_, err := s.bw.Write(req.Value)
				select {
				case req.ErrCh <- err:
				default:
				}
				n++

				if n >= 1000 {
					break
				}
			}

			if err := s.bw.Flush(); err != nil {
				logger.Error("Failed to flush segment: %s", err)
			}
		case <-t.C:
			if err := s.bw.Flush(); err != nil {
				logger.Error("Failed to flush segment: %s", err)
			}

		case <-s.closing:
			for len(s.ringBuf.Queue()) > 0 {
				req := <-s.ringBuf.Queue()
				_, err := s.bw.Write(req.Value)
				req.Value = nil
				select {
				case req.ErrCh <- err:
				default:
				}
			}

			if err := s.bw.Flush(); err != nil {
				logger.Error("Failed to flush segment: %s", err)
			}
			return
		}
	}
}
