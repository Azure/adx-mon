//go:build linux && cgo

package journal

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"time"

	"github.com/Azure/adx-mon/collector/logs/transforms/parser"
	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/coreos/go-systemd/sdjournal"
)

const (
	waittime = 500 * time.Millisecond

	journaldLineBreakField = "_LINE_BREAK"
	journaldStreamIdField  = "_STREAM_ID"

	// Indicates the log message was split due to the line-max configuration
	// By default, is 48k but is configured with LineMax in journald.conf
	journald_line_break_value_line_max = "line-max"
)

type openmode string

// constants for the mode we're opening the journal in
const (
	// journalOpenModeStartup is the initial path we use for opening. Attempts to seek to the last read entry, or to the head if no cursor is found.
	journalOpenModeStartup openmode = "startup"

	// journalOpenModeRecover is the recovery path. Attempts to seek to the bottom of the journal to try to skip whatever corrupt entries may be causing errors.
	journalOpenModeRecover openmode = "recover"
)

type tailer struct {
	matches        []string
	database       string
	table          string
	journalFields  []string
	cursorFilePath string
	logLineParsers []parser.Parser
	batchQueue     chan<- *types.Log

	// journalPath is an optional directory to read journal files from instead
	// of the local system journal.
	journalPath string

	// streamPartials maps _STREAM_ID to the accumulated partial log messages
	streamPartials map[string]string

	logger *slog.Logger

	// readerFactory, when provided, runs on the same locked owner thread as
	// every other reader operation. It permits isolated error-path tests.
	readerFactory func() (journalReader, error)
}

