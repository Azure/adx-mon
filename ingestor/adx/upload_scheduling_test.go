package adx

import (
	"context"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/pkg/ingestpolicy"
	"github.com/Azure/adx-mon/pkg/wal"
	adxschema "github.com/Azure/adx-mon/schema"
	"github.com/stretchr/testify/require"
)

// uploadRecorder records each queued ingestion and can hold them until released.
type uploadRecorder struct {
	mu      sync.Mutex
	uploads []string
	started chan string
	gate    chan struct{}
	open    func()
	err     error
}

func newUploadRecorder(hold bool) *uploadRecorder {
	r := &uploadRecorder{started: make(chan string, 100), gate: make(chan struct{})}
	r.open = sync.OnceFunc(func() { close(r.gate) })
	if !hold {
		r.open()
	}
	return r
}

func (r *uploadRecorder) upload(ctx context.Context, reader io.Reader, _, table string, _ adxschema.SchemaMapping) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	r.started <- string(data)
	select {
	case <-r.gate:
	case <-ctx.Done():
		return ctx.Err()
	}
	r.mu.Lock()
	r.uploads = append(r.uploads, string(data))
	r.mu.Unlock()
	return r.err
}

func (r *uploadRecorder) recorded() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.uploads...)
}

// waitStarted returns the data of the next ingestion to start, failing after a few seconds.
func (r *uploadRecorder) waitStarted(t *testing.T) string {
	t.Helper()
	select {
	case data := <-r.started:
		return data
	case <-time.After(5 * time.Second):
		t.Fatal("no upload started")
		return ""
	}
}

// requireNoStart fails if an ingestion starts within a short wait.
func (r *uploadRecorder) requireNoStart(t *testing.T) {
	t.Helper()
	select {
	case data := <-r.started:
		t.Fatalf("unexpected upload of %q", data)
	case <-time.After(100 * time.Millisecond):
	}
}

// newSchedulingEnv returns a streaming test env whose uploader runs workers with a recorder for queued ingestion.
func newSchedulingEnv(t *testing.T, opts UploaderOpts, rec *uploadRecorder) *streamingTestEnv {
	t.Helper()
	env := newStreamingTestEnv(t)
	opts.Database = "db"
	opts.DefaultMapping = adxschema.DefaultMetricsMapping
	env.u = NewUploader(nil, opts)
	env.u.syncer = NewSyncer(env.mgmt, "db", adxschema.DefaultMetricsMapping, PromMetrics)
	env.u.syncer.cancelFn = func() {}
	env.u.queuedUpload = rec.upload
	ctx, cancel := context.WithCancel(context.Background())
	env.u.startWorkers(ctx)
	t.Cleanup(func() {
		// Release held uploads so Close does not wait for them when a test fails early.
		rec.open()
		cancel()
		require.NoError(t, env.u.Close())
	})
	return env
}

// requireFilesRemoved waits for paths to be deleted.  A batch is marked removed just before its files are deleted.
func requireFilesRemoved(t *testing.T, paths []string) {
	t.Helper()
	for _, p := range paths {
		require.Eventually(t, func() bool {
			_, err := os.Stat(p)
			return os.IsNotExist(err)
		}, 5*time.Second, time.Millisecond, "%s not removed", p)
	}
}

func csvRow(v string) []byte { return []byte("2024-01-01T00:00:00Z,1,{}," + v + "\n") }

// queuedBatch adds one segment per value to table and returns the batch the batcher builds from them.
func (e *streamingTestEnv) queuedBatch(t *testing.T, table string, values ...string) (*cluster.Batch, []string) {
	t.Helper()
	var paths []string
	for _, v := range values {
		paths = append(paths, e.addSegment(t, table, ingestpolicy.PriorityQueued, csvRow(v), time.Time{}))
	}
	return e.nextBatch(t, ingestpolicy.PriorityQueued), paths
}

// A batch of several segments is read as one ingestion of every segment, and its segments are removed after it.
func TestUploaderIngestsBatchAsOneUpload(t *testing.T) {
	rec := newUploadRecorder(false)
	env := newSchedulingEnv(t, UploaderOpts{ConcurrentUploads: 2}, rec)

	batch, paths := env.queuedBatch(t, "Cpu", "1", "2")
	require.Len(t, batch.Segments, 2)
	env.u.UploadQueue() <- batch

	require.Equal(t, string(csvRow("1"))+string(csvRow("2")), rec.waitStarted(t))
	require.Eventually(t, batch.IsRemoved, 5*time.Second, time.Millisecond)
	requireFilesRemoved(t, paths)
}

