//go:build windows

package tail

import (
	"os"

	"github.com/tenebris-tech/tail/winfile"
)

func OpenFile(name string) (file *os.File, fileIdentifier string, err error) {
	file, err = winfile.OpenFile(name, os.O_RDONLY, 0)

	// TODO - use windows.GetFileInformationByHandle to get the file identifier based on volume, and fileId
	return file, "", err
}