// ReadFromJournal follows the flow described in the examples within `man 3 sd_journal_wait`.
func (t *tailer) ReadFromJournal(ctx context.Context) {
	t.logger = logger.Logger().With(
		slog.String("source", "journal"),
		slog.String("database", t.database),
		slog.String("table", t.table),
		slog.Any("matches", t.matches),
		slog.String("journal_path", t.journalPath),
	)

	// Must lock this goroutine (and lifecycle of sdjournal.Journal object which contains the sd_journal pointer) to the underlying OS thread.
	// man 3 sd_journal under "NOTES"
	// "given sd_journal pointer may only be used from one specific thread at all times (and it has to be the very same one during the entire lifetime of the object), but multiple, independent threads may use multiple, independent objects safely"
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	reader, err := t.openJournal(journalOpenModeStartup)
	if err != nil {
		t.logger.Error("Failed to open journal tailer. Exiting.", "operation", "open", "outcome", "exit", "err", err)
		return
	}
	// Capture the variable, not the original handle: recovery replaces it and
	// owns closing the previous reader, including when reopening fails.
	defer func() {
		if reader != nil {
			t.closeJournal(reader)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		ret, err := reader.Next()
		if err != nil {
			t.logger.Error("Failed to advance in journal", "err", err)

			reader, err = t.recoverJournal(ctx, reader, "next", err)
			if err != nil {
				return // Recovery failed. Exit.
			}

			continue
		}

		if ret == 0 {
			// Wait for entries
			if err := t.waitForNewJournalEntries(ctx, reader); err != nil {
				if ctx.Err() != nil {
					return
				}
				t.logger.Error("Failed to wait for new journal entries", "err", err)

				reader, err = t.recoverJournal(ctx, reader, "wait", err)
				if err != nil {
					return // Recovery failed. Exit.
				}

				continue
			}
			continue
		}

		entry, err := reader.GetEntry()
		if err != nil {
			t.logger.Error("Failed to get journal entry", "err", err)

			reader, err = t.recoverJournal(ctx, reader, "entry", err)
			if err != nil {
				return // Recovery failed. Exit.
			}

			continue
		}

		message, isPartial := t.combinePartialMessages(entry)
		if isPartial {
			// We are waiting for more messages to combine
			continue
		}

		log := types.LogPool.Get(1).(*types.Log)
		log.Reset()
		log.SetTimestamp(uint64(entry.RealtimeTimestamp) * 1000) // microseconds -> nanoseconds
		log.SetObservedTimestamp(uint64(time.Now().UnixNano()))
		log.SetAttributeValue(types.AttributeDatabaseName, t.database)
		log.SetAttributeValue(types.AttributeTableName, t.table)

		parser.ExecuteParsers(t.logLineParsers, log, message, "journald")

		// Write after parsing to ensure these values are always set to values we need for acking.
		log.SetAttributeValue(journald_cursor_attribute, entry.Cursor)
		log.SetAttributeValue(journald_cursor_filename_attribute, t.cursorFilePath)

		for _, field := range t.journalFields {
			if value, ok := entry.Fields[field]; ok {
				log.SetResourceValue(field, value)
			}
		}

		select {
		case t.batchQueue <- log:
		case <-ctx.Done():
			log.Reset()
			types.LogPool.Put(log)
			return
		}
	}
}

func (t *tailer) openJournal(mode openmode) (_ journalReader, err error) {
	var reader journalReader
	if t.readerFactory != nil {
		reader, err = t.readerFactory()
	} else if t.journalPath != "" {
		reader, err = sdjournal.NewJournalFromDir(t.journalPath)
	} else {
		reader, err = sdjournal.NewJournal()
	}

	if err != nil {
		return nil, fmt.Errorf("failed to open journal: %w", err)
	}
	defer func() {
		if err != nil {
			t.closeJournal(reader)
		}
	}()

	if err := applyMatches(reader, t.matches); err != nil {
		return nil, err
	}

	var position string
	if mode == journalOpenModeStartup {
		position, err = t.seekCursorAtStart(reader)
	} else {
		position, err = seekLiveTail(reader)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to position journal for %s: %w", mode, err)
	}

	t.logger.Info("Journal reader opened and positioned", "mode", mode, "position", position)
	return reader, nil
}

// seekLiveTail selects the final matching entry before following forward.
// SeekTail leaves a logical position after all entries, not a current entry.
// Previous resolves that position onto the last matching entry without emitting
// it, allowing Next to follow subsequent appends without replaying that entry.
// See sd_journal_seek_head(3) for the lazy seek semantics.
func seekLiveTail(reader journalReader) (string, error) {
	if err := reader.SeekTail(); err != nil {
		return "", fmt.Errorf("failed to seek to journal tail: %w", err)
	}
	count, err := reader.Previous()
	if err != nil {
		return "", fmt.Errorf("failed to anchor journal tail: %w", err)
	}
	if count == 0 {
		// Matches are already applied. A busy journal may still have no entry
		// for this target; leave the first future match for the normal loop.
		if err := reader.SeekHead(); err != nil {
			return "", fmt.Errorf("failed to seek to journal head after empty tail: %w", err)
		}
		return "empty_head", nil
	}
	return "live_tail", nil
}

func (t *tailer) closeJournal(reader journalReader) {
	if err := reader.Close(); err != nil {
		t.logger.Error("Failed to close journal reader", "operation", "close", "err", err)
	}
}

func applyMatches(reader journalReader, matches []string) error {
	for _, match := range matches {
		if match == "+" {
			if err := reader.AddDisjunction(); err != nil {
				return fmt.Errorf("failed to create journal disjunction: %w", err)
			}

			continue
		}

		if err := reader.AddMatch(match); err != nil {
			return fmt.Errorf("failed to add journal match %s: %w", match, err)
		}
	}

	return nil
}

// recoverJournal closes the current journal reader and opens a new one in an attempt to recover from a syscall error.
func (t *tailer) recoverJournal(ctx context.Context, reader journalReader, operation string, cause error) (journalReader, error) {
	t.logger.Warn("Restarting journal tailer after error; skipping to live entries", "operation", operation,
		"err", cause, "recovery_policy", "skip_to_live")
	t.closeJournal(reader)
	if err := t.backoff(ctx); err != nil {
		return nil, err
	}
	reader, err := t.openJournal(journalOpenModeRecover)
	if err != nil {
		t.logger.Error("Unable to recover journal tailer, exiting. Failed to open journal tailer.", "operation", "reopen", "outcome", "exit", "err", err)
		return nil, err
	}
	discarded := len(t.streamPartials)
	clear(t.streamPartials)
	t.logger.Info("Journal recovery created a coverage gap", "recovery_policy", "skip_to_live", "discarded_partial_streams", discarded)
	return reader, nil
}

// seekCursorAtStart leaves either the verified acknowledged entry current or a
// head seek pending. The normal loop, not diagnostics, reads the first replay entry.
func (t *tailer) seekCursorAtStart(reader journalReader) (string, error) {
	seekHead := func(reason error) (string, error) {
		if err := reader.SeekHead(); err != nil {
			if reason != nil {
				return "", fmt.Errorf("failed to seek to journal head at startup: %w; fallback reason: %w", err, reason)
			}
			return "", fmt.Errorf("failed to seek to journal head at startup: %w", err)
		}
		return "head", nil
	}
	existingCursor, err := readCursor(t.cursorFilePath)
	if err != nil {
		t.logger.Warn("Failed to read journal cursor; replaying retained history", "cursor_file", t.cursorFilePath, "err", err)
		return seekHead(err)
	}
	if existingCursor == "" {
		t.logger.Warn("Journal cursor is empty; replaying retained history", "cursor_file", t.cursorFilePath)
		return seekHead(nil)
	}
	t.logger.Debug("Found existing journal cursor", "cursor_file", t.cursorFilePath, "cursor", existingCursor)
	if err := reader.SeekCursor(existingCursor); err != nil {
		t.logger.Warn("Failed to seek to journal cursor; replaying retained history", "cursor", existingCursor, "err", err)
		return seekHead(fmt.Errorf("failed to seek to startup cursor: %w", err))
	}
	// Seeking is lazy: Next must select an entry before TestCursor is valid.
	count, err := reader.Next()
	if err != nil {
		t.logger.Warn("Failed to advance journal after cursor seek; replaying retained history", "cursor", existingCursor, "err", err)
		return seekHead(fmt.Errorf("failed to advance after startup cursor seek: %w", err))
	}
	if count == 0 {
		t.logger.Warn("Journal cursor seek selected no entry; replaying retained history", "cursor", existingCursor)
		return seekHead(nil)
	}
	if err := reader.TestCursor(existingCursor); err != nil {
		// ErrNoTestCursor is a mismatch, not proof that the entry expired.
		t.logger.Warn("Journal cursor verification failed; replaying retained history", "cursor", existingCursor, "err", err)
		return seekHead(fmt.Errorf("failed to verify startup cursor: %w", err))
	}
	return "cursor", nil
}

func (t *tailer) backoff(ctx context.Context) error {
	timer := time.NewTimer(waittime)
	defer timer.Stop()
	select {
	case <-timer.C:
		return ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (t *tailer) waitForNewJournalEntries(ctx context.Context, reader journalReader) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	status := reader.Wait(waittime)
	if status < 0 {
		return fmt.Errorf("waitForNewJournalEntries error status: %v", status)
	}
	return nil
}

// combinePartialMessages combines partial log messages that have been split by journald due to the line-max configuration.
func (j *tailer) combinePartialMessages(entry *sdjournal.JournalEntry) (string, bool) {
	isPartial := entry.Fields[journaldLineBreakField] == journald_line_break_value_line_max

	currentLogMsg := entry.Fields[sdjournal.SD_JOURNAL_FIELD_MESSAGE]

	streamID := entry.Fields[journaldStreamIdField]
	previousLog, hasPreviousLog := j.streamPartials[streamID]
	if hasPreviousLog {
		currentLogMsg = previousLog + currentLogMsg
	}

	if isPartial {
		j.streamPartials[streamID] = currentLogMsg
		return "", true
	} else if hasPreviousLog {
		// We have a complete message. Clear out the stored partial.
		delete(j.streamPartials, streamID)
	}
	return currentLogMsg, false
}
