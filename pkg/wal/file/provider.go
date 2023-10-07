package file

import "os"

// Provider is an interface for creating and opening Files.
type Provider interface {
	Create(name string) (File, error)
	OpenFile(name string, flag int, perm os.FileMode) (File, error)
	Open(name string) (File, error)
}
