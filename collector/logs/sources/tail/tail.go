package tail

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Azure/adx-mon/collector/logs/engine"
	"github.com/Azure/adx-mon/collector/logs/sources/tail/sourceparse"
	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/pkg/atomicfile"
	"github.com/Azure/adx-mon/pkg/ingestpolicy"
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

// ErrTargetExists is returned by AddTarget when the file is already tailed or its previous tailer is still being removed.
var ErrTargetExists = errors.New("target already exists")

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
	Resources map[string]any
}

// TailSourceConfig configures TailSource.
type TailSourceConfig struct {
	StaticTargets     []FileTailTarget
	CursorDirectory   string
	WorkerCreator     engine.WorkerCreatorFunc
	RealtimePolicy    *ingestpolicy.Policy
	RealtimeBatchWait time.Duration
	// TODO mkeesey - TailSource should not need to manage poddiscovery service lifecycle.
	// However, creation of TailSource happens with a create method to access store, making this
	// wiring difficult.
	PodDiscoveryOpts *PodDiscoveryOpts
}

// TailSource implements the types.Source interface for tailing files.
type TailSource struct {
	staticTargets     []FileTailTarget
	cursorDirectory   string
	workerCreator     engine.WorkerCreatorFunc
	realtimePolicy    *ingestpolicy.Policy
	realtimeBatchWait time.Duration

	ackGenerator func(*types.Log) func()
	tailers      map[string]*Tailer
	// file paths whose tailers have been stopped but are still draining
	removing map[string]struct{}
	// protects tailers and removing
	mu sync.RWMutex

	podDiscovery *PodDiscovery
}

func NewTailSource(config TailSourceConfig) (*TailSource, error) {
	ts := &TailSource{
		staticTargets:     config.StaticTargets,
		cursorDirectory:   config.CursorDirectory,
		workerCreator:     config.WorkerCreator,
		realtimePolicy:    config.RealtimePolicy,
		realtimeBatchWait: config.RealtimeBatchWait,
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
	s.removing = map[string]struct{}{}
	for _, target := range s.staticTargets {

		err := s.AddTarget(target, nil)
		if errors.Is(err, ErrTargetExists) {
			logger.Warnf("TailSource open: ignoring duplicate static target %q", target.FilePath)
			continue
		}
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
// Returns ErrTargetExists if the file is already tailed or still being removed; updateChan is not used in that case.
func (s *TailSource) AddTarget(target FileTailTarget, updateChan <-chan FileTailTarget) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.tailers[target.FilePath]; ok {
		return fmt.Errorf("AddTarget %q: %w", target.FilePath, ErrTargetExists)
	}
	if _, ok := s.removing[target.FilePath]; ok {
		return fmt.Errorf("AddTarget %q: previous tailer is still being removed: %w", target.FilePath, ErrTargetExists)
	}

	tailerConfig := TailerConfig{
		Target:            target,
		UpdateChan:        updateChan,
		AckGenerator:      s.ackGenerator,
		WorkerCreator:     s.workerCreator,
		RealtimePolicy:    s.realtimePolicy,
		RealtimeBatchWait: s.realtimeBatchWait,
		CursorDirectory:   s.cursorDirectory,
		WorkerName:        s.Name(),
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
// Waiting for the tailer to drain happens without holding the lock, so other targets can be added and removed meanwhile.
func (s *TailSource) RemoveTarget(filePath string) {
	s.mu.Lock()
	tailer, ok := s.tailers[filePath]
	if !ok {
		s.mu.Unlock()
		return
	}
	delete(s.tailers, filePath)
	s.removing[filePath] = struct{}{}
	tailer.Stop()
	s.mu.Unlock()

	tailer.Wait()
	cleanCursor(cursorPath(s.cursorDirectory, filePath))

	s.mu.Lock()
	delete(s.removing, filePath)
	s.mu.Unlock()
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
	if err := atomicfile.WriteFile(cursorPath, []byte(cursorVal), 0644); err != nil {
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
