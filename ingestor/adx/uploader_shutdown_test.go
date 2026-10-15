package adx

import (
	"context"
	"io"
	"net/http"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/pkg/ingestpolicy"
	adxschema "github.com/Azure/adx-mon/schema"
	"github.com/Azure/azure-kusto-go/azkustodata"
	kustov1 "github.com/Azure/azure-kusto-go/azkustodata/query/v1"
	"github.com/Azure/azure-kusto-go/azkustoingest"
	"github.com/stretchr/testify/require"
)

func TestUploader_WorkersRegisteredBeforeLaunch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		u := NewUploader(nil, UploaderOpts{ConcurrentUploads: 4})
		executionCtx, cancelExecution := context.WithCancel(context.Background())
		defer cancelExecution()
		// Only start upload workers; the syncer's reconciliation loop is not needed here.
		u.syncer.cancelFn = func() {}
		u.startWorkers(executionCtx)

		waited := false
		go func() {
			u.wg.Wait()
			waited = true
		}()
		synctest.Wait()
		require.False(t, waited, "idle workers must be tracked even before they start an upload")

		require.NoError(t, u.Close())
		synctest.Wait()
		require.True(t, waited)
		require.NoError(t, executionCtx.Err())
	})
}

type shutdownStreamer struct {
	upload func(context.Context) error
	closed bool
}

func (s *shutdownStreamer) FromReader(ctx context.Context, _ io.Reader, _ ...azkustoingest.FileOption) (*azkustoingest.Result, error) {
	return nil, s.upload(ctx)
}

func (s *shutdownStreamer) Close() error {
	s.closed = true
	return nil
}

// The worker channels and cancellation signals must be created inside the synctest bubble.
// WAL segments are created outside it because the segment ID generator uses wall-clock time.
func prepareShutdownUploader(u *uploader) {
	u.syncer.cancelFn = func() {}
	u.opts.ConcurrentUploads = 1
	u.queue = make(chan *cluster.Batch, 10)
	u.realtimeQueue = make(chan *cluster.Batch, 10)
}

type shutdownContextKey struct{}

func releaseUpload(finish chan struct{}) {
	select {
	case <-finish:
	default:
		close(finish)
	}
}

