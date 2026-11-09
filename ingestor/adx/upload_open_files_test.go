package adx

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	adxschema "github.com/Azure/adx-mon/schema"
	"github.com/stretchr/testify/require"
)

// openFilesIn returns how many of the process's open files are in dir.
func openFilesIn(t *testing.T, dir string) int {
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Skip("needs /proc/self/fd")
	}
	n := 0
	for _, e := range entries {
		if target, err := os.Readlink(filepath.Join("/proc/self/fd", e.Name())); err == nil && strings.HasPrefix(target, dir+"/") {
			n++
		}
	}
	return n
}

// segmentFilesOpenWhileUploading uploads a batch of n segments and returns the most segment files open at once while
// the upload reads it.
func segmentFilesOpenWhileUploading(t *testing.T, n int) int {
	rec := newUploadRecorder(false)
	env := newSchedulingEnv(t, UploaderOpts{ConcurrentUploads: 1}, rec)
	var values []string
	for i := 0; i < n; i++ {
		values = append(values, string(rune('a'+i)))
	}
	batch, paths := env.queuedBatch(t, "Cpu", values...)
	require.Len(t, batch.Segments, n)
	dir := filepath.Dir(paths[0])

	maxOpen := make(chan int, 1)
	env.u.queuedUpload = func(ctx context.Context, reader io.Reader, _, _ string, _ adxschema.SchemaMapping) error {
		most := 0
		buf := make([]byte, 1)
		for {
			most = max(most, openFilesIn(t, dir))
			if _, err := reader.Read(buf); err == io.EOF {
				break
			} else if err != nil {
				return err
			}
		}
		maxOpen <- most
		return nil
	}
	env.u.UploadQueue() <- batch
	select {
	case m := <-maxOpen:
		return m
	case <-time.After(5 * time.Second):
		t.Fatal("no upload")
		return 0
	}
}

// An upload opens every segment of its batch before it reads the first, so a batch of 500 segments holds 500 files
// open at once.
func TestUploadOpensEverySegmentAtOnce(t *testing.T) {
	require.Equal(t, 5, segmentFilesOpenWhileUploading(t, 5))
}
