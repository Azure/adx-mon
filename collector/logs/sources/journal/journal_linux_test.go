//go:build linux && cgo

package journal

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/Azure/adx-mon/collector/logs"
	"github.com/Azure/adx-mon/collector/logs/engine"
	"github.com/Azure/adx-mon/collector/logs/sinks"
	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/coreos/go-systemd/journal"
	"github.com/coreos/go-systemd/sdjournal"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

const (
	journalProbeTimeout = 5 * time.Second
	sinkTimeout         = 60 * time.Second
)

// requireReadableJournal verifies that entries sent by this process are
// visible to sdjournal before running tests that otherwise wait for delivery.
func requireReadableJournal(t *testing.T) {
	t.Helper()
	if !journal.Enabled() {
		t.Skip("journal is not available - skipping")
	}

	// sdjournal handles must only be used from the OS thread that opened them.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	reader, err := sdjournal.NewJournal()
	if err != nil {
		t.Skipf("cannot open journal for reading: %v", err)
	}
	defer reader.Close()

	probeTag := "COLLECTOR_JOURNAL_READ_PROBE"
	probeValue := fmt.Sprintf("%d-%d", os.Getpid(), rand.Int())
	if err := reader.AddMatch(probeTag + "=" + probeValue); err != nil {
		t.Skipf("cannot match journal entries: %v", err)
	}
	if err := reader.SeekTail(); err != nil {
		t.Skipf("cannot seek journal: %v", err)
	}
	if err := journal.Send("journal read visibility probe", journal.PriInfo, map[string]string{probeTag: probeValue}); err != nil {
		t.Skipf("cannot write journal probe: %v", err)
	}

	deadline := time.Now().Add(journalProbeTimeout)
	for time.Now().Before(deadline) {
		n, err := reader.Next()
		if err != nil {
			t.Skipf("cannot read journal entries: %v", err)
		}
		if n > 0 {
			entry, err := reader.GetEntry()
			if err != nil {
				t.Skipf("cannot read journal entry: %v", err)
			}
			if entry.Fields[probeTag] == probeValue {
				return
			}
		}
		remaining := time.Until(deadline)
		if remaining > 100*time.Millisecond {
			remaining = 100 * time.Millisecond
		}
		if status := reader.Wait(remaining); status < 0 {
			t.Skipf("cannot wait for journal entries: status %d", status)
		}
	}
	t.Skip("journal accepts writes but does not expose them to this process")
}

// waitForSink fails rather than leaving a journal integration test blocked.
func waitForSink(t *testing.T, sink *sinks.CountingSink) int64 {
	t.Helper()
	select {
	case count := <-sink.DoneChan():
		return count
	case <-time.After(sinkTimeout):
		t.Fatalf("journal entries did not arrive within %s; journald may be rate-limiting this unit", sinkTimeout)
		return 0
	}
}

