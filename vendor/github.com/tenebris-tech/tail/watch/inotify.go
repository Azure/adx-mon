// Copyright (c) 2015 HPE Software Inc. All rights reserved.
// Copyright (c) 2013 ActiveState Software Inc. All rights reserved.

package watch

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/tenebris-tech/tail/logging"
	"gopkg.in/tomb.v1"
)

// InotifyFileWatcher uses inotify to monitor file changes.
type InotifyFileWatcher struct {
	Filename string
	logger   logging.Logger
}

func NewInotifyFileWatcher(filename string, logger logging.Logger) *InotifyFileWatcher {
	fw := &InotifyFileWatcher{filepath.Clean(filename), logger}
	return fw
}

func (fw *InotifyFileWatcher) BlockUntilExists(t *tomb.Tomb) error {
	// On Linux, we cannot use inotify on files that do not exist. We also cannot just watch the parent
	// directory in file-rotation cases if we are watching a symlink, since the symlink we are "watching"
	// does not change, but the results of os.Stat does change because it follows links.
	// Once the file exists, we can use inotify for other events because it follows links.

	// Instead, just do a blocking check every POLL_DURATION until the file exists.
	for {
		if _, err := os.Stat(fw.Filename); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
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

func (fw *InotifyFileWatcher) BlockUntilEvent(t *tomb.Tomb, openedFileInfo fs.FileInfo, pos int64) (ChangeType, error) {
	// We create new watchers and watches for each call of BlockUntilEvent. This avoids needing the maintain a separate loop
	// to maintain the watches and to recreate watches when files are rotated or removed, which is required with inotify on Linux.
	// This also ensures that we more closely report the status of the file instance that is being consumed by tail, instead of
	// maintaining a separate state in this watcher that may be skewed.
	for {
		changeType, err := fw.watchForEvents(t, openedFileInfo, pos)
		if err != nil {
			if err == tomb.ErrDying {
				return None, err
			} else if errors.Is(err, fsnotify.ErrEventOverflow) {
				continue // Recreate any watches and try again.
			}
			return None, err
		}

		return changeType, nil
	}
}

func (fw *InotifyFileWatcher) watchForEvents(t *tomb.Tomb, openedFileInfo fs.FileInfo, pos int64) (ChangeType, error) {
	// Create new watcher for each iteration in case of errors.
	// On Linux, in cases of errors such as overflow, we need to close the inotify FD and create a new one.
	// See https://lwn.net/Articles/605128/
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return None, err
	}
	defer watcher.Close()

	err = watcher.Add(fw.Filename)
	if err != nil {
		return None, err
	}
	defer watcher.Remove(fw.Filename)

	// Use stat to check for changes between when we start setting up the watches and when the watch is active.
	// If there are changes, we can return immediately.
	changeType, err := StatChanges(fw.Filename, openedFileInfo, pos, fw.logger)
	if err != nil {
		return None, err
	}
	if changeType != None {
		return changeType, nil
	}
	for {
		select {
		case evt, ok := <-watcher.Events:
			if !ok {
				return None, tomb.ErrDying
			}

			changeType, err = fw.handleEvent(evt, openedFileInfo, pos)
			if err != nil {
				return None, err
			}
			if changeType != None {
				return changeType, nil
			}
		case err = <-watcher.Errors:
			return None, err // Need to reset and recreate any watches. Bail out here.
		case <-t.Dying():
			return None, tomb.ErrDying
		}
	}
}

func (fw *InotifyFileWatcher) handleEvent(evt fsnotify.Event, openedFileInfo fs.FileInfo, prevSize int64) (ChangeType, error) {
	// Order is important here.
	// If a file is removed or renamed, we want to report a delete instead of any other write or chmod that may also be present in the event
	// so that the consumer will consume to the end, then rotate to the new file so we don't lose this event.
	if evt.Has(fsnotify.Remove) || evt.Has(fsnotify.Rename) {
		return Deleted, nil
	}

	if evt.Has(fsnotify.Write) {
		return Modified, nil
	}

	// With an open FD, unlink(fd) - inotify returns IN_ATTRIB (==fsnotify.Chmod) on linux
	if evt.Has(fsnotify.Chmod) {
		changeType, err := StatChanges(fw.Filename, openedFileInfo, prevSize, fw.logger)
		if err != nil {
			return None, err
		}
		return changeType, nil
	}

	return None, nil
}
