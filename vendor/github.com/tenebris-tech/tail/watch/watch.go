// Copyright (c) 2015 HPE Software Inc. All rights reserved.
// Copyright (c) 2013 ActiveState Software Inc. All rights reserved.

package watch

import (
	"io/fs"

	"gopkg.in/tomb.v1"
)

// FileWatcher monitors file-level events.
type FileWatcher interface {
	// BlockUntilExists blocks until the file comes into existence.
	BlockUntilExists(*tomb.Tomb) error

	BlockUntilEvent(*tomb.Tomb, fs.FileInfo, int64) (ChangeType, error) //TODO take context
}
