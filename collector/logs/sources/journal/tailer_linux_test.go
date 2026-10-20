//go:build linux && cgo

package journal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/coreos/go-systemd/sdjournal"
	"github.com/stretchr/testify/require"
)

type fakeJournalReader struct {
	calls          []string
	matchErr       error
	disjunctionErr error
	seekTailErr    error
	previousErr    error
	seekHeadErr    error
	seekCursorErr  error
	testCursorErr  error
	closeErr       error
	previousCount  uint64
	nextFn         func() (uint64, error)
	entryFn        func() (*sdjournal.JournalEntry, error)
	waitFn         func(time.Duration) int
	closeCount     int
}

func (f *fakeJournalReader) AddMatch(match string) error {
	f.calls = append(f.calls, "match:"+match)
	return f.matchErr
}

func (f *fakeJournalReader) AddDisjunction() error {
	f.calls = append(f.calls, "or")
	return f.disjunctionErr
}

func (f *fakeJournalReader) Next() (uint64, error) {
	f.calls = append(f.calls, "next")
	if f.nextFn != nil {
		return f.nextFn()
	}
	return 0, nil
}

func (f *fakeJournalReader) GetEntry() (*sdjournal.JournalEntry, error) {
	f.calls = append(f.calls, "entry")
	return f.entryFn()
}

func (f *fakeJournalReader) NextSkip(skip uint64) (uint64, error) { return 0, nil }

func (f *fakeJournalReader) SeekHead() error {
	f.calls = append(f.calls, "head")
	return f.seekHeadErr
}

func (f *fakeJournalReader) SeekTail() error {
	f.calls = append(f.calls, "tail")
	return f.seekTailErr
}

func (f *fakeJournalReader) Previous() (uint64, error) {
	f.calls = append(f.calls, "previous")
	return f.previousCount, f.previousErr
}

func (f *fakeJournalReader) SeekCursor(cursor string) error {
	f.calls = append(f.calls, "cursor:"+cursor)
	return f.seekCursorErr
}

func (f *fakeJournalReader) TestCursor(cursor string) error {
	f.calls = append(f.calls, "test:"+cursor)
	return f.testCursorErr
}

func (f *fakeJournalReader) Wait(timeout time.Duration) int {
	f.calls = append(f.calls, "wait")
	if f.waitFn != nil {
		return f.waitFn(timeout)
	}
	return sdjournal.SD_JOURNAL_NOP
}

func (f *fakeJournalReader) Close() error {
	f.calls = append(f.calls, "close")
	f.closeCount++
	return f.closeErr
}

func testTailer(t *testing.T) *tailer {
	t.Helper()
	return &tailer{
		database: "testdb", table: "testtable",
		cursorFilePath: filepath.Join(t.TempDir(), "cursor"),
		streamPartials: make(map[string]string),
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestSeekLiveTail(t *testing.T) {
	cause := errors.New("position failure")
	for _, tc := range []struct {
		name              string
		reader            fakeJournalReader
		position, wantErr string
		calls             []string
	}{
		{"nonempty", fakeJournalReader{previousCount: 1}, "live_tail", "", []string{"tail", "previous"}},
		{"empty", fakeJournalReader{}, "empty_head", "", []string{"tail", "previous", "head"}},
		{"tail error", fakeJournalReader{seekTailErr: cause}, "", "failed to seek to journal tail", []string{"tail"}},
		{"anchor error", fakeJournalReader{previousErr: cause}, "", "failed to anchor journal tail", []string{"tail", "previous"}},
		{"empty head error", fakeJournalReader{seekHeadErr: cause}, "", "failed to seek to journal head after empty tail", []string{"tail", "previous", "head"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			position, err := seekLiveTail(&tc.reader)
			require.Equal(t, tc.position, position)
			if tc.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.wantErr)
				require.ErrorIs(t, err, cause)
			}
			require.Equal(t, tc.calls, tc.reader.calls)
		})
	}
}

