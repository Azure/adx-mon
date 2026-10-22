package service

import (
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWaitForShutdownReturnsAfterShutdownFinishes(t *testing.T) {
	sc := make(chan os.Signal, 1)
	var closed atomic.Bool

	sc <- os.Interrupt
	WaitForShutdown(sc, func(os.Signal) {
		// Simulate a service that takes a while to drain before Close returns.
		time.Sleep(100 * time.Millisecond)
		closed.Store(true)
	})

	require.True(t, closed.Load(), "WaitForShutdown returned before shutdown finished")
}

func TestWaitForShutdownPassesSignal(t *testing.T) {
	sc := make(chan os.Signal, 1)
	var got os.Signal

	sc <- os.Interrupt
	WaitForShutdown(sc, func(sig os.Signal) {
		got = sig
	})

	require.Equal(t, os.Interrupt, got)
}
