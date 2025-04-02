// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package channel provides generic operations on channels.
package channel

import "context"

// SendWithContext tries to send a value to a channel which is aborted if the context is canceled.
//
// Function returns true if the value was sent, false if the context was canceled.
func SendWithContext[T any](ctx context.Context, ch chan<- T, val T) bool {
	select {
	case <-ctx.Done():
		return false
	case ch <- val:
		return true
	}
}
