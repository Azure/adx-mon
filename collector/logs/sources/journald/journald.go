package journald

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/coreos/go-systemd/sdjournal"
	"golang.org/x/sync/errgroup"
)

const (
	journald_message_field = "MESSAGE"

	journald_cursor_attribute = "journald.cursor"
)

type JournaldSourceConfig struct {
	CursorPath string
}

// Journald source reads from the systemd journal
type JournaldSource struct {
	reader          JournalReader
	outputQueue     chan *types.LogBatch
	partialMessages map[string]*sdjournal.JournalEntry
	cursorPath      string

	closeFn context.CancelFunc
	group   *errgroup.Group
	ackmtx  sync.Mutex
}

// JournalReader is an interface for reading from the journal
type JournalReader interface {
	AddMatch(match string) error
	Next() (uint64, error)
	GetEntry() (*sdjournal.JournalEntry, error)
	GetCursor() (string, error)
	NextSkip(skip uint64) (uint64, error)
	SeekTail() error
	SeekCursor(cursor string) error
	Wait(timeout time.Duration) int
	Close() error
}

// NewJournaldSource creates a new journald source
func NewJournaldSource(config JournaldSourceConfig) (*JournaldSource, error) {
	reader, err := sdjournal.NewJournal()
	if err != nil {
		return nil, err
	}

	return &JournaldSource{
		reader:          reader,
		outputQueue:     make(chan *types.LogBatch, 1),
		partialMessages: make(map[string]*sdjournal.JournalEntry),
		cursorPath:      config.CursorPath,
	}, nil
	// TODO add conjunctions, etc
}

func (j *JournaldSource) Open(ctx context.Context) error {
	ctx, closeFn := context.WithCancel(ctx)
	j.closeFn = closeFn

	if err := j.seekAtStart(); err != nil {
		return err
	}

	batchQueue := make(chan *types.Log, 1)

	// errGroupContext is used within each runner to:
	// 1. Propagate shutdowns on Close()
	// 2. Propagate shutdowns if any task fails
	group, errGroupContext := errgroup.WithContext(ctx)
	j.group = group

	// Read from Journal.
	group.Go(func() error {
		return j.readFromJournal(errGroupContext, batchQueue)
	})

	// Batch logs.
	batchConfig := types.BatchConfig{
		MaxBatchSize: 1000,
		MaxBatchWait: 1 * time.Second,
		InputQueue:   batchQueue,
		OutputQueue:  j.outputQueue,
		AckGenerator: func(log *types.Log) func() {
			cursor := log.Attributes[journald_cursor_attribute].(string)
			return func() {
				j.ackBatch(cursor)
			}
		},
	}
	group.Go(func() error {
		return types.BatchLogs(errGroupContext, batchConfig)
	})

	return nil
}

func (j *JournaldSource) Close() error {
	j.closeFn()
	j.group.Wait()
	return j.reader.Close()
}

func (j *JournaldSource) Queue() <-chan *types.LogBatch {
	return j.outputQueue
}

func (j *JournaldSource) Name() string {
	return "journald"
}

func (j *JournaldSource) seekAtStart() error {
	err := j.seekWithCursorFile()
	if err != nil {
		logger.Warnf("Unable to seek with cursor file: %s", err)
		err = j.reader.SeekTail()
		if err != nil {
			return fmt.Errorf("failed to seek to tail: %w", err)
		}
	}
	return nil
}

func (j *JournaldSource) seekWithCursorFile() error {
	j.ackmtx.Lock()
	defer j.ackmtx.Unlock()
	cursorFile, err := os.Open(j.cursorPath)
	if err != nil {
		return fmt.Errorf("failed to open cursor file: %w", err)
	}
	cursorBytes, err := io.ReadAll(io.LimitReader(cursorFile, 1024))
	if err != nil {
		return fmt.Errorf("failed to read cursor file: %w", err)
	}
	err = j.reader.SeekCursor(string(cursorBytes))
	if err != nil {
		return fmt.Errorf("failed to seek to cursor: %w", err)
	}
	j.reader.NextSkip(1) // Skip the first entry we already read
	return nil
}

func (j *JournaldSource) readFromJournal(ctx context.Context, batchQueue chan<- *types.Log) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			ret, err := j.reader.Next()
			if err != nil {
				// Unclear how to handle these errors. The implementation of sd_journald_next returns errors
				// when attempting to continue iteration. Suspect these are i/o related if there are issues.
				logger.Errorf("failed to advance in journal: %v", err)
				j.backoff()
				continue
			}

			if ret == 0 {
				// Wait for entries
				if err := j.waitForNewJournalEntries(ctx); err != nil {
					logger.Errorf("failed to wait for new journal entries: %v", err)
					j.backoff()
					continue
				}
				continue
			}

			entry, err := j.reader.GetEntry()
			if err != nil {
				logger.Errorf("failed to get journal entry: %v", err)
				j.backoff()
				continue
			}

			entry, ok := j.combinePartialMessages(entry)
			if !ok {
				// We are waiting for more messages to combine
				continue
			}

			log := types.LogPool.Get(1).(*types.Log)
			log.Reset()
			log.Timestamp = uint64(entry.RealtimeTimestamp)
			log.ObservedTimestamp = uint64(time.Now().UnixNano())
			log.Body["message"] = entry.Fields[journald_message_field]
			log.Attributes[journald_cursor_attribute] = entry.Cursor

			enrichContainerMetadata(log, entry)
			batchQueue <- log
		}
	}
}

func (j *JournaldSource) waitForNewJournalEntries(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			status := j.reader.Wait(250 * time.Millisecond)
			switch status {
			case sdjournal.SD_JOURNAL_NOP:
				continue
			case sdjournal.SD_JOURNAL_APPEND, sdjournal.SD_JOURNAL_INVALIDATE:
				// New events available. return from here.
				return nil
			default:
				if status < 0 {
					return fmt.Errorf("error status waiting for journal entries: %d", status)
				}
			}
		}
	}
}

func (j *JournaldSource) ackBatch(cursor string) {
	j.ackmtx.Lock()
	defer j.ackmtx.Unlock()
	output, err := os.Create(j.cursorPath)
	if err != nil {
		logger.Errorf("Could not create/truncate file %s with error: %s", j.cursorPath, err)
		return
	}
	_, err = output.WriteString(cursor)
	if err != nil {
		logger.Errorf("Could not ack cursor %s with error: %s", cursor, err)
	}
}

func (j *JournaldSource) backoff() {
	time.Sleep(1 * time.Second)
}
