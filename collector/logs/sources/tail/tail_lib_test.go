package tail

import (
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/stretchr/testify/require"
	"github.com/tenebris-tech/tail"
	"golang.org/x/sync/errgroup"
)

const MaxFiles = 11
const NumLines = 10000
const MaxSleepNs = 500000 // 0.5 ms
const MinSleepNS = 1000   // 0.001 ms

// Disable Library e2e tests unless we're digging into things.
const EnableLibraryTests = false

// TestLib tests the tail library that we utilize. This runs pretty slowly, but allows us to
// exercise the library against how files are rotated with kubernetes log drivers.
func TestLib(t *testing.T) {
	if !EnableLibraryTests || testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	testDir := t.TempDir()
	testFile := filepath.Join(testDir, "test.log")

	tailer, err := tail.TailFile(testFile, tail.Config{Follow: true, ReOpen: true})
	require.NoError(t, err)
	defer tailer.Cleanup()
	defer tailer.Stop()

	group, ctx := errgroup.WithContext(context.Background())
	counterCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	// Read lines from the tailer
	group.Go(func() error {
		count := 0
		for {
			select {
			case <-counterCtx.Done():
				return fmt.Errorf("tailer exited with count %d: %w", count, counterCtx.Err())
			case line, ok := <-tailer.Lines:
				if !ok {
					return fmt.Errorf("tailer closed the channel: exited with count %d", count)
				}
				if line.Err != nil {
					logger.Errorf("tailer error: %v", line.Err)
					//skip
					continue
				}
				number, err := strconv.Atoi(line.Text)
				require.NoError(t, err)
				// Ensure lines are in order.
				// Text starts from 1, but count starts from 0
				require.Equal(t, count+1, number)
				count++
				if count == NumLines {
					return nil
				}
				// Add a bit of jitter to log reads
				pause(t)
			}
		}
	})

	// Write the log files
	group.Go(func() error {
		writeLogsToFiles(t, testFile)
		return nil
	})

	err = group.Wait()
	require.NoError(t, err)
}

func pause(t *testing.T) {
	t.Helper()
	sleepTime, err := rand.Int(rand.Reader, big.NewInt(MaxSleepNs-MinSleepNS))
	require.NoError(t, err)
	time.Sleep(time.Duration(sleepTime.Int64()+MinSleepNS) * time.Nanosecond)
}

// writeLogsToFiles simulates a log driver that does the following operations:
// 1. Opens/Truncates an initial file
// 2. Writes to this file until it reaches a max size (or max number of lines for this test)
// 3. Shifts all old files by one and renames the file to <filename>.1
// 4. Compresses the file that was just rotated
// 5. Opens the same file with truncation mode and writes to it
func writeLogsToFiles(t *testing.T, filename string) {
	t.Helper()

	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	for i := 1; i <= NumLines; i++ {
		if i%1000 == 0 {
			f.Sync()
			f.Close()
			rotate(filename, MaxFiles, true)

			compressFile(t, fmt.Sprintf("%s.1", filename), time.Now())
			f, err = os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			require.NoError(t, err)
		}
		_, err := f.WriteString(fmt.Sprintf("%d\n", i))
		require.NoError(t, err)

		// Add a bit of jitter to log writes
		pause(t)
	}
	f.Close()
	t.Log(time.Now(), "Done writing logs")
}

func rotate(name string, maxFiles int, compress bool) error {
	if maxFiles < 2 {
		return nil
	}

	var extension string
	if compress {
		extension = ".gz"
	}

	lastFile := fmt.Sprintf("%s.%d%s", name, maxFiles-1, extension)
	err := os.Remove(lastFile)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("error removing oldest log file: %w", err)
	}

	for i := maxFiles - 1; i > 1; i-- {
		toPath := name + "." + strconv.Itoa(i) + extension
		fromPath := name + "." + strconv.Itoa(i-1) + extension
		if err := os.Rename(fromPath, toPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	if err := os.Rename(name, name+".1"); err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}

type rotateFileMetadata struct {
	LastTime time.Time `json:"lastTime,omitempty"`
}

func compressFile(t *testing.T, fileName string, lastTimestamp time.Time) {
	t.Helper()
	file, err := os.Open(fileName)
	if err != nil {
		require.NoError(t, err)
		return
	}
	defer func() {
		file.Close()
		err := os.Remove(fileName)
		if err != nil {
			require.NoError(t, err)
		}
	}()

	outFile, err := os.OpenFile(fileName+".gz", os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0640)
	if err != nil {
		require.NoError(t, err)
		return
	}
	defer func() {
		outFile.Close()
		if err != nil {
			os.Remove(fileName + ".gz")
		}
	}()

	compressWriter := gzip.NewWriter(outFile)
	defer compressWriter.Close()

	// Add the last log entry timestamp to the gzip header
	extra := rotateFileMetadata{}
	extra.LastTime = lastTimestamp
	compressWriter.Header.Extra, err = json.Marshal(&extra)
	if err != nil {
		require.NoError(t, err)
	}

	_, err = io.Copy(compressWriter, file)
	if err != nil {
		require.NoError(t, err)
		return
	}
}
