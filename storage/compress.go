package storage

import (
	"bufio"
	"bytes"
	"context"
	"github.com/Azure/adx-mon/limiter"
	"github.com/Azure/adx-mon/pkg/service"
	"github.com/Azure/adx-mon/pool"
	gzip "github.com/klauspost/pgzip"
	//gzip "compress/gzip"
	"io"

	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	bufioWriterPool = pool.NewGeneric(32, func(sz int) interface{} {
		return bufio.NewWriterSize(io.Discard, 4*1024)
	})

	gzWriterPool = pool.NewGeneric(32, func(sz int) interface{} {
		return gzip.NewWriter(io.Discard)
	})

	bufioReaderPool = pool.NewGeneric(32, func(sz int) interface{} {
		return bufio.NewReaderSize(bytes.NewReader(nil), 4*1024)
	})

	limit = limiter.NewFixed(1)
)

type Compressor interface {
	service.Component
}

type compressor struct{}

func NewCompressor() Compressor {
	return &compressor{}
}

func (c *compressor) Open(ctx context.Context) error {
	return nil
}

func (c *compressor) Compress(seg Segment) (string, error) {
	path := seg.Path()
	fileName := filepath.Base(path)

	fileName = strings.TrimSuffix(fileName, filepath.Ext(path))
	fileName = fmt.Sprintf("%s.csv.gz.tmp", fileName)

	path = filepath.Join(filepath.Dir(path), fileName)

	fd, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return "", fmt.Errorf("compress segment: %s: %w", path, err)
	}

	bw := bufioWriterPool.Get(4 * 1024).(*bufio.Writer)
	defer bufioWriterPool.Put(bw)
	bw.Reset(fd)

	gw := gzWriterPool.Get(0).(*gzip.Writer)
	gw.SetConcurrency(256<<10, 2)
	defer gzWriterPool.Put(gw)
	gw.Reset(bw)

	r, err := seg.Reader()
	if err != nil {
		return "", err
	}

	br := bufioReaderPool.Get(4 * 1024).(*bufio.Reader)
	defer bufioReaderPool.Put(br)
	br.Reset(r)

	_, err = io.Copy(gw, br)
	if err != nil {
		return "", err
	}

	if err := gw.Close(); err != nil {
		return "", err
	}

	if err := bw.Flush(); err != nil {
		return "", err
	}

	if err := fd.Sync(); err != nil {
		return "", err
	}

	if err := fd.Close(); err != nil {
		return "", err
	}

	destPath := strings.TrimSuffix(path, ".tmp")
	if err := os.Rename(path, destPath); err != nil {
		return "", fmt.Errorf("rename tmp archive: %w", err)
	}

	return destPath, nil
}

func (c *compressor) Close() error {
	return nil
}