func TestStartupPositioning(t *testing.T) {
	positionErr := errors.New("cursor positioning failure")
	headErr := errors.New("head fallback failure")
	selectEntry := func() (uint64, error) { return 1, nil }
	for _, tc := range []struct {
		name, contents string
		reader         fakeJournalReader
		calls          []string
		wantPosition   string
		// cause is the positioning error that must remain in the error chain
		// when the head fallback also fails.
		cause error
	}{
		{"missing checkpoint", "", fakeJournalReader{}, []string{"head"}, "head", nil},
		{"empty cursor", `{"cursor":""}`, fakeJournalReader{}, []string{"head"}, "head", nil},
		{"seek failure", `{"cursor":"saved"}`, fakeJournalReader{seekCursorErr: positionErr}, []string{"cursor:saved", "head"}, "head", positionErr},
		{"next failure", `{"cursor":"saved"}`, fakeJournalReader{nextFn: func() (uint64, error) { return 0, positionErr }}, []string{"cursor:saved", "next", "head"}, "head", positionErr},
		{"next zero", `{"cursor":"saved"}`, fakeJournalReader{}, []string{"cursor:saved", "next", "head"}, "head", nil},
		{"cursor mismatch", `{"cursor":"saved"}`, fakeJournalReader{nextFn: selectEntry, testCursorErr: sdjournal.ErrNoTestCursor}, []string{"cursor:saved", "next", "test:saved", "head"}, "head", sdjournal.ErrNoTestCursor},
		{"test error", `{"cursor":"saved"}`, fakeJournalReader{nextFn: selectEntry, testCursorErr: positionErr}, []string{"cursor:saved", "next", "test:saved", "head"}, "head", positionErr},
		{"valid cursor", `{"cursor":"saved"}`, fakeJournalReader{nextFn: selectEntry}, []string{"cursor:saved", "next", "test:saved"}, "cursor", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, headFails := range []bool{false, true} {
				if headFails && tc.wantPosition != "head" {
					continue
				}
				tailer := testTailer(t)
				if tc.contents != "" {
					require.NoError(t, os.WriteFile(tailer.cursorFilePath, []byte(tc.contents), 0600))
				}
				reader := tc.reader
				if headFails {
					reader.seekHeadErr = headErr
				}
				position, err := tailer.seekCursorAtStart(&reader)
				if headFails {
					require.ErrorIs(t, err, headErr)
					require.ErrorContains(t, err, "failed to seek to journal head at startup")
					if tc.cause != nil {
						require.ErrorIs(t, err, tc.cause)
					}
				} else {
					require.NoError(t, err)
					require.Equal(t, tc.wantPosition, position)
				}
				require.Equal(t, tc.calls, reader.calls)
			}
		})
	}
}

func TestOpenJournalFailureClosesReader(t *testing.T) {
	cause := errors.New("operation failure")
	for _, tc := range []struct {
		name   string
		mode   openmode
		reader fakeJournalReader
	}{
		{"matches", journalOpenModeRecover, fakeJournalReader{matchErr: cause}},
		{"recover positioning", journalOpenModeRecover, fakeJournalReader{seekTailErr: cause}},
		{"startup positioning", journalOpenModeStartup, fakeJournalReader{seekHeadErr: cause}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tailer := testTailer(t)
			tailer.matches = []string{"UNIT=test", "+"}
			tc.reader.closeErr = errors.New("close failure must not replace operation")
			tailer.readerFactory = func() (journalReader, error) { return &tc.reader, nil }
			reader, err := tailer.openJournal(tc.mode)
			require.Nil(t, reader)
			require.ErrorIs(t, err, cause)
			require.Equal(t, 1, tc.reader.closeCount)
		})
	}
}

// All asynchronous tests have bounded waits and join their owner goroutine.
func startTestTailer(t *testing.T, tailer *tailer) (context.Context, context.CancelFunc, <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	done := make(chan struct{})
	go func() { defer close(done); tailer.ReadFromJournal(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("journal owner did not stop after cancellation")
		}
	})
	return ctx, cancel, done
}

func receiveTestLog(t *testing.T, ctx context.Context, queue <-chan *types.Log) string {
	t.Helper()
	select {
	case log := <-queue:
		message := types.StringOrEmpty(log.GetBodyValue(types.BodyKeyMessage))
		log.Reset()
		types.LogPool.Put(log)
		return message
	case <-ctx.Done():
		t.Fatal("timed out waiting for journal record")
		return ""
	}
}