func TestJournalE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}
	requireReadableJournal(t)

	logger.SetLevel(slog.LevelDebug)

	cursorDir := t.TempDir()
	testtag := "COLLECTORE2E"
	randNum := rand.Int()
	testValue := fmt.Sprintf("testValue-%d", randNum)
	journalFields := map[string]string{testtag: testValue}
	t.Logf("Sending logs - view in journalctl with journalctl %s=%s", testtag, testValue)
	matchTag := fmt.Sprintf("%s=%s", testtag, testValue)

	numLogs := 1000
	for i := 0; i < numLogs; i++ {
		err := journal.Send(fmt.Sprintf("%d", i), journal.PriInfo, journalFields)
		require.NoError(t, err)
	}

	sink := sinks.NewCountingSink(int64(numLogs))
	allSinks := []types.Sink{sink}
	source := New(SourceConfig{
		Targets: []JournalTargetConfig{
			{
				Matches:  []string{matchTag},
				Database: "testdb",
				Table:    "testtable",
			},
		},
		CursorDirectory: cursorDir,
		WorkerCreator:   engine.WorkerCreator(nil, allSinks),
	})

	service := &logs.Service{
		Source: source,
		Sinks:  allSinks,
	}
	ctx := context.Background()
	err := service.Open(ctx)
	require.NoError(t, err)
	serviceClosed := false
	t.Cleanup(func() {
		if !serviceClosed {
			_ = service.Close()
		}
	})
	waitForSink(t, sink)
	_ = service.Close()
	serviceClosed = true
	require.Equal(t, fmt.Sprintf("%d", numLogs-1), types.StringOrEmpty(sink.Latest().GetBodyValue(types.BodyKeyMessage)))

	// Start next batch with same tag, offset from the last log
	for i := numLogs; i < numLogs*2; i++ {
		err := journal.Send(fmt.Sprintf("%d", i), journal.PriInfo, journalFields)
		require.NoError(t, err)
	}

	// Write logs with non-matching tags.
	for i := 0; i < numLogs; i++ {
		err := journal.Send("a", journal.PriInfo, map[string]string{"COLLECTORE2E": "OTHERTAG"})
		require.NoError(t, err)
	}

	// Expect numLogs more logs
	newSink := sinks.NewCountingSink(int64(numLogs))
	newAllSinks := []types.Sink{newSink}
	source = New(SourceConfig{
		Targets: []JournalTargetConfig{
			{
				Matches:       []string{matchTag},
				Database:      "testdb",
				Table:         "testtable",
				JournalFields: []string{sdjournal.SD_JOURNAL_FIELD_EXE},
			},
		},
		CursorDirectory: cursorDir,
		WorkerCreator:   engine.WorkerCreator(nil, newAllSinks),
	})

	service = &logs.Service{
		Source: source,
		Sinks:  newAllSinks,
	}
	ctx = context.Background()
	err = service.Open(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = service.Close() })
	waitForSink(t, newSink)
	require.Equal(t, fmt.Sprintf("%d", numLogs*2-1), types.StringOrEmpty(newSink.Latest().GetBodyValue(types.BodyKeyMessage)))

	// Ensure the systemd unit identifier is set
	resource, ok := newSink.Latest().GetResourceValue(sdjournal.SD_JOURNAL_FIELD_EXE)
	require.True(t, ok)
	require.NotEmpty(t, resource)
}

func TestJournalMulipleSources(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}
	requireReadableJournal(t)

	logger.SetLevel(slog.LevelDebug)

	cursorDir := t.TempDir()
	testtagOne := "COLLECTORE2E"
	testtagTwo := "COLLECTORE2E2"

	randNum := rand.Int()
	testValue := fmt.Sprintf("testValue-%d", randNum)
	journalFieldsOne := map[string]string{testtagOne: testValue}
	journalFieldsTwo := map[string]string{testtagTwo: testValue}
	t.Logf("Sending logs - view in journalctl with journalctl %s=%s", testtagOne, testValue)
	t.Logf("Sending logs - view in journalctl with journalctl %s=%s", testtagTwo, testValue)
	matchTagOne := fmt.Sprintf("%s=%s", testtagOne, testValue)
	matchTagTwo := fmt.Sprintf("%s=%s", testtagTwo, testValue)

	// Keep the volume below journald's default per-unit rate limit.
	numLogs := 1000
	errgrp, _ := errgroup.WithContext(context.Background())
	errgrp.Go(func() error {
		for i := 0; i < numLogs; i++ {
			err := journal.Send(fmt.Sprintf("%d", i), journal.PriInfo, journalFieldsOne)
			if err != nil {
				return err
			}
		}
		return nil
	})
	errgrp.Go(func() error {
		for i := 0; i < numLogs; i++ {
			err := journal.Send(fmt.Sprintf("%d", i), journal.PriInfo, journalFieldsTwo)
			if err != nil {
				return err
			}
		}
		return nil
	})

	sink := sinks.NewCountingSink(int64(numLogs * 2)) // both sources send numLogs
	allSinks := []types.Sink{sink}
	source := New(SourceConfig{
		Targets: []JournalTargetConfig{
			{
				Matches:  []string{matchTagOne},
				Database: "testdb",
				Table:    "testtable",
			},
			{
				Matches:  []string{matchTagTwo},
				Database: "testdb",
				Table:    "testtable",
			},
		},
		CursorDirectory: cursorDir,
		WorkerCreator:   engine.WorkerCreator(nil, allSinks),
	})

	service := &logs.Service{
		Source: source,
		Sinks:  allSinks,
	}
	ctx := context.Background()
	err := service.Open(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = service.Close() })

	err = errgrp.Wait()
	require.NoError(t, err)

	waitForSink(t, sink)
	require.Equal(t, fmt.Sprintf("%d", numLogs-1), types.StringOrEmpty(sink.Latest().GetBodyValue(types.BodyKeyMessage)))
}

