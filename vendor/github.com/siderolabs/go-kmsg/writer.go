// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package kmsg

import (
	"bytes"
	"io"
)

// MaxLineLength to be passed to kmsg, see https://github.com/torvalds/linux/blob/master/kernel/printk/printk.c#L450.
const MaxLineLength = 1024 - 48

// Writer ensures writes by line and limits each line to maxLineLength characters.
//
// This workarounds kmsg limits.
type Writer struct {
	KmsgWriter io.Writer
}

// Write implements io.Writer interface.
func (w *Writer) Write(p []byte) (n int, err error) {
	// split writes by `\n`, and limit each line to MaxLineLength
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		if i == -1 {
			i = len(p) - 1
		}

		line := p[:i+1]
		if len(line) > MaxLineLength {
			line = append(line[:MaxLineLength-4], []byte("...\n")...)
		}

		// replace tab characters from multierror library error messages with spaces,
		// as tabs are not visible in the console
		line = bytes.ReplaceAll(line, []byte{'\t'}, []byte{' '})

		var nn int
		nn, err = w.KmsgWriter.Write(line)

		if nn == len(line) {
			n += i + 1
		} else {
			n += nn
		}

		if err != nil {
			return n, err
		}

		p = p[i+1:]
	}

	return n, err
}