func TestReadErrorsRecoverAndClearPartials(t *testing.T) {
	for _, operation := range []string{"next", "entry", "wait"} {
		t.Run(operation, func(t *testing.T) {
			tailer := testTailer(t)
			queue := make(chan *types.Log, 1)
			tailer.batchQueue = queue
			cause := errors.New("failed to get realtime timestamp: bad message")
			// Emit a real partial before the discontinuity.
			old := &fakeJournalReader{}
			nextCount := 0
			old.nextFn = func() (uint64, error) {
				nextCount++
				if nextCount == 1 {
					return 1, nil
				}
				if operation == "next" {
					return 0, cause
				}
				if operation == "wait" {
					return 0, nil
				}
				return 1, nil
			}
			entries := 0
			old.entryFn = func() (*sdjournal.JournalEntry, error) {
				entries++
				if entries > 1 {
					return nil, cause
				}
				return &sdjournal.JournalEntry{Cursor: "before-gap", Fields: map[string]string{
					"MESSAGE": "prefix-", journaldStreamIdField: "stream", journaldLineBreakField: journald_line_break_value_line_max,
				}}, nil
			}
			old.waitFn = func(time.Duration) int { return -5 }
			fresh := &fakeJournalReader{previousCount: 1}
			read := false
			fresh.nextFn = func() (uint64, error) {
				if read {
					return 0, nil
				}
				read = true
				return 1, nil
			}
			fresh.entryFn = func() (*sdjournal.JournalEntry, error) {
				return &sdjournal.JournalEntry{Cursor: "after-gap", Fields: map[string]string{"MESSAGE": "suffix", journaldStreamIdField: "stream"}}, nil
			}
			fresh.waitFn = func(timeout time.Duration) int { time.Sleep(timeout); return sdjournal.SD_JOURNAL_NOP }
			opens := 0
			tailer.readerFactory = func() (journalReader, error) {
				opens++
				if opens == 1 {
					return old, nil
				}
				return fresh, nil
			}
			ctx, cancel, done := startTestTailer(t, tailer)
			require.Equal(t, "suffix", receiveTestLog(t, ctx, queue))
			cancel()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("owner failed to exit")
			}
			require.Equal(t, 1, old.closeCount)
			require.Equal(t, 1, fresh.closeCount)
			require.Equal(t, 2, opens)
			require.Empty(t, tailer.streamPartials)
			require.Equal(t, []string{"tail", "previous"}, fresh.calls[:2])
		})
	}
}

func TestWaitResultsRetryNext(t *testing.T) {
	for _, status := range []int{sdjournal.SD_JOURNAL_NOP, sdjournal.SD_JOURNAL_APPEND, sdjournal.SD_JOURNAL_INVALIDATE} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			tailer := testTailer(t)
			queue := make(chan *types.Log, 1)
			tailer.batchQueue = queue
			reader := &fakeJournalReader{}
			calls := 0
			reader.nextFn = func() (uint64, error) {
				calls++
				if calls == 2 {
					return 1, nil
				}
				return 0, nil
			}
			reader.entryFn = func() (*sdjournal.JournalEntry, error) {
				return &sdjournal.JournalEntry{Fields: map[string]string{"MESSAGE": "after-wait"}}, nil
			}
			reader.waitFn = func(timeout time.Duration) int {
				if calls > 2 {
					time.Sleep(timeout)
				}
				return status
			}
			tailer.readerFactory = func() (journalReader, error) { return reader, nil }
			ctx, _, _ := startTestTailer(t, tailer)
			require.Equal(t, "after-wait", receiveTestLog(t, ctx, queue))
		})
	}
}

func TestRecoveryErrorClosesEachReaderOnce(t *testing.T) {
	for _, tc := range []struct {
		name            string
		cancelOnFailure bool
		wantOpens       int
	}{
		{"reopen fails", false, 2},
		{"cancelled before reopen", true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tailer := testTailer(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			old := &fakeJournalReader{nextFn: func() (uint64, error) {
				if tc.cancelOnFailure {
					cancel()
				}
				return 0, errors.New("next failed")
			}}
			fresh := &fakeJournalReader{seekTailErr: errors.New("tail failed")}
			readers := []*fakeJournalReader{old, fresh}
			opens := 0
			tailer.readerFactory = func() (journalReader, error) {
				reader := readers[opens]
				opens++
				return reader, nil
			}
			done := make(chan struct{})
			go func() { defer close(done); tailer.ReadFromJournal(ctx) }()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("tailer did not exit after recovery error")
			}
			require.Equal(t, tc.wantOpens, opens)
			for _, reader := range readers[:opens] {
				require.Equal(t, 1, reader.closeCount)
			}
		})
	}
}

func TestCancellationWhileQueueBlockedClosesReader(t *testing.T) {
	tailer := testTailer(t)
	tailer.batchQueue = make(chan *types.Log)
	entryRead := make(chan struct{})
	reader := &fakeJournalReader{
		nextFn: func() (uint64, error) { return 1, nil },
		entryFn: func() (*sdjournal.JournalEntry, error) {
			close(entryRead)
			return &sdjournal.JournalEntry{Fields: map[string]string{"MESSAGE": "cannot-enqueue"}}, nil
		},
	}
	tailer.readerFactory = func() (journalReader, error) { return reader, nil }
	ctx, cancel, done := startTestTailer(t, tailer)
	select {
	case <-entryRead:
	case <-ctx.Done():
		t.Fatal("entry was not read")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("queue send did not cancel")
	}
	require.Equal(t, 1, reader.closeCount)
}

