package data

import (
	"bufio"
	encoding_binary "encoding/binary"
	"fmt"
	"github.com/Azure/adx-mon/prompb"
	"os"
)

type FileWriter struct {
	f  *os.File
	bw *bufio.Writer
}

func NewFileWriter(path string) (*FileWriter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	bw := bufio.NewWriter(f)
	return &FileWriter{f: f, bw: bw}, nil
}

func (f *FileWriter) Write(wr *prompb.WriteRequest) (int, error) {
	b, err := wr.Marshal()
	if err != nil {
		return 0, err
	}

	// Write the length
	if err := encoding_binary.Write(f.f, encoding_binary.BigEndian, uint64(len(b))); err != nil {
		return 0, err
	}

	n, err := f.bw.Write(b)
	return n + 8, err
}

func (f *FileWriter) Close() error {
	if err := f.bw.Flush(); err != nil {
		return err
	}

	if err := f.f.Sync(); err != nil {
		return err
	}
	return f.f.Close()
}

type FileReader struct {
	f *os.File
}

func NewFileReader(path string) (*FileReader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return &FileReader{f: f}, nil
}

func (f *FileReader) Read() (*prompb.WriteRequest, error) {
	var sz uint64
	// Write the length
	if err := encoding_binary.Read(f.f, encoding_binary.BigEndian, &sz); err != nil {
		return nil, err
	}

	b := make([]byte, sz)

	// Write the length
	if n, err := f.f.Read(b[:sz]); err != nil {
		return nil, err
	} else if n != int(sz) {
		return nil, fmt.Errorf("short read: %d != %d", n, sz)
	}

	wr := &prompb.WriteRequest{}
	if err := wr.Unmarshal(b[:sz]); err != nil {
		return nil, err
	}
	return wr, nil
}

func (f *FileReader) Close() error {
	return f.f.Close()
}
