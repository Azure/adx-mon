package tail

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Azure/adx-mon/collector/logs"
	"github.com/Azure/adx-mon/collector/logs/engine"
	"github.com/Azure/adx-mon/collector/logs/sinks"
	"github.com/Azure/adx-mon/collector/logs/sources/tail/sourceparse"
	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/stretchr/testify/require"
)

func TestTailSourceStaticTargets(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}
	t.Parallel()
	numLogs := 1000

	testDir := t.TempDir()
	// consistent date so we know how many bytes are generated in the file.
	generatedLogStartTime := time.Unix(0, 0)
	testFileOne := filepath.Join(testDir, "test.log")
	generateLogs(t, testFileOne, numLogs, generatedLogStartTime, time.Millisecond*10)
	testFileTwo := filepath.Join(testDir, "test2.log")
	generateLogs(t, testFileTwo, numLogs, generatedLogStartTime, time.Millisecond*10)

	// Expect 2x numLogs, for both files
	sink := sinks.NewCountingSink(int64(numLogs * 2))
	allSinks := []types.Sink{sink}
	tailSource, err := NewTailSource(TailSourceConfig{
		StaticTargets: []FileTailTarget{
			{
				FilePath: testFileOne,
				LogType:  sourceparse.LogTypeDocker,
				Database: "Logs",
				Table:    "TestService",
				Parsers:  []string{"json"},
			},
			{
				FilePath: testFileTwo,
				LogType:  sourceparse.LogTypePlain,
				Database: "Logs",
				Table:    "TestServiceTwo",
			},
		},
		CursorDirectory: testDir,
		WorkerCreator:   engine.WorkerCreator(nil, allSinks),
	})
	require.NoError(t, err)

	service := &logs.Service{
		Source: tailSource,
		Sinks:  allSinks,
	}
	context := context.Background()

	err = service.Open(context)
	require.NoError(t, err)
	defer service.Close()
	<-sink.DoneChan()

	fidone, testOffsetOne, err := readCursor(cursorPath(testDir, testFileOne))
	require.NoError(t, err)
	require.Equal(t, int64(74770), testOffsetOne)
	require.NotEmpty(t, fidone)
	fidtwo, testOffsetTwo, err := readCursor(cursorPath(testDir, testFileTwo))
	require.NoError(t, err)
	require.Equal(t, testOffsetOne, testOffsetTwo)
	require.NotEmpty(t, fidtwo)
	// Same contents that were read with same offsets, but different files with different ids.
	require.NotEqual(t, fidone, fidtwo)
}

func TestTailSourcePartialLogs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}
	t.Parallel()
	testDir := t.TempDir()
	testFileOne := filepath.Join(testDir, "test.log")
	file, err := os.OpenFile(testFileOne, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0640)
	require.NoError(t, err)
	currentTime := time.Now()
	writeLogLine(t, file, currentTime, `line one\n`)
	currentTime = currentTime.Add(1 * time.Second)
	writeLogLine(t, file, currentTime, `line two `)
	writeLogLine(t, file, currentTime, `finished\n`)
	currentTime = currentTime.Add(1 * time.Second)
	writeLogLine(t, file, currentTime, `line three\n`)
	currentTime = currentTime.Add(1 * time.Second)
	writeLogLine(t, file, currentTime, `line four`)
	writeLogLine(t, file, currentTime, ` continued `)
	writeLogLine(t, file, currentTime, ` again\n`)
	currentTime = currentTime.Add(1 * time.Second)
	writeLogLine(t, file, currentTime, `line five\n`)
	file.Close()

	sink := sinks.NewCountingSink(int64(5))
	allSinks := []types.Sink{sink}
	tailSource, err := NewTailSource(TailSourceConfig{
		StaticTargets: []FileTailTarget{
			{
				FilePath: testFileOne,
				LogType:  sourceparse.LogTypeDocker,
				Database: "Logs",
				Table:    "TestService",
				Parsers:  []string{"json"},
			},
		},
		CursorDirectory: testDir,
		WorkerCreator:   engine.WorkerCreator(nil, allSinks),
	})
	require.NoError(t, err)

	service := &logs.Service{
		Source: tailSource,
		Sinks:  allSinks,
	}
	context := context.Background()

	err = service.Open(context)
	require.NoError(t, err)
	defer service.Close()
	<-sink.DoneChan()
}

