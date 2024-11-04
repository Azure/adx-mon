package tail

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/Azure/adx-mon/collector/logs/engine"
	"github.com/Azure/adx-mon/collector/logs/sources/tail/sourceparse"
	"github.com/Azure/adx-mon/collector/logs/transforms/parser"
	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/tenebris-tech/tail"
	"golang.org/x/sync/errgroup"
)

type TailerConfig struct {
	Target          FileTailTarget
	UpdateChan      <-chan FileTailTarget
	AckGenerator    func(*types.Log) func()
	WorkerCreator   engine.WorkerCreatorFunc
	CursorDirectory string
	WorkerName      string
}

// Tailer is a specific instance of a file being tailed.
type Tailer struct {
	tail           *tail.Tail
	shutdown       context.CancelFunc
	errgroup       *errgroup.Group
	database       string
	table          string
	logTypeParser  sourceparse.LogTypeParser
	logLineParsers []parser.Parser
	resources      map[string]interface{}
}

func StartTailing(config TailerConfig) (*Tailer, error) {
	group, ctx := errgroup.WithContext(context.Background())
	ctx, shutdown := context.WithCancel(ctx)

	batchQueue := make(chan *types.Log, 512)
	outputQueue := make(chan *types.LogBatch, 1)
	tailConfig := tail.Config{Follow: true, ReOpen: true, Poll: true}
	existingCursorPath := cursorPath(config.CursorDirectory, config.Target.FilePath)
	fileId, position, err := readCursor(existingCursorPath)
	if err == nil {
		if logger.IsDebug() {
			logger.Debugf("TailSource: found existing cursor for file %q: %s %d", config.Target.FilePath, fileId, position)
		}
		tailConfig.Location = &tail.SeekInfo{
			Offset:         position,
			Whence:         io.SeekStart,
			FileIdentifier: fileId,
		}
	}

	tailFile, err := tail.TailFile(config.Target.FilePath, tailConfig)
	if err != nil {
		shutdown()
		return nil, fmt.Errorf("addTarget create tailfile: %w", err)
	}

	parsers, err := parser.NewParsers(config.Target.Parsers)
	if err != nil {
		shutdown()
		return nil, fmt.Errorf("addTarget create parsers: %w", err)
	}

	attributes := make(map[string]interface{})
	for k, v := range config.Target.Resources {
		attributes[k] = v
	}

	tailer := &Tailer{
		tail:           tailFile,
		shutdown:       shutdown,
		errgroup:       group,
		database:       config.Target.Database,
		table:          config.Target.Table,
		logTypeParser:  sourceparse.GetLogTypeParser(config.Target.LogType),
		logLineParsers: parsers,
		resources:      attributes,
	}
	group.Go(func() error {
		return readLines(ctx, tailer, config.UpdateChan, batchQueue)
	})

	batchConfig := engine.BatchConfig{
		MaxBatchSize: 1000,
		MaxBatchWait: 1 * time.Second,
		InputQueue:   batchQueue,
		OutputQueue:  outputQueue,
		AckGenerator: config.AckGenerator,
	}
	group.Go(func() error {
		return engine.BatchLogs(ctx, batchConfig)
	})

	worker := config.WorkerCreator(config.WorkerName, outputQueue)
	group.Go(worker.Run)

	return tailer, nil
}

// Stop stops the tailer and cleans up resources.
// Does not wait for the tailer to finish processing, to allow closing many tailers concurrently.
// Call Wait() after calling Stop() to wait for the tailer to finish processing.
func (t *Tailer) Stop() {
	t.tail.Cleanup()
	t.tail.Stop()
	t.shutdown()
}

// Wait waits for the tailer to finish processing.
func (t *Tailer) Wait() {
	t.errgroup.Wait()
}

func readLines(ctx context.Context, tailer *Tailer, updateChannel <-chan FileTailTarget, outputQueue chan<- *types.Log) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		// Receive updates from the optional updateChannel.
		case newTarget, ok := <-updateChannel:
			if ok {
				newParsers, err := parser.NewParsers(newTarget.Parsers)
				if err != nil {
					logger.Errorf("readLines: parser error for filename %q: %v", tailer.tail.Filename, err)
					continue
				}
				tailer.logLineParsers = newParsers
				tailer.database = newTarget.Database
				tailer.table = newTarget.Table
				tailer.resources = make(map[string]interface{})
				for k, v := range newTarget.Resources {
					tailer.resources[k] = v
				}
			}
		case line, ok := <-tailer.tail.Lines:
			if !ok {
				logger.Infof("readLines: tailer closed the channel for filename %q", tailer.tail.Filename)
				return nil // No longer getting lines due to the tailer being closed. Exit.
			}
			if line.Err != nil {
				logger.Errorf("readLines: tailer error for filename %q: %v", tailer.tail.Filename, line.Err)
				//skip
				continue
			}

			log := types.LogPool.Get(1).(*types.Log)
			log.Reset()

			isPartial, err := tailer.logTypeParser.Parse(line.Text, log)
			if err != nil {
				logger.Errorf("readLines: parselog error for filename %q: %v", tailer.tail.Filename, err)
				//skip
				types.LogPool.Put(log)
				continue
			}
			if isPartial {
				types.LogPool.Put(log)
				continue
			}

			position := line.Offset
			currentFileId := line.FileIdentifier
			log.Attributes[types.AttributeDatabaseName] = tailer.database
			log.Attributes[types.AttributeTableName] = tailer.table

			for k, v := range tailer.resources {
				log.Resource[k] = v
			}

			successfulParse := false
			for _, parser := range tailer.logLineParsers {
				err := parser.Parse(log)
				if err == nil {
					successfulParse = true
					break // successful parse
				} else if logger.IsDebug() {
					logger.Debugf("readLines: parser error for filename %q: %v", tailer.tail.Filename, err)
				}
			}

			if successfulParse {
				// Successful parse, remove the raw message
				delete(log.Body, types.BodyKeyMessage)
			}

			// Write after parsing to ensure these values are always set to values we need for acking.
			log.Attributes[cursor_position] = position
			log.Attributes[cursor_file_id] = currentFileId
			log.Attributes[cursor_file_name] = tailer.tail.Filename

			outputQueue <- log
		}
	}
}
