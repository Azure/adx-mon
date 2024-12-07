//go:build linux && cgo

package journal

import (
	"context"
	"fmt"
	"time"

	"github.com/Azure/adx-mon/collector/logs/transforms/parser"
	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/coreos/go-systemd/sdjournal"
)

const (
	waittime = 100 * time.Millisecond

	journald_line_break_field = "_LINE_BREAK"
	journald_stream_id_field  = "_STREAM_ID"

	// Indicates the log message was split due to the line-max configuration
	// By default, is 48k but is configured with LineMax in journald.conf
	journald_line_break_value_line_max = "line-max"
)

type tailer struct {
	reader         journalReader
	database       string
	table          string
	cursorFilePath string
	logLineParsers []parser.Parser
	batchQueue     chan<- *types.Log

	// streamPartials maps _STREAM_ID to the accumulated partial log messages
	streamPartials map[string]string
}

// readFromJournal follows the flow described in the examples within `man 3 sd_journal_wait`.
func (t *tailer) readFromJournal(ctx context.Context) {
	t.seekCursorAtStart()

	for {
		select {
		case <-ctx.Done():
			t.reader.Close()
			return
		default:
		}

		ret, err := t.reader.Next()
		if err != nil {
			// Unclear how to handle these errors. The implementation of sd_journald_next returns errors
			// when attempting to continue iteration. Suspect these are i/o related if there are issues.
			logger.Errorf("failed to advance in journal: %v", err)
			t.backoff() // TODO: recreate reader?
			continue
		}

		if ret == 0 {
			// Wait for entries
			if err := t.waitForNewJournalEntries(ctx); err != nil {
				logger.Errorf("failed to wait for new journal entries: %v", err)
				t.backoff() // TODO: recreate reader?
				continue
			}
			continue
		}

		entry, err := t.reader.GetEntry()
		if err != nil {
			logger.Errorf("failed to get journal entry: %v", err)
			t.backoff()
			continue
		}

		message, isPartial := t.combinePartialMessages(entry)
		if isPartial {
			// We are waiting for more messages to combine
			continue
		}

		log := types.LogPool.Get(1).(*types.Log)
		log.Reset()
		log.Timestamp = uint64(entry.RealtimeTimestamp) * 1000 // microseconds -> nanoseconds
		log.ObservedTimestamp = uint64(time.Now().UnixNano())
		log.Attributes[types.AttributeDatabaseName] = t.database
		log.Attributes[types.AttributeTableName] = t.table

		successfulParse := false
		for _, logLineParser := range t.logLineParsers {
			err := logLineParser.Parse(log, message)
			if err == nil {
				successfulParse = true
				break
			} else if logger.IsDebug() {
				logger.Debugf("readFromJournal: parser error for journald input %v", err)
			}
		}

		if !successfulParse {
			// Unsuccessful parse, add the raw message
			log.Body[types.BodyKeyMessage] = message
		}

		// Write after parsing to ensure these values are always set to values we need for acking.
		log.Attributes[journald_cursor_attribute] = entry.Cursor
		log.Attributes[journald_cursor_filename_attribute] = t.cursorFilePath

		t.batchQueue <- log
	}
}

func (t *tailer) seekCursorAtStart() {
	existingCursor, err := readCursor(t.cursorFilePath)
	if err != nil {
		logger.Warnf("failed to read cursor %s: %v", t.cursorFilePath, err)
		t.reader.SeekHead()
	} else {
		if logger.IsDebug() {
			logger.Debugf("journal: found existing cursor in %q: %s", t.cursorFilePath, existingCursor)
		}

		err := t.reader.SeekCursor(existingCursor)
		if err != nil {
			logger.Warnf("failed to seek to cursor %s: %v", existingCursor, err)
			t.reader.SeekHead()
		} else {
			// Cursor points at the last read entry, so skip it
			t.reader.NextSkip(1)
		}
	}
}

func (t *tailer) backoff() {
	time.Sleep(waittime)
}

func (t *tailer) waitForNewJournalEntries(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		status := t.reader.Wait(waittime)
		switch status {
		case sdjournal.SD_JOURNAL_NOP:
			continue
		case sdjournal.SD_JOURNAL_APPEND, sdjournal.SD_JOURNAL_INVALIDATE:
			return nil
		default:
			if status < 0 {
				return fmt.Errorf("waitForNewJournalEntries error status: %v", status)
			}
		}
	}
}

// combinePartialMessages combines partial log messages that have been split by journald due to the line-max configuration.
func (j *tailer) combinePartialMessages(entry *sdjournal.JournalEntry) (string, bool) {
	isPartial := entry.Fields[journald_line_break_field] == journald_line_break_value_line_max

	currentLogMsg := entry.Fields[sdjournal.SD_JOURNAL_FIELD_MESSAGE]

	streamID := entry.Fields[journald_stream_id_field]
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
