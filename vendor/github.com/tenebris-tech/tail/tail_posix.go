//go:build linux || darwin || freebsd || netbsd || openbsd

package tail

import (
	"fmt"
	"io/fs"
	"os"
	"syscall"
)

func OpenFile(name string) (file *os.File, fileInfo fs.FileInfo, err error) {
	file, err = os.Open(name)
	if err != nil {
		return nil, nil, err
	}

	fileInfo, err = file.Stat()
	if err != nil {
		return nil, nil, err
	}

	return file, fileInfo, nil
}

func FileIdentifier(fileInfo fs.FileInfo) (string, error) {
	sys, ok := fileInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("failed to get file identifier for %s", fileInfo.Name())
	}
	return fmt.Sprintf("%d:%d", sys.Dev, sys.Ino), nil
}