func TestUploader_ShutdownAllowsInflightUploadToComplete(t *testing.T) {
	for _, priority := range []ingestpolicy.Priority{ingestpolicy.PriorityQueued, ingestpolicy.PriorityRealtime} {
		for _, cancelParentFirst := range []bool{false, true} {
			mode := "close"
			if cancelParentFirst {
				mode = "parent cancellation before close"
			}
			t.Run(priority.String()+"/"+mode, func(t *testing.T) {
				env := newStreamingTestEnv(t)
				path := env.addSegment(t, "Cpu", priority, testCSV, time.Time{})
				batch := env.nextBatch(t, priority)
				env.addSegment(t, "PendingQueued", ingestpolicy.PriorityQueued, testCSV, time.Time{})
				pendingQueued := env.nextBatch(t, ingestpolicy.PriorityQueued)
				env.addSegment(t, "PendingRealtime", ingestpolicy.PriorityRealtime, testCSV, time.Time{})
				pendingRealtime := env.nextBatch(t, ingestpolicy.PriorityRealtime)
				env.addSegment(t, "AfterClose", ingestpolicy.PriorityQueued, testCSV, time.Time{})
				afterClose := env.nextBatch(t, ingestpolicy.PriorityQueued)
				synctest.Test(t, func(t *testing.T) {
					u := env.u
					parentCtx, cancelParent := context.WithTimeout(context.WithValue(context.Background(), shutdownContextKey{}, "ingestor"), time.Second)
					defer cancelParent()
					prepareShutdownUploader(u)

					started := make(chan context.Context, 10)
					finish := make(chan struct{})
					defer releaseUpload(finish)
					calls := 0
					block := func(ctx context.Context) error {
						calls++
						started <- ctx
						select {
						case <-finish:
							return nil
						case <-ctx.Done():
							return ctx.Err()
						}
					}
					streamer := &shutdownStreamer{upload: block}
					u.streamer = streamer
					u.queuedUpload = func(ctx context.Context, _ io.Reader, _, _ string, _ adxschema.SchemaMapping) error {
						return block(ctx)
					}

					queue := u.UploadQueue()
					if priority == ingestpolicy.PriorityRealtime {
						queue = u.RealtimeUploadQueue()
					}
					queue <- batch
					u.startWorkers(parentCtx)
					requestCtx := <-started
					require.Equal(t, "ingestor", requestCtx.Value(shutdownContextKey{}))
					parentDeadline, ok := parentCtx.Deadline()
					require.True(t, ok)
					requestDeadline, ok := requestCtx.Deadline()
					require.True(t, ok)
					require.True(t, requestDeadline.After(parentDeadline), "uploads must use their own deadlines")

					// Pending batches must not be started after shutdown, regardless of priority.
					u.UploadQueue() <- pendingQueued
					u.RealtimeUploadQueue() <- pendingRealtime

					if cancelParentFirst {
						cancelParent()
						synctest.Wait()
						require.ErrorIs(t, parentCtx.Err(), context.Canceled)
						require.NoError(t, requestCtx.Err(), "parent cancellation before Close must leave uploads running")
						require.Equal(t, 1, calls)
					}
					closed := false
					go func() {
						_ = u.Close()
						closed = true
					}()
					synctest.Wait()
					require.NoError(t, requestCtx.Err(), "stopping intake must not cancel an active upload")
					require.False(t, closed, "Close must wait for the active upload")
					require.False(t, streamer.closed, "clients must stay open until workers exit")

					// The existing channel API permits sends, but work submitted after Close must not start.
					u.UploadQueue() <- afterClose
					synctest.Wait()
					require.Equal(t, 1, calls)

					close(finish)
					synctest.Wait()
					require.True(t, closed)
					require.True(t, streamer.closed)
					require.True(t, batch.IsReleased())
					require.True(t, batch.IsRemoved(), "the active upload must complete successfully")
					require.NoFileExists(t, path)
					require.Equal(t, 1, calls)
					require.False(t, pendingQueued.IsReleased())
					require.False(t, pendingRealtime.IsReleased())
					require.False(t, afterClose.IsReleased())
					require.Len(t, u.UploadQueue(), 2)
					require.Len(t, u.RealtimeUploadQueue(), 1)
					require.Zero(t, u.realtime.Slots.Stats().InUse)
				})
			})
		}
	}
}

