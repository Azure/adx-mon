// Copyright (c) 2015 HPE Software Inc. All rights reserved.
// Copyright (c) 2013 ActiveState Software Inc. All rights reserved.
// Copyright (c) Microsoft Corporation.

package tail

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/tenebris-tech/tail/logging"
	"github.com/tenebris-tech/tail/ratelimiter"
	"github.com/tenebris-tech/tail/util"
	"github.com/tenebris-tech/tail/watch"

	"gopkg.in/tomb.v1"
)

var (
	ErrStop = errors.New("tail should now stop")
)

type Line struct {
	Text           string
	Time           time.Time
	Err            error  // Error from tail
	Offset         int64  // Offset of the beginning of the line in the file
	FileIdentifier string // unique identifier for the current file - OS specific
}

// SeekInfo represents arguments to `os.Seek`
type SeekInfo struct {
	Offset int64
	Whence int // os.SEEK_*

	// FileIdentifier is an optional string to define the opaque identifier for the file offset.
	// This allows only seeking if reading the same file as before.
	// Populate using a value generated from Line.FileIdentifier.
	FileIdentifier string
}

// Config is used to specify how a file must be tailed.
type Config struct {
	// File-specifc
	Location    *SeekInfo // Seek to this location before tailing
	ReOpen      bool      // Reopen recreated files (tail -F)
	MustExist   bool      // Fail early if the file does not exist
	Poll        bool      // Poll for file changes instead of using inotify
	Pipe        bool      // Is a named pipe (mkfifo)
	RateLimiter *ratelimiter.LeakyBucket

	// Generic IO
	Follow      bool // Continue looking for new lines (tail -f)
	MaxLineSize int  // If non-zero, split longer lines into multiple lines

	// Logger, when nil, is set to tail.DefaultLogger
	// To disable logging: set field to tail.DiscardingLogger
	Logger logging.Logger
}

type Tail struct {
	Filename string
	Lines    chan *Line
	Config

	file           *os.File
	reader         *bufio.Reader
	fileInfo       fs.FileInfo
	fileIdentifier string // unique identifier for the current file - OS specific
	offset         int64

	watcher     watch.FileWatcher
	changes     *watch.FileChanges
	needsReopen bool

	tomb.Tomb // provides: Done, Kill, Dying

	lk sync.Mutex
}

var (
	// DefaultLogger is used when Config.Logger == nil
	DefaultLogger = log.New(os.Stderr, "", log.LstdFlags)
	// DiscardingLogger can be used to disable logging output
	DiscardingLogger = log.New(io.Discard, "", 0)
)

// TailFile begins tailing the file. Output stream is made available
// via the `Tail.Lines` channel. To handle errors during tailing,
// invoke the `Wait` or `Err` method after finishing reading from the
// `Lines` channel.
func TailFile(filename string, config Config) (*Tail, error) {
	if config.ReOpen && !config.Follow {
		util.Fatal("cannot set ReOpen without Follow.")
	}

	t := &Tail{
		Filename: filename,
		Lines:    make(chan *Line),
		Config:   config,
	}

	// when Logger was not specified in config, use default logger
	if t.Logger == nil {
		t.Logger = log.New(os.Stderr, "", log.LstdFlags)
	}

	if t.Poll {
		t.watcher = watch.NewPollingFileWatcher(filename, t.Logger)
	} else {
		t.watcher = watch.NewInotifyFileWatcher(filename, t.Logger)
	}

	if t.MustExist {
		var err error
		t.file, t.fileInfo, err = OpenFile(t.Filename)
		if err != nil {
			return nil, err
		}
		t.fileIdentifier, err = FileIdentifier(t.fileInfo)
		if err != nil {
			return nil, err
		}
		t.openReader()
	}

	go t.tailFileSync()

	return t, nil
}

// Return the file's current position, like stdio's ftell().
// But this value is not very accurate.
// it may readed one line in the chan(tail.Lines),
// so it may lost one line.
func (tail *Tail) Tell() (offset int64, err error) {
	if tail.file == nil {
		return
	}
	offset, err = tail.file.Seek(0, io.SeekCurrent)
	if err != nil {
		return
	}

	tail.lk.Lock()
	defer tail.lk.Unlock()
	if tail.reader == nil {
		return
	}

	offset -= int64(tail.reader.Buffered())
	return
}

