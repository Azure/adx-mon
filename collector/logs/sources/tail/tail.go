package tail

import (
	"context"
	"fmt"
	"time"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/tenebris-tech/tail"
	"golang.org/x/sync/errgroup"
)

const (
	LogTypeDocker = "docker"
)

type FileTailTarget struct {
	FilePath string
	LogType  string
}

type TailSourceConfig struct {
	StaticTargets []FileTailTarget
}

type TailSource struct {
	staticTargets []FileTailTarget
	outputQueue   chan *types.LogBatch

	closeFn context.CancelFunc
	group   *errgroup.Group
	tailers []*tail.Tail
}

func NewTailSource(config TailSourceConfig) (*TailSource, error) {
	return &TailSource{
		staticTargets: config.StaticTargets,
		outputQueue:   make(chan *types.LogBatch, 1),
	}, nil
}

func (s *TailSource) Open(ctx context.Context) error {
	ctx, closeFn := context.WithCancel(ctx)
	s.closeFn = closeFn

	group, ctx := errgroup.WithContext(ctx)
	s.group = group

	batchQueue := make(chan *types.Log, 512)
	batchConfig := types.BatchConfig{
		MaxBatchSize: 1000,
		MaxBatchWait: 1 * time.Second,
		InputQueue:   batchQueue,
		OutputQueue:  s.outputQueue,
		AckGenerator: func(log *types.Log) func() {
			// TODO
			return func() {
			}
		},
	}
	group.Go(func() error {
		return types.BatchLogs(ctx, batchConfig)
	})

	s.tailers = make([]*tail.Tail, 0, len(s.staticTargets))
	for _, target := range s.staticTargets {
		target := target
		tailer, err := tail.TailFile(target.FilePath, tail.Config{Follow: true, ReOpen: true})
		if err != nil {
			for _, t := range s.tailers {
				t.Cleanup()
				t.Stop()
			}
			return fmt.Errorf("TailSource open: %w", err)
		}
		s.tailers = append(s.tailers, tailer)

		group.Go(func() error {
			return readLines(ctx, target, tailer, batchQueue)
		})
	}

	return nil
}

func (s *TailSource) Close() error {
	for _, t := range s.tailers {
		t.Cleanup()
		t.Stop()
	}
	s.closeFn()
	s.group.Wait()
	return nil
}

func (s *TailSource) Name() string {
	return "tailsource"
}

func (s *TailSource) Queue() <-chan *types.LogBatch {
	return s.outputQueue
}

func readLines(ctx context.Context, target FileTailTarget, tailer *tail.Tail, outputQueue chan<- *types.Log) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case line, ok := <-tailer.Lines:
			if !ok {
				return fmt.Errorf("readLines: tailer closed the channel for filename %q", tailer.Filename)
			}
			if line.Err != nil {
				logger.Errorf("readLines: tailer error for filename %q: %v", tailer.Filename, line.Err)
				//skip
				continue
			}

			log := types.LogPool.Get(1).(*types.Log)
			log.Reset()

			var err error
			switch target.LogType {
			case LogTypeDocker:
				err = parseDockerLog(line.Text, log)
			default:
				err = parsePlaintextLog(line.Text, log)
			}
			if err != nil {
				logger.Errorf("readLines: parselog error for filename %q: %v", tailer.Filename, err)
				//skip
				continue
			}
			// TODO combine partial lines. Docker separates lines with newlines in the log message.
			outputQueue <- log
		}
	}
}

func parsePlaintextLog(line string, log *types.Log) error {
	log.Timestamp = uint64(time.Now().UnixNano())
	log.ObservedTimestamp = uint64(time.Now().UnixNano())
	log.Body["message"] = line

	return nil
}
