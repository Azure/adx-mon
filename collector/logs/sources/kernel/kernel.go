package kernel

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Azure/adx-mon/collector/logs/engine"
	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/siderolabs/go-kmsg"
)

// Constants for attribute keys
const (
	kernelSequenceAttr   = "adxmon_kernel_sequence"
	kernelCursorFilename = "adxmon_kernel_cursor_filename"
)

// KernelSourceConfig configures KernelSource.
type KernelSourceConfig struct {
	WorkerCreator   engine.WorkerCreatorFunc
	CursorDirectory string
	Targets         []KernelTargetConfig
}

type KernelTargetConfig struct {
	Database       string
	Table          string
	PriorityFilter string

	// processed is the last processed sequence number as
	// stored in the cursor file if one exists.
	processed uint64
	// cursorFile is the name of the cursor file
	cursorFile string
	// priority is the kmsg priority of the log, which speeds up comparison
	priority kmsg.Priority
}

// KernelSource implements the types.Source interface for reading kernel logs.
type KernelSource struct {
	workerCreator engine.WorkerCreatorFunc
	ackGenerator  func(*types.Log) func()
	targets       []KernelTargetConfig
	cursorDir     string

	cancel context.CancelFunc
	wg     sync.WaitGroup // Added WaitGroup for goroutine lifecycle management
	reader kmsg.Reader    // Add reader field for testing
}

// NewKernelSource creates a new KernelSource.
func NewKernelSource(config KernelSourceConfig) (*KernelSource, error) {
	return &KernelSource{
		workerCreator: config.WorkerCreator,
		targets:       config.Targets,
		cursorDir:     config.CursorDirectory,
	}, nil
}

// Open starts reading kernel logs.
func (s *KernelSource) Open(ctx context.Context) error {
	s.ackGenerator = noopAckGenerator
	if s.cursorDir != "" {
		s.ackGenerator = func(log *types.Log) func() {
			cursorFileName := types.StringOrEmpty(log.GetAttributeValue(kernelCursorFilename))
			cursorPositionVal, ok := log.GetAttributeValue(kernelSequenceAttr)

			if !ok || cursorFileName == "" {
				return noopAck
			}
			cursorPosition, ok := cursorPositionVal.(uint64)
			if !ok {
				return noopAck
			}

			return func() {
				s.ackSequence(cursorPosition, cursorFileName)
			}
		}
	}

	for i, target := range s.targets {
		target.cursorFile = SafeFilename(target.Database, target.Table)
		fp := filepath.Join(s.cursorDir, target.cursorFile)
		if offset, err := readOffset(fp); err == nil {
			target.processed = offset
		}
		target.priority = stringToPriority(target.PriorityFilter)
		s.targets[i] = target
	}

	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	outputQueue := make(chan *types.LogBatch, 1)
	batchQueue := make(chan *types.Log, 512)

	// Start reading kernel logs
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.readKernelLogs(ctx, batchQueue)
	}()

	// Setup batching
	batchConfig := engine.BatchConfig{
		MaxBatchSize: 1000,
		MaxBatchWait: 1 * time.Second,
		InputQueue:   batchQueue,
		OutputQueue:  outputQueue,
		AckGenerator: s.ackGenerator,
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		engine.BatchLogs(ctx, batchConfig)
	}()

	// Create and start worker
	worker := s.workerCreator(s.Name(), outputQueue)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		worker.Run()
	}()

	return nil
}

// Close stops reading kernel logs and waits for goroutines to finish.
func (s *KernelSource) Close() error {
	s.cancel()

	// Wait for all goroutines to finish
	s.wg.Wait()

	return nil
}

// Name returns the name of the source.
func (s *KernelSource) Name() string {
	return "kernelsource"
}

// ackSequence writes the sequence number to the offset file
func (s *KernelSource) ackSequence(sequence uint64, cursorFile string) {
	if err := writeOffset(filepath.Join(s.cursorDir, cursorFile), sequence); err != nil {
		logger.Errorf("failed to write kernel offset: %v", err)
	}
}

