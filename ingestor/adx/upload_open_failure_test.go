package adx

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A segment that can't be opened for a reason other than being gone, such as running out of file descriptors or a
// disk error, is skipped; the rest of the batch is uploaded and the whole batch, including the skipped segment, is
// removed.
func TestUploadBatchRemovesSegmentThatFailedToOpen(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read a file without permission")
	}
	rec := newUploadRecorder(false)
	env := newSchedulingEnv(t, UploaderOpts{ConcurrentUploads: 2}, rec)
	batch, paths := env.queuedBatch(t, "Cpu", "1", "2")
	require.Len(t, batch.Segments, 2)
	require.NoError(t, os.Chmod(paths[1], 0))
	env.u.UploadQueue() <- batch
	rec.waitStarted(t)
	require.Eventually(t, batch.IsRemoved, 5*time.Second, time.Millisecond)
	requireFilesRemoved(t, paths)
}
