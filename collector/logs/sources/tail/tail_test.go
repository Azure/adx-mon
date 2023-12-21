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
	"github.com/stretchr/testify/require"
)

func TestTailSource(t *testing.T) {
	numLogs := 1000

	testDir := t.TempDir()
	// consistent date so we know how many bytes are generated in the file.
	generatedLogStartTime := time.Unix(0, 0)
	testFile := filepath.Join(testDir, "test.log")
	generateLogs(t, testFile, numLogs, generatedLogStartTime, time.Millisecond*10)

	sink := sinks.NewCountingSink(int64(numLogs))
	tailSource, err := NewTailSource(TailSourceConfig{
		StaticTargets: []FileTailTarget{
			{
				FilePath: testFile,
				LogType:  LogTypeDocker,
			},
		},
		CursorDirectory: testDir,
		WorkerCreator:   engine.WorkerCreator(nil, sink),
	})
	require.NoError(t, err)

	service := &logs.Service{
		Source: tailSource,
		Sink:   sink,
	}
	context := context.Background()

	service.Open(context)
	<-sink.DoneChan()
	service.Close()

	_, testOffset, err := readCursor(cursorPath(testDir, testFile))
	require.NoError(t, err)
	require.Equal(t, int64(74770), testOffset)
}

func TestTailSourceMultipleSources(t *testing.T) {
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
	tailSource, err := NewTailSource(TailSourceConfig{
		StaticTargets: []FileTailTarget{
			{
				FilePath: testFileOne,
				LogType:  LogTypeDocker,
			},
			{
				FilePath: testFileTwo,
				LogType:  LogTypeDocker,
			},
		},
		CursorDirectory: testDir,
		WorkerCreator:   engine.WorkerCreator(nil, sink),
	})
	require.NoError(t, err)

	service := &logs.Service{
		Source: tailSource,
		Sink:   sink,
	}
	context := context.Background()

	service.Open(context)
	<-sink.DoneChan()
	service.Close()

	_, testOffsetOne, err := readCursor(cursorPath(testDir, testFileOne))
	require.NoError(t, err)
	require.Equal(t, int64(74770), testOffsetOne)
	_, testOffsetTwo, err := readCursor(cursorPath(testDir, testFileTwo))
	require.NoError(t, err)
	require.Equal(t, testOffsetOne, testOffsetTwo)
}

func BenchmarkTailSource(b *testing.B) {
	numLogs := 1000

	testDir := b.TempDir()
	testFile := filepath.Join(testDir, "test.log")
	generateLogs(b, testFile, numLogs, time.Now(), time.Millisecond*10)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		sink := sinks.NewCountingSink(int64(numLogs))
		tailSource, err := NewTailSource(TailSourceConfig{
			StaticTargets: []FileTailTarget{
				{
					FilePath: testFile,
					LogType:  LogTypeDocker,
				},
			},
			CursorDirectory: b.TempDir(),
			WorkerCreator:   engine.WorkerCreator(nil, sink),
		})
		require.NoError(b, err)

		service := &logs.Service{
			Source: tailSource,
			Sink:   sink,
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
		tailSource, err := NewTailSource(TailSourceConfig{
			StaticTargets: []FileTailTarget{
				{
					FilePath: testFileOne,
					LogType:  LogTypeDocker,
				},
				{
					FilePath: testFileTwo,
					LogType:  LogTypeDocker,
				},
			},
			CursorDirectory: b.TempDir(),
			WorkerCreator:   engine.WorkerCreator(nil, sink),
		})
		require.NoError(b, err)

		service := &logs.Service{
			Source: tailSource,
			Sink:   sink,
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
}

func generateLogs(t testing.TB, fileName string, count int, startTime time.Time, interval time.Duration) {
	t.Helper()
	file, err := os.OpenFile(fileName, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0640)
	require.NoError(t, err)
	defer file.Close()

	currentTime := startTime.UTC()
	for i := 0; i < count; i++ {
		timestamp := currentTime.Format(time.RFC3339Nano)
		_, err := file.WriteString(fmt.Sprintf(`{"time": "%s", "stream": "stdout", "log": "line %d"}`, timestamp, i))
		require.NoError(t, err)
		_, err = file.WriteString("\n")
		require.NoError(t, err)
		currentTime = currentTime.Add(interval)
	}

	err = file.Sync()
	require.NoError(t, err)
}
