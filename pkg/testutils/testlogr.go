// Package testutils provides utilities for testing across the project.
//
// testlogr.go provides a logr.Logger implementation that writes to testing.T,
// making it suitable for use with controller-runtime's log.SetLogger in tests.
// This allows controller-runtime logs to be captured in test output via t.Log.
package testutils

import (
	"fmt"
	"testing"

	"github.com/go-logr/logr"
)

// NewTestLogger returns a logr.Logger that writes all log output to t.Log.
// This is useful for controller-runtime tests, so logs are synchronized with test output.
func NewTestLogger(t *testing.T) logr.Logger {
	return logr.New(&testLogSink{t: t})
}

type testLogSink struct {
	t *testing.T
}

func (l *testLogSink) Init(info logr.RuntimeInfo) {}
func (l *testLogSink) Enabled(level int) bool     { return true }
func (l *testLogSink) Info(level int, msg string, keysAndValues ...interface{}) {
	if len(keysAndValues) > 0 {
		msg = msg + " " + fmt.Sprint(keysAndValues...)
	}
	l.t.Log("[controller-runtime]", msg)
}
func (l *testLogSink) Error(err error, msg string, keysAndValues ...interface{}) {
	full := msg
	if err != nil {
		full = full + ": " + err.Error()
	}
	if len(keysAndValues) > 0 {
		full = full + " " + fmt.Sprint(keysAndValues...)
	}
	l.t.Log("[controller-runtime][ERROR]", full)
}
func (l *testLogSink) WithValues(keysAndValues ...interface{}) logr.LogSink { return l }
func (l *testLogSink) WithName(name string) logr.LogSink                    { return l }
