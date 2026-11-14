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

// Close stops the compression goroutine by closing the pipe reader.  The writer
// goroutine's io.Copy then returns ErrClosedPipe, its deferred closes run, and
// the source file is released.  Callers that stop reading (e.g. on error) must
// call Close to avoid leaking the goroutine and the open file.
func (r *CompressReader) Close() error {
	return r.pr.Close()
}
