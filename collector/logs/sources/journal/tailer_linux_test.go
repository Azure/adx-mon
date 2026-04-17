//go:build linux && cgo

package journal

import (
	"context"
	"errors"
	"sync"
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
}

func (f *fakeJournalReader) AddMatch(match string) error {
	f.calls = append(f.calls, "match:"+match)
	return f.matchErr
}

func (f *fakeJournalReader) AddDisjunction() error {
	f.calls = append(f.calls, "or")
	return f.disjunctionErr
}

func (f *fakeJournalReader) Next() (uint64, error) { return 0, nil }

func (f *fakeJournalReader) GetEntry() (*sdjournal.JournalEntry, error) { return nil, nil }

func (f *fakeJournalReader) NextSkip(skip uint64) (uint64, error) { return 0, nil }

func (f *fakeJournalReader) SeekHead() error { return nil }

func (f *fakeJournalReader) SeekCursor(cursor string) error { return nil }

func (f *fakeJournalReader) TestCursor(cursor string) error { return nil }

func (f *fakeJournalReader) Wait(timeout time.Duration) int { return 0 }

func (f *fakeJournalReader) Close() error { return nil }

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
	t.Skip("skipping test because of inconsistent journalctl feature support in some build containers. Some will error out with 'protocol not supported' based on compiled features.")
	tmpdir := t.TempDir()
	cursorPath := cursorPath(tmpdir, []string{"test"}, "testdb", "testtable")
	queue := make(chan *types.Log, 1000)

	tailer := &tailer{
		database:       "testdb",
		table:          "testtable",
		cursorFilePath: cursorPath,
		journalFiles:   []string{"test_file.journal"},
		batchQueue:     queue,
		streamPartials: make(map[string]string),
	}

	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())

	wg.Add(1)
	go func() {
		tailer.ReadFromJournal(ctx)
		wg.Done()
	}()

	for msg := range queue {
		if val, _ := msg.GetBodyValue(types.BodyKeyMessage); val == "4999" { // managed to get to the end
			break
		}
	}

	cancel()
	wg.Wait()
}

func TestReadFromJournalNonExisting(t *testing.T) {
	tmpdir := t.TempDir()
	cursorPath := cursorPath(tmpdir, []string{"test"}, "testdb", "testtable")
	queue := make(chan *types.Log, 1000)

	tailer := &tailer{
		database:       "testdb",
		table:          "testtable",
		matches:        []string{"_SYSTEMD_UNIT=kubelet.service", "_HOSTNAME=testmachine"},
		cursorFilePath: cursorPath,
		journalFiles:   []string{"non_existing_file.journal"},
		batchQueue:     queue,
		streamPartials: make(map[string]string),
	}

	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())

	wg.Add(1)
	go func() {
		tailer.ReadFromJournal(ctx)
		wg.Done()
	}()

	wg.Wait() //goroutine exits without file, no need to cancel
	cancel()
}
