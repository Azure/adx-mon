//go:build linux && cgo

package journal

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Azure/adx-mon/collector/logs/engine"
	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/coreos/go-systemd/sdjournal"
)

const (
	journald_cursor_attribute          = "adxmon_journald_cursor"
	journald_cursor_filename_attribute = "adxmon_journald_cursor_filename"
)

// journalReader is an interface for reading journal entries.
type journalReader interface {
	AddMatch(match string) error
	Next() (uint64, error)
	GetEntry() (*sdjournal.JournalEntry, error)
	NextSkip(skip uint64) (uint64, error)
	SeekHead() error
	SeekCursor(cursor string) error
	Wait(timeout time.Duration) int
	Close() error
}

// Source reads logs from the journald journal. Implements the Source interface.
type Source struct {
	targets         []JournalTargetConfig
	cursorDirectory string
	workerCreator   engine.WorkerCreatorFunc

	tailers []*tailer
	closeFn context.CancelFunc
	wg      sync.WaitGroup
}

// New creates a new journal source.
func New(config SourceConfig) *Source {
	return &Source{
		targets:         config.Targets,
		cursorDirectory: config.CursorDirectory,
		workerCreator:   config.WorkerCreator,
	}
}

func (s *Source) Open(ctx context.Context) error {
	ctx, closeFn := context.WithCancel(ctx)
	s.closeFn = closeFn

	batchQueue := make(chan *types.Log, 512)
	outputQueue := make(chan *types.LogBatch, 1)

	ackGenerator := func(*types.Log) func() { return func() {} }
	if s.cursorDirectory != "" {
		ackGenerator = func(log *types.Log) func() {
			return func() {
				cursorFilePath := log.Attributes[journald_cursor_filename_attribute].(string)
				cursorValue := log.Attributes[journald_cursor_attribute].(string)
				writeCursor(cursorFilePath, cursorValue)
			}
		}
	}

	tailers := make([]*tailer, 0, len(s.targets))
	for _, target := range s.targets {
		logger.Info("Opening journal source", "filters", target.Matches, "database", target.Database, "table", target.Table)
		reader, err := sdjournal.NewJournal()
		if err != nil {
			s.Close()
			return fmt.Errorf("journal source open: %v", err)
		}

		for _, match := range target.Matches {
			if match == "+" {
				err := reader.AddDisjunction()
				if err != nil {
					s.Close()
					return fmt.Errorf("journal source open addDisjunction: %w", err)
				}
			}

			if err := reader.AddMatch(match); err != nil {
				s.Close()
				return fmt.Errorf("journal source open addMatch %s: %w", match, err)
			}
		}

		cPath := cursorPath(s.cursorDirectory, target.Matches, target.Database, target.Table)
		tailer := &tailer{
			reader:         reader,
			database:       target.Database,
			table:          target.Table,
			cursorFilePath: cPath,
			logLineParsers: target.LogLineParsers,
			batchQueue:     batchQueue,

			streamPartials: make(map[string]string),
		}

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			tailer.readFromJournal(ctx)
		}()

		batchConfig := engine.BatchConfig{
			MaxBatchSize: 1000,
			MaxBatchWait: 1 * time.Second,
			InputQueue:   batchQueue,
			OutputQueue:  outputQueue,
			AckGenerator: ackGenerator,
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			engine.BatchLogs(ctx, batchConfig)
		}()

		worker := s.workerCreator("journal", outputQueue)
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			worker.Run()
		}()

		tailers = append(tailers, tailer)
	}
	s.tailers = tailers

	return nil
}

func (s *Source) Close() error {
	s.closeFn()
	s.wg.Wait()
	return nil
}

func (s *Source) Name() string {
	return "journal"
}
