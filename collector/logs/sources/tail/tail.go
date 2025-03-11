package tail

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Azure/adx-mon/collector/logs/engine"
	"github.com/Azure/adx-mon/collector/logs/sources/tail/sourceparse"
	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/pkg/logger"
)

const (
	cursor_position  = "adxmon_cursor_position"
	cursor_file_id   = "adxmon_cursor_file_id"
	cursor_file_name = "adxmon_cursor_file_name"
)

var (
	noopAck          = func() {}
	noopAckGenerator = func(*types.Log) func() { return noopAck }
)

// FileTailTarget describes a file to tail, how to parse it, and the destination for the parsed logs.
// It is used in the list of StaticTargets in TailSourceConfig, or used in AddTarget.
// NOTE: If new fields are added that change during runtime (e.g. from pod metadata changing) that are sent
// via UpdateChan, update isTargetChanged to take this new field into account.
type FileTailTarget struct {
	// FilePath is the file to tail.
	FilePath string

	// LogType is the format of the log file. e.g. docker for logs written by the docker json driver.
	// This provides a mechanism to combine split lines, parse timestamps (if present), and extract a message field.
	// Defaults to plain.
	LogType sourceparse.Type

	// The destination database name. Populated into the databasename attribute of the log that can be overwritten by transforms.
	Database string

	// The destination table name. Populated into the tablename attribute of the log that can be overwritten by transforms.
	Table string

	// Parsers is a list of parsers names to apply to each line of the log file to attempt to extract fields from the message body.
	// These are run sequentially until one succeeds, or until all have been tried.
	// These are converted into parser.ParserType.
	Parsers []string

	// Resources is a map of additional resource k/v pairs to add to each log.
	Resources map[string]interface{}
}

// TailSourceConfig configures TailSource.
type TailSourceConfig struct {
	StaticTargets   []FileTailTarget
	CursorDirectory string
	WorkerCreator   engine.WorkerCreatorFunc
	// TODO mkeesey - TailSource should not need to manage poddiscovery service lifecycle.
	// However, creation of TailSource happens with a create method to access store, making this
	// wiring difficult.
	PodDiscoveryOpts *PodDiscoveryOpts
}

// TailSource implements the types.Source interface for tailing files.
type TailSource struct {
	staticTargets   []FileTailTarget
	cursorDirectory string
	workerCreator   engine.WorkerCreatorFunc

	ackGenerator func(*types.Log) func()
	tailers      map[string]*Tailer
	// protects tailers
	mu sync.RWMutex

	podDiscovery *PodDiscovery
}

func NewTailSource(config TailSourceConfig) (*TailSource, error) {
	ts := &TailSource{
		staticTargets:   config.StaticTargets,
		cursorDirectory: config.CursorDirectory,
		workerCreator:   config.WorkerCreator,
	}

	if config.PodDiscoveryOpts != nil {
		ts.podDiscovery = NewPodDiscovery(*config.PodDiscoveryOpts, ts)
	}

	return ts, nil
}

func (s *TailSource) Open(ctx context.Context) error {
	s.ackGenerator = noopAckGenerator
	if s.cursorDirectory != "" {
		s.ackGenerator = func(log *types.Log) func() {
			cursorFileName := types.StringOrEmpty(log.GetAttributeValue(cursor_file_name))
			cursorFileId := types.StringOrEmpty(log.GetAttributeValue(cursor_file_id))
			cursorPositionVal, ok := log.GetAttributeValue(cursor_position)

			if !ok || cursorFileName == "" || cursorFileId == "" {
				return noopAck
			}
			cursorPosition, ok := cursorPositionVal.(int64)
			if !ok {
				return noopAck
			}

			return func() {
				s.ackBatch(cursorFileName, cursorFileId, cursorPosition)
			}
		}
	}

	s.tailers = map[string]*Tailer{}
	for _, target := range s.staticTargets {
		target := target

		err := s.AddTarget(target, nil)
		if err != nil {
			// On startup, if we fail to add a target, we should close all the tailers we've opened so far and return.
			s.Close()
			return fmt.Errorf("TailSource open: %w", err)
		}
	}

	if s.podDiscovery != nil {
		err := s.podDiscovery.Open(ctx)
		if err != nil {
			// On startup, if we fail to open the pod discovery, we should close all the tailers we've opened so far and return.
			s.Close()
			return fmt.Errorf("TailSource open: %w", err)
		}
	}

	return nil
}

func (s *TailSource) Close() error {
	if s.podDiscovery != nil {
		s.podDiscovery.Close()
	}

	s.mu.Lock()
	for _, t := range s.tailers {
		t.Stop()
	}
	for _, t := range s.tailers {
		t.Wait()
	}
	clear(s.tailers)
	s.mu.Unlock()
	return nil
}

func (s *TailSource) Name() string {
	return "tailsource"
}

// AddTarget adds a new file to tail.
// updateChan is an optional channel to provide updated FileTailTarget metadata during runtime.
// Does not support updating FilePath or LogType.
func (s *TailSource) AddTarget(target FileTailTarget, updateChan <-chan FileTailTarget) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.tailers[target.FilePath]; ok {
		return nil // already exists
	}

	tailerConfig := TailerConfig{
		Target:          target,
		UpdateChan:      updateChan,
		AckGenerator:    s.ackGenerator,
		WorkerCreator:   s.workerCreator,
		CursorDirectory: s.cursorDirectory,
		WorkerName:      s.Name(),
	}

	tailer, err := StartTailing(tailerConfig)
	if err != nil {
		return fmt.Errorf("AddTarget: %w", err)
	}

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
		tailer.Wait()
		delete(s.tailers, filePath)
		cleanCursor(cursorPath(s.cursorDirectory, filePath))
	}
}

func (s *TailSource) ackBatch(cursor_file_name, cursor_file_id string, cursor_position int64) {
	cursorPath := cursorPath(s.cursorDirectory, cursor_file_name)

	err := writeCursor(cursorPath, cursor_file_id, cursor_position)
	if err != nil {
		logger.Errorf("ackBatches: %s", err)
	}
}

type tailcursor struct {
	FID    string `json:"fid"`
	Cursor int64  `json:"cursor"`
}

func cursorPath(cursorDirectory string, filename string) string {
	baseName := strings.ReplaceAll(filename, string(filepath.Separator), "_")
	cursorFileName := fmt.Sprintf("%s.cursor", baseName)
	return filepath.Join(cursorDirectory, cursorFileName)
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
