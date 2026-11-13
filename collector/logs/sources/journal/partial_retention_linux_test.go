//go:build linux && cgo

package journal

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/coreos/go-systemd/sdjournal"
	"github.com/stretchr/testify/require"
)

func journalFragment(stream, message string, partial bool) *sdjournal.JournalEntry {
	f := map[string]string{journaldStreamIdField: stream, sdjournal.SD_JOURNAL_FIELD_MESSAGE: message}
	if partial {
		f[journaldLineBreakField] = journald_line_break_value_line_max
	}
	return &sdjournal.JournalEntry{Fields: f}
}

func TestJournalPartialsBounded(t *testing.T) {
	j := testTailer(t)
	for range 80 {
		j.combinePartialMessages(journalFragment("stream", strings.Repeat("x", 32<<10), true))
	}
	n := j.streamPartials.RetainedBytes()
	require.LessOrEqual(t, n, 1<<20, "a missing final fragment must not grow indefinitely")
	_, pending := j.combinePartialMessages(journalFragment("stream", "discarded final", false))
	require.True(t, pending)
	msg, pending := j.combinePartialMessages(journalFragment("stream", "recovered", false))
	require.False(t, pending)
	require.Equal(t, "recovered", msg)
	j.combinePartialMessages(journalFragment("stream", "new ", true))
	msg, pending = j.combinePartialMessages(journalFragment("stream", "message", false))
	require.False(t, pending)
	require.Equal(t, "new message", msg)
}

func TestJournalPartialsExpireWhileQuiet(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		j := testTailer(t)
		for i := range 200 {
			j.combinePartialMessages(journalFragment(fmt.Sprint(i), "abandoned", true))
		}
		ctx, cancel := context.WithCancel(context.Background())
		reader := &fakeJournalReader{waitFn: func(d time.Duration) int { time.Sleep(d); return sdjournal.SD_JOURNAL_NOP }}
		j.readerFactory = func() (journalReader, error) { return reader, nil }
		done := make(chan struct{})
		go func() { defer close(done); j.ReadFromJournal(ctx) }()
		time.Sleep(6 * time.Minute)
		cancel()
		<-done
		require.Zero(t, j.streamPartials.Len(), "abandoned stream IDs must expire even when the journal is quiet")
	})
}
