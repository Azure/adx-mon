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

	// journalFile is the path to journal files. This is used for testing purposes.
	journalFiles []string

	// streamPartials maps _STREAM_ID to the accumulated partial log messages
	streamPartials map[string]string

	logger *slog.Logger
}

// ReadFromJournal follows the flow described in the examples within `man 3 sd_journal_wait`.
func (t *tailer) ReadFromJournal(ctx context.Context) {
	t.logger = logger.Logger().With(
		slog.String("source", "journal"),
		slog.String("database", t.database),
		slog.String("table", t.table),
		slog.Any("matches", t.matches),
	)

	// Must lock this goroutine (and lifecycle of sdjournal.Journal object which contains the sd_journal pointer) to the underlying OS thread.
	// man 3 sd_journal under "NOTES"
	// "given sd_journal pointer may only be used from one specific thread at all times (and it has to be the very same one during the entire lifetime of the object), but multiple, independent threads may use multiple, independent objects safely"
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	reader, err := t.openJournal(journalOpenModeStartup)
	if err != nil {
		t.logger.Error("Failed to open journal tailer. Exiting.", "err", err)
		return
	}

	for {
		select {
		case <-ctx.Done():
			reader.Close()
			return
		default:
		}

		ret, err := reader.Next()
		if err != nil {
			t.logger.Error("Failed to advance in journal", "err", err)

			reader, err = t.recoverJournal(reader)
			if err != nil {
				return // Recovery failed. Exit.
			}

			continue
		}

		if ret == 0 {
			// Wait for entries
			if err := t.waitForNewJournalEntries(ctx, reader); err != nil {
				t.logger.Error("Failed to wait for new journal entries", "err", err)

				reader, err = t.recoverJournal(reader)
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

			reader, err = t.recoverJournal(reader)
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

		t.batchQueue <- log
	}
}

func (t *tailer) openJournal(mode openmode) (*sdjournal.Journal, error) {
	var reader *sdjournal.Journal
	var err error
	if len(t.journalFiles) == 0 {
		reader, err = sdjournal.NewJournal()
	} else {
		reader, err = sdjournal.NewJournalFromFiles(t.journalFiles...)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to open journal: %w", err)
	}

	for _, match := range t.matches {
		if match == "+" {
			err := reader.AddDisjunction()
			if err != nil {
				return nil, fmt.Errorf("failed to create journal disjunction: %w", err)
			}
		}

		if err := reader.AddMatch(match); err != nil {
			return nil, fmt.Errorf("failed to add journal match %s: %w", match, err)
		}
	}

	if mode == journalOpenModeStartup {
		t.seekCursorAtStart(reader)
	} else {
		// If we are recovering, we want to start at the end of the journal to attempt to skip any corrupt entries.
		// This is a best effort attempt to skip over the corrupt entries.
		reader.SeekTail()
	}

	return reader, nil
}

// recoverJournal closes the current journal reader and opens a new one in an attempt to recover from a syscall error.
func (t *tailer) recoverJournal(reader *sdjournal.Journal) (*sdjournal.Journal, error) {
	t.logger.Info("Restarting journal tailer after error. Opening and seeking to end to attempt to skip corrupt entries.")
	reader.Close()
	t.backoff()
	reader, err := t.openJournal(journalOpenModeRecover)
	if err != nil {
		t.logger.Error("Unable to recover journal tailer, exiting. Failed to open journal tailer.", "err", err)
		return nil, err
	}
	return reader, nil
}

func (t *tailer) seekCursorAtStart(reader journalReader) {
	existingCursor, err := readCursor(t.cursorFilePath)
	if err != nil {
		t.logger.Warn(fmt.Sprintf("failed to read cursor %s", t.cursorFilePath), "err", err)
		reader.SeekHead()
	} else {
		if logger.IsDebug() {
			t.logger.Debug(fmt.Sprintf("journal: found existing cursor in %q: %s", t.cursorFilePath, existingCursor))
		}

		err := reader.SeekCursor(existingCursor)
		if err != nil {
			t.logger.Warn(fmt.Sprintf("failed to seek to cursor %s", existingCursor), "err", err)
			reader.SeekHead()
		} else {
			// After seeking to the cursor, we need to test the cursor to see if we went to the right place.
			// If not, we will end up somewhere else (or potentially in a spot where we will never get new entries) because of time skew or something else.
			// This can lead to cases where we _never_ get new entries, so we need to test the cursor.
			// We must call next() prior to testing the cursor to actually set a position in the journal.
			// Usage in man 3 SD_JOURNAL_GET_CURSOR
			_, err = reader.Next()
			if err != nil {
				t.logger.Warn(fmt.Sprintf("failed to advance journal after seeking to cursor %s, seeking to head", existingCursor), "err", err)
				reader.SeekHead()
				return
			}

			err = reader.TestCursor(existingCursor)
			if err != nil {
				t.logger.Warn(fmt.Sprintf("failed to test cursor %s, seeking to head", existingCursor), "err", err)
				reader.SeekHead()
			}
		}
	}
}

func (t *tailer) backoff() {
	time.Sleep(waittime)
}

func (t *tailer) waitForNewJournalEntries(ctx context.Context, reader journalReader) error {
	status := reader.Wait(waittime)
	switch status {
	case sdjournal.SD_JOURNAL_NOP, sdjournal.SD_JOURNAL_APPEND, sdjournal.SD_JOURNAL_INVALIDATE:
		return nil
	default:
		if status < 0 {
			return fmt.Errorf("waitForNewJournalEntries error status: %v", status)
		}
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
