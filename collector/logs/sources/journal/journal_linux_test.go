package journal

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"testing"

	"github.com/Azure/adx-mon/collector/logs"
	"github.com/Azure/adx-mon/collector/logs/engine"
	"github.com/Azure/adx-mon/collector/logs/sinks"
	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/coreos/go-systemd/journal"
	"github.com/stretchr/testify/require"
)

func TestJournalE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}
	if !journal.Enabled() {
		t.Skip("journal is not available - skipping")
	}

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
	source := New(SourceConfig{
		Targets: []JournalTargetConfig{
			{
				Matches:  []string{matchTag},
				Database: "testdb",
				Table:    "testtable",
			},
		},
		CursorDirectory: cursorDir,
		WorkerCreator:   engine.WorkerCreator(nil, sink),
	})

	service := &logs.Service{
		Source: source,
		Sink:   sink,
	}
	ctx := context.Background()
	err := service.Open(ctx)
	require.NoError(t, err)
	<-sink.DoneChan()
	service.Close()
	require.Equal(t, fmt.Sprintf("%d", numLogs-1), sink.Latest().Body[types.BodyKeyMessage])

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
	sink = sinks.NewCountingSink(int64(numLogs))
	source = New(SourceConfig{
		Targets: []JournalTargetConfig{
			{
				Matches:  []string{matchTag},
				Database: "testdb",
				Table:    "testtable",
			},
		},
		CursorDirectory: cursorDir,
		WorkerCreator:   engine.WorkerCreator(nil, sink),
	})

	service = &logs.Service{
		Source: source,
		Sink:   sink,
	}
	ctx = context.Background()
	err = service.Open(ctx)
	require.NoError(t, err)
	defer service.Close()
	<-sink.DoneChan()
	require.Equal(t, fmt.Sprintf("%d", numLogs*2-1), sink.Latest().Body[types.BodyKeyMessage])
}