func TestApplyMatches(t *testing.T) {
	testCases := []struct {
		name           string
		matches        []string
		matchErr       error
		disjunctionErr error
		wantErr        string
		wantCalls      []string
	}{
		{
			name:      "only matches",
			matches:   []string{"FIELD_A=valueA", "FIELD_B=valueB"},
			wantCalls: []string{"match:FIELD_A=valueA", "match:FIELD_B=valueB"},
		},
		{
			name:      "with disjunction",
			matches:   []string{"FIELD_A=valueA", "+", "FIELD_B=valueB"},
			wantCalls: []string{"match:FIELD_A=valueA", "or", "match:FIELD_B=valueB"},
		},
		{
			name:      "match error",
			matches:   []string{"FIELD_A=valueA", "FIELD_B=valueB"},
			matchErr:  errors.New("boom"),
			wantErr:   "failed to add journal match FIELD_A=valueA",
			wantCalls: []string{"match:FIELD_A=valueA"},
		},
		{
			name:           "disjunction error",
			matches:        []string{"FIELD_A=valueA", "+", "FIELD_B=valueB"},
			disjunctionErr: errors.New("boom"),
			wantErr:        "failed to create journal disjunction",
			wantCalls:      []string{"match:FIELD_A=valueA", "or"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			reader := &fakeJournalReader{
				matchErr:       tc.matchErr,
				disjunctionErr: tc.disjunctionErr,
			}

			err := applyMatches(reader, tc.matches)
			if tc.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.wantErr)
			}

			require.Equal(t, tc.wantCalls, reader.calls)
		})
	}
}

// Test files are exports from tests.
// We export from the system journal with the following command, assuming the variable COLLECTORE2E=testValue-3622684368965614593. This rewrites the _HOSTNAME field to testmachine. Requires the systemd-journal-remote package.
// journalctl COLLECTORE2E=testValue-3622684368965614593 --output=export | sed 's/^_HOSTNAME=.*/_HOSTNAME=testmachine/g' | /usr/lib/systemd/systemd-journal-remote -o test_file.journal --split-mode=none -
// Can view in journalctl with journalctl --file test_file.journal

func TestReadFile(t *testing.T) {
	// The fixture probe owns its native handle on this thread. The tailer
	// below opens a separate handle on its own locked owner goroutine.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	tmpdir := t.TempDir()
	cursorPath := cursorPath(tmpdir, []string{"test"}, "testdb", "testtable", "")
	queue := make(chan *types.Log, 1000)

	// journalPath opens a directory rather than individual files, so stage the
	// fixture in a directory of its own.
	journalDir := t.TempDir()
	contents, err := os.ReadFile("test_file.journal")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(journalDir, "test_file.journal"), contents, 0o644))

	// journalctl feature support varies between build containers: some are
	// compiled without what this fixture needs and fail with "protocol not
	// supported". Probe the staged directory once and skip only where the
	// environment genuinely cannot read it, so the success path still runs
	// everywhere else instead of being skipped unconditionally.
	probe, err := sdjournal.NewJournalFromDir(journalDir)
	if err != nil {
		t.Skipf("journal directory reads unsupported in this environment: %v", err)
	}
	require.NoError(t, probe.Close())

	tailer := &tailer{
		database:       "testdb",
		table:          "testtable",
		cursorFilePath: cursorPath,
		journalPath:    journalDir,
		batchQueue:     queue,
		streamPartials: make(map[string]string),
	}

	ctx, cancel, done := startTestTailer(t, tailer)
	for {
		if receiveTestLog(t, ctx, queue) == "4999" { // managed to get to the end
			break
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("fixture tailer did not stop")
	}
}

func TestReadFromJournalNonExisting(t *testing.T) {
	tmpdir := t.TempDir()
	cursorPath := cursorPath(tmpdir, []string{"test"}, "testdb", "testtable", "")
	queue := make(chan *types.Log, 1000)

	tailer := &tailer{
		database:       "testdb",
		table:          "testtable",
		matches:        []string{"_SYSTEMD_UNIT=kubelet.service", "_HOSTNAME=testmachine"},
		cursorFilePath: cursorPath,
		journalPath:    filepath.Join(t.TempDir(), "non_existing_dir"),
		batchQueue:     queue,
		streamPartials: make(map[string]string),
	}

	ctx, _, done := startTestTailer(t, tailer)
	select {
	case <-done: // Opening a nonexistent directory must exit.
	case <-ctx.Done():
		t.Fatal("journal tailer did not exit after failed open")
	}
}
