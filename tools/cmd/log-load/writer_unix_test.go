//go:build unix

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestFileSetupRejectsFIFOWithoutReader(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		for _, index := range []int{0, 1} {
			t.Run("symlink="+strconv.FormatBool(symlink)+"/writer="+strconv.Itoa(index), func(t *testing.T) {
				directory := t.TempDir()
				fifo := writerPath(directory, index)
				if symlink {
					fifo = filepath.Join(directory, "blocked.fifo")
				}
				if err := unix.Mkfifo(fifo, 0600); err != nil {
					if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EOPNOTSUPP) {
						t.Skipf("FIFOs unavailable: %v", err)
					}
					t.Fatal(err)
				}
				if symlink {
					if err := os.Symlink(fifo, writerPath(directory, index)); err != nil {
						t.Fatal(err)
					}
				}
				type setupResult struct {
					writers []batchWriter
					err     error
				}
				done := make(chan setupResult, 1)
				go func() {
					writers, err := openWriters(destination{directory: directory}, 3)
					done <- setupResult{writers: writers, err: err}
				}()
				var result setupResult
				select {
				case result = <-done:
				case <-time.After(5 * time.Second):
					// Unblock a regressed blocking open so the test does not leak
					// a stuck goroutine or descriptor after reporting the failure.
					fd, err := unix.Open(fifo, unix.O_RDONLY|unix.O_NONBLOCK, 0)
					if err != nil {
						t.Fatalf("writer setup blocked; could not unblock FIFO: %v", err)
					}
					defer unix.Close(fd)
					select {
					case result = <-done:
						for _, writer := range result.writers {
							_, _ = writer.Close()
						}
					case <-time.After(5 * time.Second):
						t.Fatal("writer setup remained blocked after opening FIFO reader")
					}
					t.Fatal("writer setup blocked opening FIFO without a reader")
				}
				for _, writer := range result.writers {
					_, _ = writer.Close()
				}
				if result.err == nil || !strings.Contains(result.err.Error(), "regular file") {
					t.Fatalf("expected nonregular writer rejection: %v", result.err)
				}
				if _, err := os.Stat(writerPath(directory, index+1)); !os.IsNotExist(err) {
					t.Fatalf("writer setup continued after rejecting FIFO: %v", err)
				}
			})
		}
	}
}