// A failed ingestion releases the batch and keeps every segment for retry.
func TestUploaderReleasesFailedBatch(t *testing.T) {
	rec := newUploadRecorder(false)
	rec.err = io.ErrUnexpectedEOF
	env := newSchedulingEnv(t, UploaderOpts{ConcurrentUploads: 2}, rec)

	batch, paths := env.queuedBatch(t, "Cpu", "1", "2")
	env.u.UploadQueue() <- batch
	rec.waitStarted(t)

	require.Eventually(t, batch.IsReleased, 5*time.Second, time.Millisecond)
	require.False(t, batch.IsRemoved())
	for _, p := range paths {
		require.FileExists(t, p)
	}
}

// One table's queued batches run on every upload worker except the reserved one.
func TestUploaderRunsTableOnUnreservedWorkers(t *testing.T) {
	rec := newUploadRecorder(true)
	env := newSchedulingEnv(t, UploaderOpts{ConcurrentUploads: 4}, rec)

	for _, v := range []string{"1", "2", "3"} {
		b, _ := env.queuedBatch(t, "Cpu", v)
		env.u.UploadQueue() <- b
		rec.waitStarted(t)
	}
	b, _ := env.queuedBatch(t, "Cpu", "4")
	env.u.UploadQueue() <- b
	rec.requireNoStart(t)

	rec.open()
	rec.waitStarted(t)
	require.Eventually(t, func() bool { return len(rec.recorded()) == 4 }, 5*time.Second, time.Millisecond)
}

// Queued batches of a table waiting behind its running upload are merged into one ingestion, and every segment is
// removed after it.
func TestUploaderMergesWaitingBatches(t *testing.T) {
	rec := newUploadRecorder(true)
	env := newSchedulingEnv(t, UploaderOpts{ConcurrentUploads: 2}, rec)

	first, _ := env.queuedBatch(t, "Cpu", "1")
	env.u.UploadQueue() <- first
	rec.waitStarted(t)

	second, p2 := env.queuedBatch(t, "Cpu", "2")
	third, p3 := env.queuedBatch(t, "Cpu", "3")
	env.u.UploadQueue() <- second
	env.u.UploadQueue() <- third
	rec.requireNoStart(t)

	rec.open()
	require.Eventually(t, second.IsRemoved, 5*time.Second, time.Millisecond)
	require.Equal(t, []string{string(csvRow("1")), string(csvRow("2")) + string(csvRow("3"))}, rec.recorded())
	requireFilesRemoved(t, append(p2, p3...))
}

// With merging disabled, waiting batches are uploaded one at a time, in order.
func TestUploaderUploadsWaitingBatchesSeparatelyWithoutMerging(t *testing.T) {
	rec := newUploadRecorder(true)
	env := newSchedulingEnv(t, UploaderOpts{ConcurrentUploads: 2, MaxMergeSegments: -1}, rec)

	first, _ := env.queuedBatch(t, "Cpu", "1")
	env.u.UploadQueue() <- first
	rec.waitStarted(t)

	second, _ := env.queuedBatch(t, "Cpu", "2")
	third, _ := env.queuedBatch(t, "Cpu", "3")
	env.u.UploadQueue() <- second
	env.u.UploadQueue() <- third
	rec.requireNoStart(t)

	rec.open()
	require.Eventually(t, third.IsRemoved, 5*time.Second, time.Millisecond)
	require.Equal(t, []string{string(csvRow("1")), string(csvRow("2")), string(csvRow("3"))}, rec.recorded())
	require.True(t, second.IsRemoved())
}

// Merges are bounded by the uncompressed size of segments, or ten times their size on disk when it is not known.
func TestUploadSize(t *testing.T) {
	require.Equal(t, int64(500), uploadSize(wal.SegmentInfo{Size: 100, RawSize: 500}))
	require.Equal(t, int64(1000), uploadSize(wal.SegmentInfo{Size: 100}))
}

// The uncompressed size a segment reports is the size of the data ingested from it.
func TestUploaderRawSizeIsIngestedSize(t *testing.T) {
	rec := newUploadRecorder(false)
	env := newSchedulingEnv(t, UploaderOpts{ConcurrentUploads: 2}, rec)

	seg, err := wal.NewSegment(env.dir, "db_Cpu")
	require.NoError(t, err)
	for _, v := range []string{"1", "2", "3"} {
		_, err = seg.Write(context.Background(), csvRow(v))
		require.NoError(t, err)
	}
	info := seg.Info()
	require.NoError(t, seg.Close())
	info.Priority = ingestpolicy.PriorityQueued
	env.idx.Add(info)
	env.u.UploadQueue() <- env.nextBatch(t, ingestpolicy.PriorityQueued)

	require.Equal(t, info.RawSize, int64(len(rec.waitStarted(t))))
	require.Less(t, info.RawSize, uploadSize(wal.SegmentInfo{Size: info.Size}))
}

