// Copyright (c) 2015 HPE Software Inc. All rights reserved.
// Copyright (c) 2013 ActiveState Software Inc. All rights reserved.

package watch

import (
	"errors"
	"io/fs"
	"os"
	"runtime"
	"time"

	"github.com/tenebris-tech/tail/logging"
	"gopkg.in/tomb.v1"
)

// PollingFileWatcher polls the file for changes.
type PollingFileWatcher struct {
	Filename string
	Size     int64
	logger   logging.Logger
}

func NewPollingFileWatcher(filename string, logger logging.Logger) *PollingFileWatcher {
	fw := &PollingFileWatcher{filename, 0, logger}
	return fw
}

var POLL_DURATION time.Duration

func (fw *PollingFileWatcher) BlockUntilExists(t *tomb.Tomb) error {
	for {
		if _, err := os.Stat(fw.Filename); err == nil {
			return nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		select {
		case <-time.After(POLL_DURATION):
			continue
		case <-t.Dying():
			return tomb.ErrDying
		}
	}
}

func (fw *PollingFileWatcher) BlockUntilEvent(t *tomb.Tomb, openedFileInfo fs.FileInfo, pos int64) (ChangeType, error) {
	for {
		changeType, err := StatChanges(fw.Filename, openedFileInfo, pos, fw.logger)
		if err != nil {
			return None, err
		}
		if changeType != None {
			return changeType, nil
		}

		select {
		case <-time.After(POLL_DURATION):
			continue
		case <-t.Dying():
			return None, tomb.ErrDying
		}
	}
}

func StatChanges(filename string, openedFileInfo fs.FileInfo, pos int64, logger logging.Logger) (ChangeType, error) {
	fi, err := os.Stat(filename)
	if err != nil {
		// Windows cannot delete a file if a handle is still open (tail keeps one open)
		// so it gives access denied to anything trying to read it until all handles are released.
		if errors.Is(err, fs.ErrNotExist) || (runtime.GOOS == "windows" && os.IsPermission(err)) {
			// logger.Printf("notexist deleted\n")
			return Deleted, nil
		}
		return None, err
	}

	if !os.SameFile(openedFileInfo, fi) {
		// logger.Printf("notsamefile deleted\n")
		return Deleted, nil
	}

	if fi.Size() > pos {
		return Modified, nil
	} else if fi.Size() < pos {
		return Truncated, nil
	}

	return None, nil
}

func init() {
	POLL_DURATION = 250 * time.Millisecond
}
