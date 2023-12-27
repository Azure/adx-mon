//go:build linux || darwin || freebsd || netbsd || openbsd

package tail

import (
	"fmt"
	"os"
	"syscall"
)

func OpenFile(name string) (file *os.File, fileIdentifier string, err error) {
	file, err = os.Open(name)
	if err != nil {
		return nil, "", err
	}

	fileInfo, err := file.Stat()
	if err != nil {
		return nil, "", err
	}

	sys, ok := fileInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, "", fmt.Errorf("failed to get file identifier for %s", name)
	}
	return file, fmt.Sprintf("%d:%d", sys.Dev, sys.Ino), nil
}
