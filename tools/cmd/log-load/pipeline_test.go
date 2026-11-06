package main

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
)

type testWriter struct {
	write      func(context.Context, *v1.ExportLogsServiceRequest) (writeResult, error)
	closeErr   error
	closeCount atomic.Int64
}

func (w *testWriter) WriteBatch(ctx context.Context, batch *v1.ExportLogsServiceRequest) (writeResult, error) {
	if w.write != nil {
		return w.write(ctx, batch)
	}
	return writeResult{records: recordCount(batch)}, nil
}

func (w *testWriter) Close() (writeResult, error) {
	w.closeCount.Add(1)
	return writeResult{}, w.closeErr
}

func testGenerator(total int64) *continuousDataGenerator {
	return &continuousDataGenerator{batchSize: 4, totalSamples: total, database: "DB", table: "Logs"}
}

func awaitResult(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("pipeline did not stop")
		return nil
	}
}

func awaitStarted(t *testing.T, started <-chan struct{}) {
	t.Helper()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("writer did not start")
	}
}

func TestPipelineWaitsForInflightWrite(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	writer := &testWriter{write: func(ctx context.Context, batch *v1.ExportLogsServiceRequest) (writeResult, error) {
		close(started)
		select {
		case <-release:
			return writeResult{records: recordCount(batch), bytes: 123}, nil
		case <-ctx.Done():
			return writeResult{}, ctx.Err()
		}
	}}
	ctx := t.Context()
	s := &stats{}
	done := make(chan error, 1)
	go func() { done <- runPipeline(ctx, testGenerator(2), []batchWriter{writer}, s, false) }()
	awaitStarted(t, started)
	select {
	case err := <-done:
		t.Fatalf("pipeline returned before in-flight work completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	if s.written.Load() != 0 {
		t.Fatal("in-flight work was counted as written")
	}
	close(release)
	if err := awaitResult(t, done); err != nil {
		t.Fatal(err)
	}
	if s.generated.Load() != 2 || s.written.Load() != 2 || s.bytes.Load() != 123 || writer.closeCount.Load() != 1 {
		t.Fatalf("wrong completion stats: generated=%d written=%d bytes=%d closes=%d", s.generated.Load(), s.written.Load(), s.bytes.Load(), writer.closeCount.Load())
	}
}

func TestPipelineWriterFailureCancelsProducer(t *testing.T) {
	writeErr, closeErr := errors.New("write failed"), errors.New("close failed")
	writer := &testWriter{
		write: func(context.Context, *v1.ExportLogsServiceRequest) (writeResult, error) {
			return writeResult{records: 1, bytes: 10}, writeErr
		},
		closeErr: closeErr,
	}
	ctx := t.Context()
	s := &stats{}
	done := make(chan error, 1)
	go func() { done <- runPipeline(ctx, testGenerator(0), []batchWriter{writer}, s, false) }()
	err := awaitResult(t, done)
	if !errors.Is(err, writeErr) || !errors.Is(err, closeErr) {
		t.Fatalf("missing I/O failure: %v", err)
	}
	if writer.closeCount.Load() != 1 || s.written.Load() != 1 || s.bytes.Load() != 10 {
		t.Fatal("writer was not closed or partial output was not counted")
	}
}

func TestPipelineCancellationClosesAllWriters(t *testing.T) {
	started := make(chan struct{}, 3)
	writers := make([]batchWriter, 3)
	for i := range writers {
		writers[i] = &testWriter{write: func(ctx context.Context, _ *v1.ExportLogsServiceRequest) (writeResult, error) {
			started <- struct{}{}
			<-ctx.Done()
			return writeResult{}, ctx.Err()
		}}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runPipeline(ctx, testGenerator(0), writers, &stats{}, false) }()
	for range writers {
		awaitStarted(t, started)
	}
	cancel()
	if err := awaitResult(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation: %v", err)
	}
	for _, writer := range writers {
		if writer.(*testWriter).closeCount.Load() != 1 {
			t.Fatal("writer not closed exactly once")
		}
	}
}

type errorGenerator struct{ err error }

func (g errorGenerator) Read() (*v1.ExportLogsServiceRequest, error) { return nil, g.err }

func TestPipelineGeneratorAndCloseErrors(t *testing.T) {
	for _, readErr := range []error{io.EOF, errors.New("generation failed")} {
		closeErr := errors.New("close failed")
		writer := &testWriter{closeErr: closeErr}
		err := runPipeline(context.Background(), errorGenerator{err: readErr}, []batchWriter{writer}, &stats{}, false)
		if !errors.Is(err, closeErr) || (readErr != io.EOF && !errors.Is(err, readErr)) {
			t.Fatalf("missing error: %v", err)
		}
		if writer.closeCount.Load() != 1 {
			t.Fatal("writer not closed exactly once")
		}
	}
}

func TestGeneratorExactBatchSizes(t *testing.T) {
	for _, total := range []int64{1, 3, 4, 5, 8, 11} {
		gen := testGenerator(total)
		var count int64
		for {
			batch, err := gen.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			got := recordCount(batch)
			if want := min(int64(gen.batchSize), total-count); got != want {
				t.Fatalf("total=%d batch size=%d, want %d", total, got, want)
			}
			count += got
		}
		if count != total {
			t.Fatalf("total=%d generated=%d", total, count)
		}
		if _, err := gen.Read(); err != io.EOF {
			t.Fatalf("generator did not remain exhausted: %v", err)
		}
	}
}