// Stop stops the tailing activity.
func (tail *Tail) Stop() error {
	tail.Kill(nil)
	return tail.Wait()
}

// StopAtEOF stops tailing as soon as the end of the file is reached.
func (tail *Tail) StopAtEOF() error {
	tail.Kill(errStopAtEOF)
	return tail.Wait()
}

var errStopAtEOF = errors.New("tail: stop at eof")

func (tail *Tail) close() {
	close(tail.Lines)
	tail.closeFile()
}

func (tail *Tail) closeFile() {
	if tail.file != nil {
		_ = tail.file.Close()
		tail.file = nil
	}
}

func (tail *Tail) reopen() error {
	tail.Logger.Printf("Attempting to reopen %s ...", tail.Filename)
	tail.closeFile()
	for {
		var err error
		tail.file, tail.fileInfo, err = OpenFile(tail.Filename)
		if err != nil {
			if os.IsNotExist(err) {
				tail.Logger.Printf("Waiting for %s to appear...", tail.Filename)
				if err := tail.watcher.BlockUntilExists(&tail.Tomb); err != nil {
					if err == tomb.ErrDying {
						return err
					}
					return fmt.Errorf("failed to detect creation of %s: %s", tail.Filename, err)
				}
				continue
			}
			return fmt.Errorf("unable to open file %s: %s", tail.Filename, err)
		}

		tail.Logger.Printf("Successfully reopened %s", tail.Filename)
		tail.fileIdentifier, err = FileIdentifier(tail.fileInfo)
		if err != nil {
			return fmt.Errorf("get FileIdentifier: %w", err)
		}
		tail.openReader()
		break
	}
	return nil
}

func (tail *Tail) readLine() (string, int64, error) {
	tail.lk.Lock()
	line, err := tail.reader.ReadString('\n')
	tail.lk.Unlock()

	read := int64(len(line))
	if err != nil {
		// Note ReadString "returns the data read before the error" in
		// case of an error, including EOF, so we return it as is. The
		// caller is expected to process it if err is EOF.
		return line, read, err
	}

	line = strings.TrimRight(line, "\n")

	return line, read, err
}

func (tail *Tail) tailFileSync() {
	defer tail.Done()
	defer tail.close()

	if !tail.MustExist {
		// deferred first open.
		err := tail.reopen()
		if err != nil {
			if err != tomb.ErrDying {
				tail.Kill(err)
			}
			return
		}
	}

	// Seek to requested location on first open of the file.
	if tail.Location != nil {
		if tail.Location.FileIdentifier == "" || tail.Location.FileIdentifier == tail.fileIdentifier {
			err := tail.seekTo(*tail.Location)
			tail.Logger.Printf("Seeked %s - %+v\n", tail.Filename, tail.Location)
			if err != nil {
				_ = tail.Killf("Seek error on %s: %s", tail.Filename, err)
				return
			}
		} else {
			tail.Logger.Printf("Skipping seek because fileIdentifier %q does not match requested FileIdentifier %q", tail.fileIdentifier, tail.Location.FileIdentifier)
		}
	}

	partialLine := ""
	// Read line by line.
	for {
		line, numRead, err := tail.readLine()
		tail.offset += numRead

		if partialLine != "" {
			line = partialLine + line
			partialLine = ""
		}

		// Process `line` even if err is EOF.
		if err == nil {
			cooloff := !tail.sendLine(line, tail.offset)
			if cooloff {
				// Wait a second before seeking till the end of
				// file when rate limit is reached.
				msg := "too much log activity; waiting a second " +
					"before resuming tailing"
				tail.Lines <- &Line{Text: msg, Time: time.Now(), Err: errors.New(msg)}
				select {
				case <-time.After(time.Second):
				case <-tail.Dying():
					return
				}
				if err := tail.seekEnd(); err != nil {
					tail.Kill(err)
					return
				}
			}
		} else if err == io.EOF {
			// We have consumed to the end and are not following. Flush this last data and exit.
			if !tail.Follow {
				if line != "" {
					tail.sendLine(line, tail.offset)
				}
				return
			}

			if tail.needsReopen {
				// We have consumed to EOF and no more data is coming. Flush this data.
				// waitForChanges will then open the new file, if appropriate.
				if line != "" {
					tail.sendLine(line, tail.offset)
				}
			} else {
				// We have consumed to EOF and are following. Save this line for the next
				// iteration, and wait for more data to become available.
				partialLine = line
			}

			// When EOF is reached, wait for more data to become
			// available. Wait strategy is based on the `tail.watcher`
			// implementation (inotify or polling).
			err := tail.waitForChanges(tail.offset)
			if err != nil {
				if err != ErrStop {
					tail.Kill(err)
				}
				return
			}
		} else {
			// non-EOF error
			_ = tail.Killf("Error reading %s: %s", tail.Filename, err)
			return
		}

		select {
		case <-tail.Dying():
			if tail.Err() == errStopAtEOF {
				continue
			}
			return
		default:
		}
	}
}

