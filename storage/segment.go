package storage

import (
	"bufio"
	"errors"
	"fmt"
	"github.com/Azure/adx-mon/prompb"
	"github.com/Azure/adx-mon/transform"
	"github.com/davidnarayan/go-flake"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
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
	Write(ts prompb.TimeSeries) error
	Bytes() ([]byte, error)
	Close() error
	Table() string
	Epoch() string
	Size() (int64, error)
	CreatedAt() time.Time
	Reader() (io.Reader, error)
	Path() string
}

type file struct {
	epoch     string
	table     string
	createdAt time.Time
	path      string

	mu   sync.RWMutex
	w    *os.File
	size int64
	csv  *transform.CSVWriter
}

func NewSegment(dir, prefix string) (Segment, error) {
	epoch := idgen.NextId()

	fileName := fmt.Sprintf("%s_%s.csv", prefix, epoch.String())
	path := filepath.Join(dir, fileName)
	fw, err := os.Create(path)
	if err != nil {
		return nil, err
	}

	fields := strings.Split(prefix, "_")

	return &file{
		createdAt: time.Now().UTC(),
		table:     fields[0],
		epoch:     epoch.String(),
		path:      path,
		w:         fw,
		csv:       transform.NewCSVWriter(bufio.NewWriter(fw)),
	}, nil
}

func OpenSegment(path string) (Segment, error) {
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

	f := &file{
		epoch:     epoch,
		createdAt: createdAt,
		path:      path,
		table:     table,
		w:         fd,
		size:      sz,
		csv:       transform.NewCSVWriter(bufio.NewWriter(fd)),
	}

	return f, f.repair()
}

func (f *file) Path() string {
	return f.path
}

func (f *file) Reader() (io.Reader, error) {
	r, err := os.Open(f.path)
	if err != nil {
		return nil, fmt.Errorf("open reader: %s: %w", f.path, err)
	}
	return r, nil
}

func (f *file) CreatedAt() time.Time {
	return f.createdAt
}

func (f *file) Size() (int64, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	stat, err := os.Stat(f.path)
	if err != nil {
		return 0, err
	}
	return stat.Size(), nil
}

func (f *file) Epoch() string {
	return f.epoch
}

func (f *file) Table() string {
	return f.table
}

func (f *file) Write(ts prompb.TimeSeries) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	err := f.csv.MarshalCSV(ts)

	return err
}

func (f *file) Bytes() ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return os.ReadFile(f.w.Name())
}

func (f *file) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if err := f.w.Sync(); errors.Is(err, os.ErrClosed) {
		return nil
	} else if err != nil {
		return err
	}

	return f.w.Close()
}

// repair truncates removes the last bytes in the file that follow a newline.  This repairs any
// corrupted segments that may not have fully flushed to disk safely.
func (f *file) repair() error {
	buf := make([]byte, 4096)

	sz, err := f.w.Stat()
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

		n, err := f.w.ReadAt(buf, idx)
		if err != nil {
			return err
		}
		buf = buf[:n]

		var lastNewLine int
		for i := len(buf) - 1; i >= 0; i-- {
			if buf[i] == '\n' {
				lastNewLine = i
				if err := f.w.Truncate(idx + int64(lastNewLine+1)); err != nil {
					return err
				}

				if err := f.w.Sync(); err != nil {
					return err
				}

				_, err := f.w.Seek(0, io.SeekEnd)
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
