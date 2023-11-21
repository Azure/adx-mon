package tail

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Azure/adx-mon/collector/logs"
	"github.com/Azure/adx-mon/collector/logs/sinks"
	"github.com/stretchr/testify/require"
)

func TestTailSource(t *testing.T) {
	numLogs := 1000

	testDir := t.TempDir()
	testFile := filepath.Join(testDir, "test.log")
	generateLogs(t, testFile, numLogs, time.Now(), time.Millisecond*10)

	tailSource, err := NewTailSource(TailSourceConfig{
		StaticTargets: []FileTailTarget{
			{
				FilePath: testFile,
				LogType:  LogTypeDocker,
			},
		},
		CursorDirectory: testDir,
	})
	require.NoError(t, err)
	sink := sinks.NewCountingSink(int64(numLogs))

	service := &logs.Service{
		Source: tailSource,
		Sink:   sink,
	}
	context := context.Background()

	service.Open(context)
	<-sink.DoneChan()
	service.Close()
}

func BenchmarkTailSource(b *testing.B) {
	numLogs := 1000

	testDir := b.TempDir()
	testFile := filepath.Join(testDir, "test.log")
	generateLogs(b, testFile, numLogs, time.Now(), time.Millisecond*10)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		tailSource, err := NewTailSource(TailSourceConfig{
			StaticTargets: []FileTailTarget{
				{
					FilePath: testFile,
					LogType:  LogTypeDocker,
				},
			},
			CursorDirectory: b.TempDir(),
		})
		require.NoError(b, err)
		sink := sinks.NewCountingSink(int64(numLogs))

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