func TestJournalDisjunction(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}
	requireReadableJournal(t)

	logger.SetLevel(slog.LevelDebug)

	cursorDir := t.TempDir()
	testValue := fmt.Sprintf("testValue-%d", rand.Int())
	branchATag := "COLLECTORE2E_OR_A"
	branchBTag := "COLLECTORE2E_OR_B"
	controlTag := "COLLECTORE2E_OR_CONTROL"
	matchA := fmt.Sprintf("%s=%s", branchATag, testValue)
	matchB := fmt.Sprintf("%s=%s", branchBTag, testValue)

	const numLogs = 50
	for i := 0; i < numLogs; i++ {
		err := journal.Send(fmt.Sprintf("a-%d", i), journal.PriInfo, map[string]string{branchATag: testValue})
		require.NoError(t, err)
	}
	for i := 0; i < numLogs; i++ {
		err := journal.Send(fmt.Sprintf("b-%d", i), journal.PriInfo, map[string]string{branchBTag: testValue})
		require.NoError(t, err)
	}
	for i := 0; i < numLogs; i++ {
		err := journal.Send(fmt.Sprintf("control-%d", i), journal.PriInfo, map[string]string{controlTag: testValue})
		require.NoError(t, err)
	}

	sink := sinks.NewCountingSink(int64(numLogs * 2))
	allSinks := []types.Sink{sink}
	source := New(SourceConfig{
		Targets: []JournalTargetConfig{{
			Matches:  []string{matchA, "+", matchB},
			Database: "testdb",
			Table:    "testtable",
		}},
		CursorDirectory: cursorDir,
		WorkerCreator:   engine.WorkerCreator(nil, allSinks),
	})

	service := &logs.Service{
		Source: source,
		Sinks:  allSinks,
	}
	err := service.Open(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = service.Close() })

	count := waitForSink(t, sink)
	require.EqualValues(t, numLogs*2, count)
}

func TestJournalInvalidCursor(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}
	requireReadableJournal(t)

	logger.SetLevel(slog.LevelDebug)

	cursorDir := t.TempDir()
	testtag := "COLLECTORE2E_INVALID_CURSOR"
	randNum := rand.Int()
	testValue := fmt.Sprintf("testValue-%d", randNum)
	journalFields := map[string]string{testtag: testValue}
	t.Logf("Sending logs - view in journalctl with journalctl %s=%s", testtag, testValue)
	matchTag := fmt.Sprintf("%s=%s", testtag, testValue)

	numLogs := 500
	// Send logs first
	for i := 0; i < numLogs; i++ {
		err := journal.Send(fmt.Sprintf("%d", i), journal.PriInfo, journalFields)
		require.NoError(t, err)
	}
	// Create an invalid cursor file manually
	// This simulates having a cursor from a previous run that is no longer valid
	targetConfig := JournalTargetConfig{
		Matches:  []string{matchTag},
		Database: "testdb",
		Table:    "testtable",
	}
	cursorFilePath := cursorPath(cursorDir, targetConfig.Matches, targetConfig.Database, targetConfig.Table, "")

	// Write an invalid cursor with the correct format, but not from this system
	invalidCursor := "s=65a1fbde9961443ab61e48f57b1e1cfb;i=32904c2;b=93046e4a5c424d96ad6e02659ce7dde9;m=7b29e77112;t=639d5fa6bb246;x=1cacd317ebe1eb33"
	err := os.WriteFile(cursorFilePath, []byte(fmt.Sprintf(`{"cursor":"%s"}`, invalidCursor)), 0644)
	require.NoError(t, err)

	// Create the journal source with the invalid cursor
	sink := sinks.NewCountingSink(int64(numLogs))
	allSinks := []types.Sink{sink}
	source := New(SourceConfig{
		Targets:         []JournalTargetConfig{targetConfig},
		CursorDirectory: cursorDir,
		WorkerCreator:   engine.WorkerCreator(nil, allSinks),
	})

	service := &logs.Service{
		Source: source,
		Sinks:  allSinks,
	}
	ctx := context.Background()
	err = service.Open(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = service.Close() })

	// Wait for all logs to be processed
	count := waitForSink(t, sink)

	// Since we had an invalid cursor, the journal should have fallen back to reading from head
	// and should have read all logs we sent
	require.Equal(t, fmt.Sprintf("%d", numLogs-1), types.StringOrEmpty(sink.Latest().GetBodyValue(types.BodyKeyMessage)))

	// Verify we got all the logs we expected
	require.Equal(t, int64(numLogs), count)
}