func TestTailSourceDynamicTargets(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}
	t.Parallel()
	numLogs := 1000

	testDir := t.TempDir()
	// consistent date so we know how many bytes are generated in the file.
	generatedLogStartTime := time.Unix(0, 0)
	testFileOne := filepath.Join(testDir, "test.log")
	generateLogs(t, testFileOne, numLogs, generatedLogStartTime, time.Millisecond*10)
	testFileTwo := filepath.Join(testDir, "test2.log")
	generateLogs(t, testFileTwo, numLogs, generatedLogStartTime, time.Millisecond*10)

	// Expect 2x numLogs, for both files
	sink := sinks.NewCountingSink(int64(numLogs * 2))
	allSinks := []types.Sink{sink}
	tailSource, err := NewTailSource(TailSourceConfig{
		StaticTargets:   []FileTailTarget{},
		CursorDirectory: testDir,
		WorkerCreator:   engine.WorkerCreator(nil, allSinks),
	})
	require.NoError(t, err)

	service := &logs.Service{
		Source: tailSource,
		Sinks:  allSinks,
	}
	context := context.Background()

	err = service.Open(context)
	require.NoError(t, err)
	defer service.Close()

	tailSource.AddTarget(
		FileTailTarget{
			FilePath: testFileOne,
			Database: "Logs",
			Table:    "TestService",
		},
		nil,
	)
	tailSource.AddTarget(
		FileTailTarget{
			FilePath: testFileTwo,
			Database: "Logs",
			Table:    "TestServiceTwo",
		},
		nil,
	)
	// Same source as first, so NOOP
	tailSource.AddTarget(
		FileTailTarget{
			FilePath: testFileOne,
			Database: "Logs",
			Table:    "TestService",
		},
		nil,
	)

	<-sink.DoneChan()

	// Validate cursors
	fidone, testOffsetOne, err := readCursor(cursorPath(testDir, testFileOne))
	require.NoError(t, err)
	require.Equal(t, int64(74770), testOffsetOne)
	require.NotEmpty(t, fidone)
	fidtwo, testOffsetTwo, err := readCursor(cursorPath(testDir, testFileTwo))
	require.NoError(t, err)
	require.Equal(t, testOffsetOne, testOffsetTwo)
	require.NotEmpty(t, fidtwo)
	// Same contents that were read with same offsets, but different files with different ids.
	require.NotEqual(t, fidone, fidtwo)

	tailSource.RemoveTarget(testFileOne)
	tailSource.RemoveTarget(testFileTwo)

	// Removing targets removes cursor files
	_, _, err = readCursor(cursorPath(testDir, testFileOne))
	require.ErrorIs(t, err, os.ErrNotExist)
	_, _, err = readCursor(cursorPath(testDir, testFileTwo))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestTailSourceDynamicUtilizesCursors(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}
	t.Parallel()
	numLogs := 1000

	testDir := t.TempDir()
	// consistent date so we know how many bytes are generated in the file.
	generatedLogStartTime := time.Unix(0, 0)
	testFileOne := filepath.Join(testDir, "test.log")
	generateLogs(t, testFileOne, numLogs, generatedLogStartTime, time.Millisecond*10)

	// -------- Setup first run, reading the first half of the logs --------
	sink := sinks.NewCountingSink(int64(numLogs))
	allSinks := []types.Sink{sink}
	tailSource, err := NewTailSource(TailSourceConfig{
		StaticTargets:   []FileTailTarget{},
		CursorDirectory: testDir,
		WorkerCreator:   engine.WorkerCreator(nil, allSinks),
	})
	require.NoError(t, err)

	service := &logs.Service{
		Source: tailSource,
		Sinks:  allSinks,
	}
	ctx := context.Background()

	err = service.Open(ctx)
	require.NoError(t, err)

	tailSource.AddTarget(
		FileTailTarget{
			FilePath: testFileOne,
			Database: "Logs",
			Table:    "TestService",
		},
		nil,
	)

	read := <-sink.DoneChan() // read first batch
	require.Equal(t, int64(numLogs), read)
	service.Close() // Shutdown all.

	// -------- Setup second run, reading the second batch of the logs --------
	numLogsTwo := 650
	f, err := os.OpenFile(testFileOne, os.O_APPEND|os.O_WRONLY, 0640)
	require.NoError(t, err)
	defer f.Close()
	writeLogs(t, f, numLogsTwo, generatedLogStartTime, time.Millisecond*10)
	sink = sinks.NewCountingSink(int64(numLogsTwo))
	allSinks = []types.Sink{sink}
	tailSource, err = NewTailSource(TailSourceConfig{
		StaticTargets:   []FileTailTarget{},
		CursorDirectory: testDir,
		WorkerCreator:   engine.WorkerCreator(nil, allSinks),
	})
	require.NoError(t, err)

	service = &logs.Service{
		Source: tailSource,
		Sinks:  allSinks,
	}
	ctx = context.Background()

	err = service.Open(ctx)
	require.NoError(t, err)

	tailSource.AddTarget(
		FileTailTarget{
			FilePath: testFileOne,
			Database: "Logs",
			Table:    "TestService",
		},
		nil,
	)

	read = <-sink.DoneChan() // read second batch
	// Does not start from the beginning, but only reads the new logs.
	require.Equal(t, int64(numLogsTwo), read)
	service.Close()
}