func TestUploader_CloseWaitsForAllInflightUploads(t *testing.T) {
	env := newStreamingTestEnv(t)
	tables := []string{"Cpu0", "Cpu1", "Cpu2"}
	var batches []*cluster.Batch
	var paths []string
	for _, table := range tables {
		paths = append(paths, env.addSegment(t, table, ingestpolicy.PriorityQueued, testCSV, time.Time{}))
		batches = append(batches, env.nextBatch(t, ingestpolicy.PriorityQueued))
	}
	env.addSegment(t, "Pending", ingestpolicy.PriorityQueued, testCSV, time.Time{})
	pending := env.nextBatch(t, ingestpolicy.PriorityQueued)

	synctest.Test(t, func(t *testing.T) {
		u := env.u
		executionCtx, cancelExecution := context.WithCancel(context.Background())
		defer cancelExecution()
		prepareShutdownUploader(u)
		u.opts.ConcurrentUploads = len(batches)
		started := make(chan context.Context, len(batches))
		finish := make(map[string]chan struct{})
		for _, table := range tables {
			finish[table] = make(chan struct{})
			defer releaseUpload(finish[table])
		}
		u.queuedUpload = func(ctx context.Context, _ io.Reader, _, table string, _ adxschema.SchemaMapping) error {
			started <- ctx
			select {
			case <-finish[table]:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		for _, batch := range batches {
			u.UploadQueue() <- batch
		}
		u.startWorkers(executionCtx)
		for range batches {
			requestCtx := <-started
			require.NoError(t, requestCtx.Err())
			deadline, ok := requestCtx.Deadline()
			require.True(t, ok)
			require.Equal(t, batchUploadTimeout, time.Until(deadline))
		}
		u.UploadQueue() <- pending

		closed := false
		go func() {
			_ = u.Close()
			closed = true
		}()
		synctest.Wait()
		require.False(t, closed)

		for i, table := range tables {
			close(finish[table])
			synctest.Wait()
			require.True(t, batches[i].IsReleased())
			require.True(t, batches[i].IsRemoved())
			require.NoFileExists(t, paths[i])
			require.Equal(t, i == len(tables)-1, closed, "Close must wait for every worker, not just the first")
		}
		require.NoError(t, executionCtx.Err())
		require.False(t, pending.IsReleased())
		require.Len(t, u.UploadQueue(), 1)
	})
}

func TestUploader_BatchTimeoutUnblocksClose(t *testing.T) {
	env := newStreamingTestEnv(t)
	path := env.addSegment(t, "Cpu", ingestpolicy.PriorityQueued, testCSV, time.Time{})
	batch := env.nextBatch(t, ingestpolicy.PriorityQueued)

	synctest.Test(t, func(t *testing.T) {
		u := env.u
		prepareShutdownUploader(u)
		started := make(chan context.Context)
		u.queuedUpload = func(ctx context.Context, _ io.Reader, _, _ string, _ adxschema.SchemaMapping) error {
			started <- ctx
			<-ctx.Done()
			return ctx.Err()
		}
		u.UploadQueue() <- batch
		u.startWorkers(context.Background())
		requestCtx := <-started

		closed := make(chan error, 1)
		go func() {
			closed <- u.Close()
		}()
		synctest.Wait()
		select {
		case err := <-closed:
			t.Fatalf("Close returned before the upload deadline: %v", err)
		default:
		}
		require.NoError(t, requestCtx.Err())

		deadline, ok := requestCtx.Deadline()
		require.True(t, ok)
		// This advances synctest's fake clock, not wall-clock time. No sleeps or polling are needed.
		timer := time.NewTimer(time.Until(deadline))
		defer timer.Stop()
		<-timer.C
		synctest.Wait()
		require.NoError(t, <-closed)
		require.ErrorIs(t, requestCtx.Err(), context.DeadlineExceeded)
		require.True(t, batch.IsReleased())
		require.False(t, batch.IsRemoved())
		require.FileExists(t, path)
	})
}

type shutdownMgmt struct {
	requests chan context.Context
}

func (m *shutdownMgmt) Mgmt(ctx context.Context, _ string, _ azkustodata.Statement, _ ...azkustodata.QueryOption) (kustov1.Dataset, error) {
	m.requests <- ctx
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestUploader_UploadContextCancellationCancelsSchemaRequests(t *testing.T) {
	for _, priority := range []ingestpolicy.Priority{ingestpolicy.PriorityQueued, ingestpolicy.PriorityRealtime} {
		for _, stage := range []string{"table", "mapping", "streaming policy"} {
			if priority == ingestpolicy.PriorityQueued && stage == "streaming policy" {
				continue
			}
			t.Run(priority.String()+"/"+stage, func(t *testing.T) {
				env := newStreamingTestEnv(t)
				path := env.addSegment(t, "Cpu", priority, testCSV, time.Time{})
				batch := env.nextBatch(t, priority)
				synctest.Test(t, func(t *testing.T) {
					u := env.u
					executionCtx, cancelExecution := context.WithCancel(context.Background())
					defer cancelExecution()
					prepareShutdownUploader(u)
					u.queuedUpload = u.uploadReader
					if stage != "table" {
						require.NoError(t, u.syncer.EnsureTable(executionCtx, "Cpu", u.opts.DefaultMapping))
					}
					if stage == "streaming policy" {
						_, err := u.syncer.EnsureMapping(executionCtx, "Cpu", u.opts.DefaultMapping)
						require.NoError(t, err)
					}
					mgmt := &shutdownMgmt{requests: make(chan context.Context)}
					u.syncer.KustoCli = mgmt

					finished := false
					go func() {
						u.uploadBatch(executionCtx, batch)
						finished = true
					}()
					requestCtx := <-mgmt.requests
					synctest.Wait()
					require.False(t, finished)
					require.NoError(t, requestCtx.Err())
					cancelExecution()
					synctest.Wait()
					require.True(t, finished)
					require.ErrorIs(t, requestCtx.Err(), context.Canceled)
					require.True(t, batch.IsReleased())
					require.False(t, batch.IsRemoved())
					require.FileExists(t, path)
					require.Zero(t, env.stream.calls)
				})
			})
		}
	}
}

type shutdownTransport struct {
	requests chan context.Context
}

func (s *shutdownTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Path == "/v1/rest/auth/metadata" {
		// The SDK's endpoint metadata lookup is separate from the ingestion request.
		return &http.Response{StatusCode: http.StatusNotFound, Body: http.NoBody, Header: make(http.Header)}, nil
	}
	s.requests <- req.Context()
	<-req.Context().Done()
	return nil, req.Context().Err()
}

func TestUploader_UploadContextCancellationCancelsIngestRequests(t *testing.T) {
	for _, direct := range []bool{false, true} {
		name := "queued"
		if direct {
			name = "direct"
		}
		t.Run(name, func(t *testing.T) {
			env := newStreamingTestEnv(t)
			path := env.addSegment(t, "Cpu", ingestpolicy.PriorityQueued, testCSV, time.Time{})
			batch := env.nextBatch(t, ingestpolicy.PriorityQueued)
			synctest.Test(t, func(t *testing.T) {
				u := env.u
				executionCtx, cancelExecution := context.WithCancel(context.Background())
				defer cancelExecution()
				prepareShutdownUploader(u)
				u.queuedUpload = u.uploadReader
				u.requireDirectIngest = direct
				require.NoError(t, u.syncer.EnsureTable(executionCtx, "Cpu", u.opts.DefaultMapping))
				_, err := u.syncer.EnsureMapping(executionCtx, "Cpu", u.opts.DefaultMapping)
				require.NoError(t, err)

				transport := &shutdownTransport{requests: make(chan context.Context)}
				httpClient := &http.Client{Transport: transport}
				csb := azkustodata.NewConnectionStringBuilder("http://localhost:8080")
				u.KustoCli, err = azkustodata.New(csb, azkustodata.WithHttpClient(httpClient))
				require.NoError(t, err)
				defer u.KustoCli.Close()
				if !direct {
					u.ingestor, err = azkustoingest.New(csb, azkustoingest.WithHttpClient(httpClient))
					require.NoError(t, err)
					defer u.ingestor.Close()
				}

				finished := false
				go func() {
					u.uploadBatch(executionCtx, batch)
					finished = true
				}()
				requestCtx := <-transport.requests
				synctest.Wait()
				require.False(t, finished)
				require.NoError(t, requestCtx.Err())
				cancelExecution()
				synctest.Wait()
				require.True(t, finished)
				require.ErrorIs(t, requestCtx.Err(), context.Canceled)
				require.True(t, batch.IsReleased())
				require.False(t, batch.IsRemoved())
				require.FileExists(t, path)
			})
		})
	}
}

func TestUploader_CanceledBatchIsReleasedWithoutUploading(t *testing.T) {
	env := newStreamingTestEnv(t)
	executionCtx, cancelExecution := context.WithCancel(context.Background())
	defer cancelExecution()
	path := env.addSegment(t, "Cpu", ingestpolicy.PriorityRealtime, testCSV, time.Time{})
	batch := env.nextBatch(t, ingestpolicy.PriorityRealtime)
	cancelExecution()

	env.u.uploadBatch(executionCtx, batch)
	require.True(t, batch.IsReleased())
	require.False(t, batch.IsRemoved())
	require.FileExists(t, path)
	require.Zero(t, env.stream.calls)
	require.Zero(t, env.queue.calls)
}

type blockingGzipSource struct {
	started chan struct{}
	finish  chan struct{}
}

func (r *blockingGzipSource) Read([]byte) (int, error) {
	close(r.started)
	<-r.finish
	return 0, io.EOF
}

func TestUploader_newGzipReader_ProducerJoinedBeforeSourceCleanup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		u := NewUploader(nil, UploaderOpts{})
		source := &blockingGzipSource{started: make(chan struct{}), finish: make(chan struct{})}
		defer releaseUpload(source.finish)
		var wg sync.WaitGroup
		r := u.newGzipReader(source, &wg)
		<-source.started
		cleanedUp := make(chan struct{})
		go func() {
			_ = r.Close()
			wg.Wait()
			close(cleanedUp)
		}()
		synctest.Wait()
		select {
		case <-cleanedUp:
			t.Fatal("source cleanup started while the compressor was still reading")
		default:
		}
		close(source.finish)
		synctest.Wait()
		<-cleanedUp
	})
}
