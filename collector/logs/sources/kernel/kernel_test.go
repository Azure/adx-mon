package kernel

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Azure/adx-mon/collector/logs"
	"github.com/Azure/adx-mon/collector/logs/engine"
	"github.com/Azure/adx-mon/collector/logs/sinks"
	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/siderolabs/go-kmsg"
	"github.com/stretchr/testify/require"
)

func TestKernelSource(t *testing.T) {
	t.Parallel()

	testDir := t.TempDir()
	cursorFile := filepath.Join(testDir, SafeFilename("Logs", "Kernel"))

	// Create a channel that the mock reader will use
	ch := make(chan kmsg.Packet)

	// Create mock reader (minimal implementation)
	mockReader := &mockKernelReader{ch: ch}

	// Send fake logs in a goroutine
	numLogs := int64(1000)
	go func() {
		defer close(ch)
		for i := range numLogs {
			ch <- kmsg.Packet{
				Message: kmsg.Message{
					SequenceNumber: i + 1,
					Timestamp:      time.Now(),
					Message:        "Fake log message",
					Priority:       6,
				},
			}
		}
	}()

	// Set up source with mock reader
	sink := sinks.NewCountingSink(numLogs)
	workerCreator := engine.WorkerCreator(nil, []types.Sink{sink})
	source, err := NewKernelSource(KernelSourceConfig{
		WorkerCreator:   workerCreator,
		CursorDirectory: testDir,
		Targets: []KernelTargetConfig{
			{
				Database: "Logs",
				Table:    "Kernel",
			},
		},
	})
	require.NoError(t, err)
	source.reader = mockReader // inject mock reader

	service := &logs.Service{
		Source: source,
		Sink:   sink,
	}

	ctx := context.Background()
	require.NoError(t, service.Open(ctx))
	defer service.Close()

	<-sink.DoneChan()

	offset, err := readOffset(cursorFile)
	require.NoError(t, err)
	require.Equal(t, uint64(numLogs), offset)
}

func TestKernelSourceWithOffset(t *testing.T) {
	t.Parallel()

	testDir := t.TempDir()
	offsetFile := filepath.Join(testDir, SafeFilename("Logs", "Kernel"))

	// Simulate that we have already processed some logs
	processed := uint64(5)
	writeOffset(offsetFile, processed)

	// Create a channel that the mock reader will use
	ch := make(chan kmsg.Packet)

	// Create mock reader (minimal implementation)
	mockReader := &mockKernelReader{ch: ch}

	// Send fake logs in a goroutine
	numLogs := int64(processed * 2)
	go func() {
		defer close(ch)
		for i := range numLogs {
			ch <- kmsg.Packet{
				Message: kmsg.Message{
					SequenceNumber: i + 1,
					Timestamp:      time.Now(),
					Message:        "Fake log message",
					Priority:       6,
				},
			}
		}
	}()

	// Set up source with mock reader
	sink := sinks.NewCountingSink(int64(processed))
	workerCreator := engine.WorkerCreator(nil, []types.Sink{sink})
	source, err := NewKernelSource(KernelSourceConfig{
		WorkerCreator:   workerCreator,
		CursorDirectory: testDir,
		Targets: []KernelTargetConfig{
			{
				Database: "Logs",
				Table:    "Kernel",
			},
		},
	})
	require.NoError(t, err)
	source.reader = mockReader // inject mock reader

	service := &logs.Service{
		Source: source,
		Sink:   sink,
	}

	ctx := context.Background()
	require.NoError(t, service.Open(ctx))
	defer service.Close()

	// We're writing an offset tombstone to simulate we've already processed `processed` logs.
	// We then send `processed * 2` logs to the kernel source reader, simulating that a new
	// kernel source reader has started and is processing logs, starting at a position we've already
	// processed. So we expect the counting sink to receive `processed` logs, but we expect
	// the final offset written to be `processed * 2`.
	<-sink.DoneChan()

	offset, err := readOffset(offsetFile)
	require.NoError(t, err)
	require.Equal(t, uint64(numLogs), offset)
}

