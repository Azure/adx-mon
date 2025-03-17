//go:build linux && cgo

package journal

import (
	"context"
	"sync"
	"testing"

	"github.com/Azure/adx-mon/collector/logs/types"
)

// Test files are exports from tests.
// We export from the system journal with the following command, assuming the variable COLLECTORE2E=testValue-3622684368965614593. This rewrites the _HOSTNAME field to testmachine. Requires the systemd-journal-remote package.
// journalctl COLLECTORE2E=testValue-3622684368965614593 --output=export | sed 's/^_HOSTNAME=.*/_HOSTNAME=testmachine/g' | /usr/lib/systemd/systemd-journal-remote -o test_file.journal --split-mode=none -
// Can view in journalctl with journalctl --file test_file.journal

func TestReadFile(t *testing.T) {
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
		if msg.Body[types.BodyKeyMessage] == "4999" { // managed to get to the end
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
