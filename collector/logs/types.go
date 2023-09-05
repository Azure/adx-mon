package logs

import (
	"context"
	"fmt"
)

// Log represents a single log entry
type Log struct {
	// Timestamp of the event in nanoseconds since the unix epoch
	Timestamp int64
	// Timestamp when this event was ingested in nanoseconds since the unix epoch
	ObservedTimestamp int64

	// Body of the log entry
	Body map[string]interface{}

	//Message  string
	Attributes map[string]string
}

func (l *Log) String() string {
	return fmt.Sprintf("timestamp: %d observedtimestamp: %d labels: %v Body: %v", l.Timestamp, l.ObservedTimestamp, l.Attributes, l.Body)
}

type LogBatch struct {
	Logs []*Log
}

func (l *LogBatch) String() string {
	return fmt.Sprintf("logs: %v", l.Logs)
}

type Transformer interface {
	Open(context.Context) error
	Transform(context.Context, *LogBatch) ([]*LogBatch, error)
	Close() error
}