func TestKernelSourceWithMultipleTargets(t *testing.T) {
	t.Parallel()

	testDir := t.TempDir()

	// Create a channel that the mock reader will use
	ch := make(chan kmsg.Packet)

	// Create mock reader (minimal implementation)
	mockReader := &mockKernelReader{ch: ch}

	// Send fake logs in a goroutine
	go func() {
		defer close(ch)
		ch <- kmsg.Packet{
			Message: kmsg.Message{
				SequenceNumber: 1,
				Timestamp:      time.Now(),
				Message:        "Fake log message",
				Priority:       6, // "info" level
			},
		}
		ch <- kmsg.Packet{
			Message: kmsg.Message{
				SequenceNumber: 2,
				Timestamp:      time.Now(),
				Message:        "Fake log message",
				Priority:       2, // "warning" level
			},
		}
		ch <- kmsg.Packet{
			Message: kmsg.Message{
				SequenceNumber: 3,
				Timestamp:      time.Now(),
				Message:        "Fake log message",
				Priority:       3, // "err" level
			},
		}
	}()

	// 3 logs are sent. All 3 match the first target (DB.A) and 1 log matches the second target (DB.B).
	// The second target (DB.B) has a priority filter of "err", which is Priority 3, so it will
	// only process the last log.

	// Set up source with mock reader
	sink := sinks.NewCountingSink(3)
	workerCreator := engine.WorkerCreator(nil, []types.Sink{sink})
	source, err := NewKernelSource(KernelSourceConfig{
		WorkerCreator:   workerCreator,
		CursorDirectory: testDir,
		Targets: []KernelTargetConfig{
			{
				Database: "DB",
				Table:    "A",
			},
			{
				Database:       "DB",
				Table:          "B",
				PriorityFilter: "err",
			},
		},
	})
	require.NoError(t, err)
	source.reader = mockReader // inject mock reader

	service := &logs.Service{
		Source: source,
		Sink:   sink,
	}

	ctx := context.Background()
	require.NoError(t, service.Open(ctx))
	defer service.Close()

	<-sink.DoneChan()

	offset, err := readOffset(filepath.Join(testDir, SafeFilename("DB", "B")))
	require.NoError(t, err)
	require.Equal(t, uint64(3), offset)
}

func BenchmarkKernelSource(b *testing.B) {
	testDir := b.TempDir()

	// Create mock reader
	mockReader := &mockKernelReader{
		ch: make(chan kmsg.Packet, 1000), // Buffer channel for better performance
	}

	sink := sinks.NewCountingSink(int64(b.N))
	workerCreator := engine.WorkerCreator(nil, []types.Sink{sink})
	source, _ := NewKernelSource(KernelSourceConfig{
		WorkerCreator:   workerCreator,
		CursorDirectory: testDir,
		Targets: []KernelTargetConfig{
			{
				Database: "Logs",
				Table:    "Kernel",
			},
		},
	})
	source.reader = mockReader

	service := &logs.Service{
		Source: source,
		Sink:   sink,
	}

	ctx := context.Background()
	service.Open(ctx)
	defer service.Close()

	// Reset timer after setup
	b.ResetTimer()

	// Send exactly b.N packets
	go func() {
		for i := 0; i < b.N; i++ {
			mockReader.ch <- kmsg.Packet{
				Message: kmsg.Message{
					SequenceNumber: int64(i),
					Timestamp:      time.Now(),
					Message:        "Fake log message for benchmarking",
					Priority:       6,
				},
			}
		}
		close(mockReader.ch)
	}()

	<-sink.DoneChan()

	b.StopTimer()

	// Report allocations per operation
	b.ReportAllocs()
}

// mockKernelReader is a simplified mock for tests where we can't write to kernel
type mockKernelReader struct {
	ch chan kmsg.Packet
}

func (m *mockKernelReader) Scan(ctx context.Context) <-chan kmsg.Packet {
	return m.ch
}

func (m *mockKernelReader) Close() error {
	return nil
}
