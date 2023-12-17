package tail

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/tenebris-tech/tail"
	"golang.org/x/sync/errgroup"
)

const (
	cursor_position  = "cursor_position"
	cursor_file_id   = "cursor_file_id"
	cursor_file_name = "cursor_file_name"

	// Log types for parsing
	LogTypeDocker = "docker"
)

var (
	noopAckGenerator = func(*types.Log) func() { return func() {} }
)

type FileTailTarget struct {
	FilePath string
	LogType  string
}

type TailSourceConfig struct {
	StaticTargets   []FileTailTarget
	CursorDirectory string
}

type TailSource struct {
	staticTargets   []FileTailTarget
	cursorDirectory string
	outputQueue     chan *types.LogBatch

	closeFn context.CancelFunc
	group   *errgroup.Group
	tailers []*tail.Tail
}

func NewTailSource(config TailSourceConfig) (*TailSource, error) {
	return &TailSource{
		staticTargets:   config.StaticTargets,
		outputQueue:     make(chan *types.LogBatch, 1),
		cursorDirectory: config.CursorDirectory,
	}, nil
}

func (s *TailSource) Open(ctx context.Context) error {
	ctx, closeFn := context.WithCancel(ctx)
	s.closeFn = closeFn

	group, ctx := errgroup.WithContext(ctx)
	s.group = group

	ackGenerator := noopAckGenerator
	if s.cursorDirectory != "" {
		ackGenerator = func(log *types.Log) func() {
			cursorFileName := log.Attributes[cursor_file_name].(string)
			cursorFileId := log.Attributes[cursor_file_id].(string)
			cursorPosition := log.Attributes[cursor_position].(int64)
			return func() {
				s.ackBatch(cursorFileName, cursorFileId, cursorPosition)
			}
		}
	}

	s.tailers = make([]*tail.Tail, 0, len(s.staticTargets))
	for _, target := range s.staticTargets {
		target := target

		batchQueue := make(chan *types.Log, 512)
		batchConfig := types.BatchConfig{
			MaxBatchSize: 1000,
			MaxBatchWait: 1 * time.Second,
			InputQueue:   batchQueue,
			OutputQueue:  s.outputQueue,
			AckGenerator: ackGenerator,
		}
		group.Go(func() error {
			return types.BatchLogs(ctx, batchConfig)
		})

		tailConfig := tail.Config{Follow: true, ReOpen: true}
		existingCursorPath := cursorPath(s.cursorDirectory, target.FilePath)
		fileId, position, err := readCursor(existingCursorPath)
		if err == nil {
			logger.Debugf("TailSource: found existing cursor for file %q: %s %d", target.FilePath, fileId, position)
			tailConfig.Location = &tail.SeekInfo{
				Offset:         position,
				Whence:         io.SeekStart,
				FileIdentifier: fileId,
			}
		}

		tailer, err := tail.TailFile(target.FilePath, tailConfig)
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

			position := line.Offset
			currentFileId := line.FileIdentifier
			log.Attributes[cursor_position] = position
			log.Attributes[cursor_file_id] = currentFileId
			log.Attributes[cursor_file_name] = tailer.Filename

			// TODO combine partial lines. Docker separates lines with newlines in the log message.
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

func parsePlaintextLog(line string, log *types.Log) error {
	log.Timestamp = uint64(time.Now().UnixNano())
	log.ObservedTimestamp = uint64(time.Now().UnixNano())
	log.Body["message"] = line

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
