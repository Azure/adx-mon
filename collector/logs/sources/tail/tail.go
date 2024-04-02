package tail

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Azure/adx-mon/collector/logs/engine"
	"github.com/Azure/adx-mon/collector/logs/transforms/parser"
	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/tenebris-tech/tail"
	"golang.org/x/sync/errgroup"
)

type Type string

const (
	cursor_position  = "cursor_position"
	cursor_file_id   = "cursor_file_id"
	cursor_file_name = "cursor_file_name"

	// Log types for extracting the timestamp and message
	LogTypeDocker Type = "docker"
	LogTypePlain  Type = "plain"
)

var (
	noopAckGenerator = func(*types.Log) func() { return func() {} }
)

// FileTailTarget describes a file to tail, how to parse it, and the destination for the parsed logs.
// It is used int he list of StaticTargets in TailSourceConfig, or used in AddTarget.
type FileTailTarget struct {
	// FilePath is the file to tail.
	FilePath string

	// LogType is the format of the log file. e.g. docker for logs written by the docker json driver.
	// This provides a mechanism to combine split lines, parse timestamps (if present), and extract a message field.
	// Defaults to plain.
	LogType Type

	// The destination database name. Populated into the databasename attribute of the log that can be overwritten by transforms.
	Database string

	// The destination table name. Populated into the tablename attribute of the log that can be overwritten by transforms.
	Table string

	// Parsers is a list of parsers names to apply to each line of the log file to attempt to extract fields from the message body.
	// These are run sequentially until one succeeds, or until all have been tried.
	// These are converted into parser.ParserType.
	Parsers []string
}

// TailSourceConfig configures TailSource.
type TailSourceConfig struct {
	StaticTargets   []FileTailTarget
	CursorDirectory string
	WorkerCreator   engine.WorkerCreatorFunc
}

// Tailer is a specific instance of a file being tailed.
type Tailer struct {
	tail           *tail.Tail
	shutdown       context.CancelFunc
	database       string
	table          string
	logTypeParser  LogTypeParser
	logLineParsers []parser.Parser
}

func (t *Tailer) Stop() {
	t.tail.Cleanup()
	t.tail.Stop()
	t.shutdown()
}

// TailSource implements the types.Source interface for tailing files.
type TailSource struct {
	staticTargets   []FileTailTarget
	cursorDirectory string
	workerCreator   engine.WorkerCreatorFunc

	mu           sync.RWMutex
	closeFn      context.CancelFunc
	groupCtx     context.Context
	group        *errgroup.Group
	ackGenerator func(*types.Log) func()
	tailers      map[string]*Tailer
}

func NewTailSource(config TailSourceConfig) (*TailSource, error) {
	return &TailSource{
		staticTargets:   config.StaticTargets,
		cursorDirectory: config.CursorDirectory,
		workerCreator:   config.WorkerCreator,
	}, nil
}

func (s *TailSource) Open(ctx context.Context) error {
	ctx, closeFn := context.WithCancel(ctx)
	s.closeFn = closeFn

	group, ctx := errgroup.WithContext(ctx)
	s.group = group
	s.groupCtx = ctx

	s.ackGenerator = noopAckGenerator
	if s.cursorDirectory != "" {
		s.ackGenerator = func(log *types.Log) func() {
			cursorFileName := log.Attributes[cursor_file_name].(string)
			cursorFileId := log.Attributes[cursor_file_id].(string)
			cursorPosition := log.Attributes[cursor_position].(int64)
			return func() {
				s.ackBatch(cursorFileName, cursorFileId, cursorPosition)
			}
		}
	}

	s.tailers = map[string]*Tailer{}
	for _, target := range s.staticTargets {
		target := target

		err := s.AddTarget(target)
		if err != nil {
			// On startup, if we fail to add a target, we should close all the tailers we've opened so far and return.
			for _, t := range s.tailers {
				t.Stop()
			}
			return fmt.Errorf("TailSource open: %w", err)
		}
	}

	return nil
}

func (s *TailSource) Close() error {
	s.mu.Lock()
	for _, t := range s.tailers {
		t.Stop()
	}
	clear(s.tailers)
	s.mu.Unlock()

	s.closeFn()
	s.group.Wait()
	return nil
}

func (s *TailSource) Name() string {
	return "tailsource"
}

// AddTarget adds a new file to tail.
func (s *TailSource) AddTarget(target FileTailTarget) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.tailers[target.FilePath]; ok {
		return nil // already exists
	}

	tailerCtx, shutdown := context.WithCancel(s.groupCtx)
	batchQueue := make(chan *types.Log, 512)
	outputQueue := make(chan *types.LogBatch, 1)
	tailConfig := tail.Config{Follow: true, ReOpen: true}
	existingCursorPath := cursorPath(s.cursorDirectory, target.FilePath)
	fileId, position, err := readCursor(existingCursorPath)
	if err == nil {
		if logger.IsDebug() {
			logger.Debugf("TailSource: found existing cursor for file %q: %s %d", target.FilePath, fileId, position)
		}
		tailConfig.Location = &tail.SeekInfo{
			Offset:         position,
			Whence:         io.SeekStart,
			FileIdentifier: fileId,
		}
	}

	tailFile, err := tail.TailFile(target.FilePath, tailConfig)
	if err != nil {
		shutdown()
		return fmt.Errorf("addTarget create tailfile: %w", err)
	}

	parsers, err := parser.NewParsers(target.Parsers)
	if err != nil {
		shutdown()
		return fmt.Errorf("addTarget create parsers: %w", err)
	}

	tailer := &Tailer{
		tail:           tailFile,
		shutdown:       shutdown,
		database:       target.Database,
		table:          target.Table,
		logTypeParser:  getLogTypeParser(target.LogType),
		logLineParsers: parsers,
	}
	s.group.Go(func() error {
		return readLines(tailerCtx, tailer, batchQueue)
	})

	batchConfig := engine.BatchConfig{
		MaxBatchSize: 1000,
		MaxBatchWait: 1 * time.Second,
		InputQueue:   batchQueue,
		OutputQueue:  outputQueue,
		AckGenerator: s.ackGenerator,
	}
	s.group.Go(func() error {
		return engine.BatchLogs(tailerCtx, batchConfig)
	})

	worker := s.workerCreator(s.Name(), outputQueue)
	s.group.Go(worker.Run)

	s.tailers[target.FilePath] = tailer

	return nil
}