// waitForChanges waits until the file has been appended, deleted,
// moved or truncated. When moved or deleted - the file will be
// reopened if ReOpen is true. Truncated files are always reopened.
func (tail *Tail) waitForChanges(offset int64) error {
	if tail.needsReopen {
		tail.needsReopen = false
		if err := tail.reopen(); err != nil {
			return err
		}
		return nil
	}

	change, err := tail.watcher.BlockUntilEvent(&tail.Tomb, tail.fileInfo, offset)
	if err != nil {
		return err
	}
	if change == watch.Modified {
		return nil
	} else if change == watch.Deleted {
		tail.changes = nil
		if tail.ReOpen {
			// Return now to consume any lines that were written before the file was moved or deleted, but after our last reads.
			// After we reach EOF again from our existing open file, we'll reopen the file and continue tailing.
			tail.needsReopen = true
			tail.Logger.Printf("Deferring reopen for deleted/moved file: %s", tail.Filename)
			return nil
		} else {
			tail.Logger.Printf("Stopping tail as file no longer exists: %s", tail.Filename)
			return ErrStop
		}
	} else if change == watch.Truncated {
		// Always reopen truncated files (Follow is true)
		// Return now to consume any lines that were written before the file was considered truncated, but after our last reads.
		// fsnotify will sometimes report a file as truncated even though it was actually moved and replaced with a new file.
		// After we reach EOF again from our existing open file, we'll reopen the file and continue tailing.
		tail.needsReopen = true
		tail.Logger.Printf("Deferring reopen for truncated file: %s", tail.Filename)
		return nil
	}

	return nil // default case
}

func (tail *Tail) openReader() {
	tail.lk.Lock()
	if tail.MaxLineSize > 0 {
		// add 2 to account for newline characters
		tail.reader = bufio.NewReaderSize(tail.file, tail.MaxLineSize+2)
	} else {
		tail.reader = bufio.NewReader(tail.file)
	}
	tail.lk.Unlock()
	tail.offset = 0
}

func (tail *Tail) seekEnd() error {
	return tail.seekTo(SeekInfo{Offset: 0, Whence: io.SeekEnd})
}

func (tail *Tail) seekTo(pos SeekInfo) error {
	_, err := tail.file.Seek(pos.Offset, pos.Whence)
	if err != nil {
		return fmt.Errorf("seek error on %s: %s", tail.Filename, err)
	}
	// Reset the read buffer whenever the file is re-seek'ed
	tail.reader.Reset(tail.file)
	tail.offset = pos.Offset
	return nil
}

// sendLine sends the line(s) to Lines channel, splitting longer lines
// if necessary. Return false if rate limit is reached.
func (tail *Tail) sendLine(line string, offset int64) bool {
	now := time.Now()
	lines := []string{line}

	// Split longer lines
	if tail.MaxLineSize > 0 && len(line) > tail.MaxLineSize {
		lines = util.PartitionString(line, tail.MaxLineSize)
	}

	for _, line := range lines {
		select {
		case <-tail.Dying():
			return true // dying
		case tail.Lines <- &Line{Text: line, Time: now, Err: nil, FileIdentifier: tail.fileIdentifier, Offset: offset}:
		}
	}

	if tail.Config.RateLimiter != nil {
		ok := tail.Config.RateLimiter.Pour(uint16(len(lines)))
		if !ok {
			tail.Logger.Printf("Leaky bucket full (%v); entering 1s cooloff period.\n",
				tail.Filename)
			return false
		}
	}

	return true
}

func (tail *Tail) Cleanup() {
	// no-op for now.
}