// A merged batch stays within MaxMergeSize, in uncompressed bytes, and MaxMergeSegments.
func TestUploaderMergeLimits(t *testing.T) {
	row := int64(len(csvRow("1")))
	for name, opts := range map[string]UploaderOpts{
		"size":     {MaxMergeSize: 2 * row},
		"segments": {MaxMergeSegments: 2},
	} {
		t.Run(name, func(t *testing.T) {
			rec := newUploadRecorder(true)
			opts.ConcurrentUploads = 2
			env := newSchedulingEnv(t, opts, rec)

			first, _ := env.queuedBatch(t, "Cpu", "1")
			env.u.UploadQueue() <- first
			rec.waitStarted(t)
			var last *cluster.Batch
			for _, v := range []string{"2", "3", "4"} {
				last, _ = env.queuedBatch(t, "Cpu", v)
				env.u.UploadQueue() <- last
			}
			rec.requireNoStart(t)

			rec.open()
			require.Eventually(t, last.IsRemoved, 5*time.Second, time.Millisecond)
			require.Equal(t, []string{
				string(csvRow("1")), string(csvRow("2")) + string(csvRow("3")), string(csvRow("4")),
			}, rec.recorded())
		})
	}
}

// A failed merged ingestion releases every segment of the merged batches for retry.
func TestUploaderReleasesFailedMergedBatch(t *testing.T) {
	rec := newUploadRecorder(true)
	rec.err = io.ErrUnexpectedEOF
	env := newSchedulingEnv(t, UploaderOpts{ConcurrentUploads: 2}, rec)

	first, p1 := env.queuedBatch(t, "Cpu", "1")
	env.u.UploadQueue() <- first
	rec.waitStarted(t)
	second, p2 := env.queuedBatch(t, "Cpu", "2")
	third, p3 := env.queuedBatch(t, "Cpu", "3")
	env.u.UploadQueue() <- second
	env.u.UploadQueue() <- third
	rec.requireNoStart(t)

	rec.open()
	require.Eventually(t, second.IsReleased, 5*time.Second, time.Millisecond)
	require.False(t, second.IsRemoved())
	paths := append(append(p1, p2...), p3...)
	for _, p := range paths {
		require.FileExists(t, p)
	}
	// Every segment can be batched again.
	require.ElementsMatch(t, paths, env.nextBatch(t, ingestpolicy.PriorityQueued).Paths())
}

// MaxUploadsPerTable limits a table's concurrent uploads, and its waiting batches merge.
func TestUploaderLimitsUploadsPerTable(t *testing.T) {
	rec := newUploadRecorder(true)
	env := newSchedulingEnv(t, UploaderOpts{ConcurrentUploads: 4, MaxUploadsPerTable: 1}, rec)

	first, _ := env.queuedBatch(t, "Cpu", "1")
	env.u.UploadQueue() <- first
	rec.waitStarted(t)
	second, _ := env.queuedBatch(t, "Cpu", "2")
	third, _ := env.queuedBatch(t, "Cpu", "3")
	env.u.UploadQueue() <- second
	env.u.UploadQueue() <- third
	rec.requireNoStart(t)

	// Another table is not held back.
	other, _ := env.queuedBatch(t, "Mem", "9")
	env.u.UploadQueue() <- other
	require.Equal(t, string(csvRow("9")), rec.waitStarted(t))

	rec.open()
	require.Eventually(t, second.IsRemoved, 5*time.Second, time.Millisecond)
	require.ElementsMatch(t, []string{
		string(csvRow("1")), string(csvRow("9")), string(csvRow("2")) + string(csvRow("3")),
	}, rec.recorded())
}

func TestUploaderOptsQueueOpts(t *testing.T) {
	opts := UploaderOpts{Database: "db", ConcurrentUploads: 10}.queueOpts()
	require.NotNil(t, opts.MergeSize)
	opts.MergeSize = nil
	require.Equal(t, cluster.FairQueueOpts{
		Workers: 10, Pipeline: "adx:db", MergeSegments: DefaultMaxUploadMergeSegments, MergeBytes: DefaultMaxUploadMergeSize,
	}, opts)

	opts = UploaderOpts{Database: "db", ConcurrentUploads: 10, MaxUploadsPerTable: 2, MaxMergeSegments: 5, MaxMergeSize: 7}.queueOpts()
	require.Equal(t, 2, opts.MaxRunningPerTable)
	require.Equal(t, 5, opts.MergeSegments)
	require.Equal(t, int64(7), opts.MergeBytes)

	require.Zero(t, UploaderOpts{MaxMergeSegments: -1}.queueOpts().MergeSegments)
}
