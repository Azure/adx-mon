// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package channel

import "context"

// RecvWithContext tries to receive a value from a channel which is aborted if the context is canceled.
//
// Function returns true if the value was received, false if the context was canceled or the channel was closed.
//
// Deprecated: use plain old select instead.
func RecvWithContext[T any](ctx context.Context, ch <-chan T) (T, bool) {
	select {
	case <-ctx.Done():
		var zero T

		return zero, false
	case val, ok := <-ch:
		if !ok {
			return val, false
		}

		return val, true
	}
}

// RecvState is the state of a channel after receiving a value.
type RecvState int

const (
	// StateRecv means that a value was received from the channel.
	StateRecv RecvState = iota
	// StateEmpty means that the channel was empty.
	StateEmpty
	// StateClosed means that the channel was closed.
	StateClosed
)

// TryRecv tries to receive a value from a channel.
//
// Function returns the value and the state of the channel.
func TryRecv[T any](ch <-chan T) (T, RecvState) {
	var zero T

	select {
	case val, ok := <-ch:
		if !ok {
			return zero, StateClosed
		}

		return val, StateRecv
	default:
		return zero, StateEmpty
	}
}