func BenchmarkTailSource(b *testing.B) {
	numLogs := 1000

	testDir := b.TempDir()
	testFile := filepath.Join(testDir, "test.log")
	generateLogs(b, testFile, numLogs, time.Now(), time.Millisecond*10)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		sink := sinks.NewCountingSink(int64(numLogs))
		allSinks := []types.Sink{sink}
		tailSource, err := NewTailSource(TailSourceConfig{
			StaticTargets: []FileTailTarget{
				{
					FilePath: testFile,
					LogType:  sourceparse.LogTypeDocker,
					Database: "Logs",
					Table:    "TestService",
				},
			},
			CursorDirectory: b.TempDir(),
			WorkerCreator:   engine.WorkerCreator(nil, allSinks),
		})
		require.NoError(b, err)

		service := &logs.Service{
			Source: tailSource,
			Sinks:  allSinks,
		}
		context := context.Background()

		service.Open(context)
		<-sink.DoneChan()
		service.Close()
	}
}

func BenchmarkTailSourceMultipleSources(b *testing.B) {
	numLogs := 1000

	testDir := b.TempDir()
	testFileOne := filepath.Join(testDir, "test.log")
	generateLogs(b, testFileOne, numLogs, time.Now(), time.Millisecond*10)
	testFileTwo := filepath.Join(testDir, "test2.log")
	generateLogs(b, testFileTwo, numLogs, time.Now(), time.Millisecond*10)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Expect 2x numLogs, for both files
		sink := sinks.NewCountingSink(int64(numLogs * 2))
		allSinks := []types.Sink{sink}
		tailSource, err := NewTailSource(TailSourceConfig{
			StaticTargets: []FileTailTarget{
				{
					FilePath: testFileOne,
					LogType:  sourceparse.LogTypeDocker,
					Database: "Logs",
					Table:    "TestService",
				},
				{
					FilePath: testFileTwo,
					LogType:  sourceparse.LogTypeDocker,
					Database: "Logs",
					Table:    "TestServiceTwo",
				},
			},
			CursorDirectory: b.TempDir(),
			WorkerCreator:   engine.WorkerCreator(nil, allSinks),
		})
		require.NoError(b, err)

		service := &logs.Service{
			Source: tailSource,
			Sinks:  allSinks,
		}
		context := context.Background()

		service.Open(context)
		<-sink.DoneChan()
		service.Close()
	}
}

func TestCursorFile(t *testing.T) {
	t.Run("reversability", func(t *testing.T) {
		testDir := t.TempDir()
		cursorPath := filepath.Join(testDir, "test.cursor")

		file_id := "some:_file_id"
		cursor := int64(13532523)
		err := writeCursor(cursorPath, file_id, cursor)
		require.NoError(t, err)

		file_id2, cursor2, err := readCursor(cursorPath)
		require.NoError(t, err)
		require.Equal(t, file_id, file_id2)
		require.Equal(t, cursor, cursor2)
	})

	t.Run("readCursor returns not exists error for non-existing file", func(t *testing.T) {
		testDir := t.TempDir()
		cursorPath := filepath.Join(testDir, "test.cursor")

		_, _, err := readCursor(cursorPath)
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("readCursor returns error for malformed cursor file", func(t *testing.T) {
		testDir := t.TempDir()
		cursorPath := filepath.Join(testDir, "test.cursor")

		err := os.WriteFile(cursorPath, []byte("malformed\n"), 0640)
		require.NoError(t, err)

		_, _, err = readCursor(cursorPath)
		require.Error(t, err)
	})

	t.Run("cursorPath creates unique paths for different files", func(t *testing.T) {
		storageDir := t.TempDir()
		cursorPathOne := cursorPath(storageDir, "/var/log/pods/podone/0.log")
		cursorPathTwo := cursorPath(storageDir, "/var/log/pods/podtwo/0.log")
		require.NotEqual(t, cursorPathOne, cursorPathTwo)
	})
}

func generateLogs(t testing.TB, fileName string, count int, startTime time.Time, interval time.Duration) {
	t.Helper()
	file, err := os.OpenFile(fileName, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0640)
	require.NoError(t, err)
	defer file.Close()

	writeLogs(t, file, count, startTime, interval)
}

func writeLogs(t testing.TB, file *os.File, count int, startTime time.Time, interval time.Duration) {
	t.Helper()
	currentTime := startTime.UTC()
	for i := 0; i < count; i++ {
		writeLogLine(t, file, currentTime, fmt.Sprintf(`li %d\n`, i))
		currentTime = currentTime.Add(interval)
	}

	err := file.Sync()
	require.NoError(t, err)
}

func writeLogLine(t testing.TB, file *os.File, currentTime time.Time, log string) {
	t.Helper()
	timestamp := currentTime.Format(time.RFC3339Nano)
	_, err := file.WriteString(fmt.Sprintf(`{"time": "%s", "stream": "stdout", "log": "%s"}`, timestamp, log))
	require.NoError(t, err)
	_, err = file.WriteString("\n")
	require.NoError(t, err)
}
