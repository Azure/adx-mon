package adx

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A segment that can't be opened for a reason other than being gone, such as running out of file descriptors or a
// disk error, fails the whole batch, which is released for a retry with every segment still on disk, and is uploaded
// in full once it can be read.
func TestUploadBatchRetriesSegmentThatFailedToOpen(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read a file without permission")
	}
	rec := newUploadRecorder(false)
	env := newSchedulingEnv(t, UploaderOpts{ConcurrentUploads: 2}, rec)
	batch, paths := env.queuedBatch(t, "Cpu", "1", "2")
	require.Len(t, batch.Segments, 2)
	require.NoError(t, os.Chmod(paths[1], 0))
	env.u.UploadQueue() <- batch
	require.Eventually(t, batch.IsReleased, 5*time.Second, time.Millisecond)
	require.False(t, batch.IsRemoved())
	for _, p := range paths {
		require.FileExists(t, p)
	}

	require.NoError(t, os.Chmod(paths[1], 0o644))
	env.waitUnbatched(t, paths)
	retry, _ := env.queuedBatch(t, "Cpu")
	require.ElementsMatch(t, paths, retry.Paths())
	env.u.UploadQueue() <- retry
	rec.waitStarted(t)
	require.Eventually(t, retry.IsRemoved, 5*time.Second, time.Millisecond)
	requireFilesRemoved(t, paths)
}

// A segment that isn't a WAL segment can never be read, so it is removed with the rest of the batch, as before.
func TestUploadBatchRemovesInvalidSegment(t *testing.T) {
	rec := newUploadRecorder(false)
	env := newSchedulingEnv(t, UploaderOpts{ConcurrentUploads: 2}, rec)
	batch, paths := env.queuedBatch(t, "Cpu", "1", "2")
	require.NoError(t, os.WriteFile(paths[1], []byte("not a segment"), 0o644))
	env.u.UploadQueue() <- batch
	rec.waitStarted(t)
	require.Eventually(t, batch.IsRemoved, 5*time.Second, time.Millisecond)
	requireFilesRemoved(t, paths)
}