// readKernelLogs reads logs from the kernel message buffer.
func (s *KernelSource) readKernelLogs(ctx context.Context, outputQueue chan<- *types.Log) {
	var reader kmsg.Reader
	var err error

	if s.reader != nil {
		// Use injected reader for testing
		reader = s.reader
	} else {
		// Create real reader
		reader, err = kmsg.NewReader(kmsg.Follow())
		if err != nil {
			logger.Errorf("failed to create kmsg reader: %v", err)
			return
		}
	}
	defer reader.Close()

	// Read kernel log entries.
	// There is only one kernel log source stream but n targets.
	for pkt := range reader.Scan(ctx) {
		if pkt.Err != nil {
			logger.Errorf("failed to read kernel log: %v", pkt.Err)
			continue
		}

		entrySeq := uint64(pkt.Message.SequenceNumber)
		for _, target := range s.targets {
			if target.processed != 0 && entrySeq <= target.processed {
				continue // skip already processed logs
			}
			if target.priority < pkt.Message.Priority {
				continue // skip logs with lower priority
			}

			log := types.LogPool.Get(1).(*types.Log)
			log.Reset()

			log.SetTimestamp(uint64(pkt.Message.Timestamp.UnixNano()))
			log.SetObservedTimestamp(uint64(time.Now().UnixNano()))
			log.SetBodyValue(types.BodyKeyMessage, pkt.Message.Message)
			log.SetAttributeValue("priority", pkt.Message.Priority)

			log.SetAttributeValue(types.AttributeDatabaseName, target.Database)
			log.SetAttributeValue(types.AttributeTableName, target.Table)

			log.SetAttributeValue(kernelSequenceAttr, entrySeq)
			log.SetAttributeValue(kernelCursorFilename, target.cursorFile)

			outputQueue <- log
		}
	}
}

// SafeFilename creates a Linux filesystem-safe filename from database and table names.
// It prefixes the name with "kernel_" and uses a short hash to avoid length issues.
func SafeFilename(database, table string) string {
	// SHA-256 hash (shorter, not reversible)
	combined := database + "_" + table
	hash := sha256.Sum256([]byte(combined))
	// Use first 8 bytes of hash for a reasonable length filename
	hashStr := fmt.Sprintf("%x", hash[:8])
	return fmt.Sprintf("kernel_%s", hashStr)
}

func readOffset(path string) (uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	// File doesn't contain enough bytes
	if len(data) < 8 {
		return 0, fmt.Errorf("invalid offset file format: insufficient data")
	}

	// Read uint64 directly from bytes (little-endian)
	return binary.LittleEndian.Uint64(data[:8]), nil
}

func writeOffset(path string, sequence uint64) error {
	// Write directly as binary
	data := make([]byte, 8)
	binary.LittleEndian.PutUint64(data, sequence)
	return os.WriteFile(path, data, 0644)
}

// stringToPriority converts a priority string to kmsg.Priority
func stringToPriority(priorityStr string) kmsg.Priority {
	// Default to Info if empty
	if priorityStr == "" {
		return kmsg.Info
	}

	// Convert string to Priority enum
	switch strings.ToLower(priorityStr) {
	case "emerg":
		return kmsg.Emerg
	case "alert":
		return kmsg.Alert
	case "crit":
		return kmsg.Crit
	case "err", "error":
		return kmsg.Err
	case "warning", "warn":
		return kmsg.Warning
	case "notice":
		return kmsg.Notice
	case "info":
		return kmsg.Info
	case "debug":
		return kmsg.Debug
	default:
		// If invalid, default to Info
		logger.Warnf("Invalid priority filter: %s, defaulting to 'info'", priorityStr)
		return kmsg.Info
	}
}

var (
	noopAck          = func() {}
	noopAckGenerator = func(*types.Log) func() { return noopAck }
)
