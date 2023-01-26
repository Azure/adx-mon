package gzip

import (
	gzip "github.com/klauspost/pgzip"
	"io"
	"os"
)

type CompressReader struct {
	f  *os.File
	pr *io.PipeReader
	gw *gzip.Writer
}

func NewCompressReader(f *os.File) *CompressReader {
	pr, pw := io.Pipe()
	gw := gzip.NewWriter(pw)
	c := &CompressReader{f: f, gw: gw, pr: pr}
	go func() {
		defer f.Close()
		defer pw.Close()
		defer gw.Close()
		io.Copy(gw, f)
	}()
	return c
}

func (r *CompressReader) Read(p []byte) (n int, err error) {
	return r.pr.Read(p)
}
