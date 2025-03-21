package debug

import "io"

// DebugWriter is an interface that defines a method for writing debug information.  Implementers
// should write a textual representation of their state to the provided io.Writer.
type DebugWriter interface {
	WriteDebug(w io.Writer) error
}