// RemoveTarget removes a file from being tailed.
// This also removes the cursor file, so this should only be called when the file is not longer expected to be tailed in the future.
func (s *TailSource) RemoveTarget(filePath string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tailer, ok := s.tailers[filePath]
	if ok {
		tailer.Stop()
		delete(s.tailers, filePath)
		cleanCursor(cursorPath(s.cursorDirectory, filePath))
	}
}

func readLines(ctx context.Context, tailer *Tailer, outputQueue chan<- *types.Log) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case line, ok := <-tailer.tail.Lines:
			if !ok {
				return fmt.Errorf("readLines: tailer closed the channel for filename %q", tailer.tail.Filename)
			}
			if line.Err != nil {
				logger.Errorf("readLines: tailer error for filename %q: %v", tailer.tail.Filename, line.Err)
				//skip
				continue
			}

			log := types.LogPool.Get(1).(*types.Log)
			log.Reset()

			err := tailer.logTypeParser(line.Text, log)
			if err != nil {
				logger.Errorf("readLines: parselog error for filename %q: %v", tailer.tail.Filename, err)
				//skip
				continue
			}

			position := line.Offset
			currentFileId := line.FileIdentifier
			log.Attributes[cursor_position] = position
			log.Attributes[cursor_file_id] = currentFileId
			log.Attributes[cursor_file_name] = tailer.tail.Filename
			log.Attributes[types.AttributeDatabaseName] = tailer.database
			log.Attributes[types.AttributeTableName] = tailer.table

			// TODO combine partial lines. Docker separates lines with newlines in the log message.

			for _, parser := range tailer.logLineParsers {
				err := parser.Parse(log)
				if err == nil {
					break // successful parse
				} else if logger.IsDebug() {
					logger.Debugf("readLines: parser error for filename %q: %v", tailer.tail.Filename, err)
				}
			}

			outputQueue <- log
		}
	}
}

func (s *TailSource) ackBatch(cursor_file_name, cursor_file_id string, cursor_position int64) {
	cursorPath := cursorPath(s.cursorDirectory, cursor_file_name)

	err := writeCursor(cursorPath, cursor_file_id, cursor_position)
	if err != nil {
		logger.Errorf("ackBatches: %s", err)
	}
}

// LogTypeParser is a function that parses a line of text from a file based on its format.
// Must assign log.Timestamp, log.ObservedTimestamp, and the types.BodyKeyMessage property of log.Body.
type LogTypeParser func(string, *types.Log) error

func getLogTypeParser(logType Type) LogTypeParser {
	switch logType {
	case LogTypeDocker:
		return parseDockerLog
	case LogTypePlain:
		return parsePlaintextLog
	default:
		return parsePlaintextLog
	}
}

func parsePlaintextLog(line string, log *types.Log) error {
	log.Timestamp = uint64(time.Now().UnixNano())
	log.ObservedTimestamp = uint64(time.Now().UnixNano())
	log.Body[types.BodyKeyMessage] = line

	return nil
}

type tailcursor struct {
	FID    string `json:"fid"`
	Cursor int64  `json:"cursor"`
}

func cursorPath(cursorDirectory string, filename string) string {
	baseName := filepath.Base(filename)
	return fmt.Sprintf("%s/%s.cursor", cursorDirectory, baseName)
}

func writeCursor(cursorPath string, file_id string, cursor int64) error {
	cursorVal := fmt.Sprintf("{\"fid\":\"%s\",\"cursor\":%d}\n", file_id, cursor)
	output, err := os.Create(cursorPath)
	if err != nil {
		return fmt.Errorf("writeCursor: failed to create/truncate cursor file %q: %w", cursorPath, err)
	}
	defer output.Close()

	_, err = output.WriteString(cursorVal)
	if err != nil {
		return fmt.Errorf("writeCursor: failed to write cursor file %q: %w", cursorPath, err)
	}

	return nil
}

func readCursor(cursorPath string) (string, int64, error) {
	file, err := os.Open(cursorPath)
	if err != nil {
		return "", 0, fmt.Errorf("readCursor: failed to open cursor file %q: %w", cursorPath, err)
	}
	defer file.Close()

	var line string
	_, err = fmt.Fscanln(file, &line)
	if err != nil {
		return "", 0, fmt.Errorf("readCursor: failed to read cursor file %q: %w", cursorPath, err)
	}
	tailcursor := tailcursor{}
	err = json.Unmarshal([]byte(line), &tailcursor)
	if err != nil {
		return "", 0, fmt.Errorf("readCursor: failed to unmarshal cursor file %q: %w", cursorPath, err)
	}

	return tailcursor.FID, tailcursor.Cursor, nil
}

func cleanCursor(cursorPath string) {
	err := os.Remove(cursorPath)
	if err != nil {
		logger.Errorf("cleanCursor: failed to remove cursor file %q: %v", cursorPath, err)
	}
}
