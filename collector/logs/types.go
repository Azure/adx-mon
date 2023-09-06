package logs

import (
	"context"
	"encoding/json"
	"fmt"
)

// Log represents a single log entry
type Log struct {
	// Timestamp of the event in nanoseconds since the unix epoch
	Timestamp int64
	// Timestamp when this event was ingested in nanoseconds since the unix epoch
	ObservedTimestamp int64

	// Body of the log entry
	Body map[string]any

	// Attributes of the log entry, not included in the log body.
	Attributes map[string]any
}

func (l *Log) String() string {
	value, _ := json.Marshal(l)
	return string(value)
}

func (l *Log) Copy() *Log {
	// TODO - what if we are storing pointers?
	attributes := map[string]any{}
	for k, v := range l.Attributes {
		attributes[k] = v
	}
	body := map[string]any{}
	for k, v := range l.Body {
		body[k] = v
	}
	return &Log{
		Timestamp:         l.Timestamp,
		ObservedTimestamp: l.ObservedTimestamp,
		Body:              body,
		Attributes:        attributes,
	}
}

type LogBatch struct {
	Logs []*Log
}

func (l *LogBatch) String() string {
	return fmt.Sprintf("logs: %v", l.Logs)
}

type Transformer interface {
	Open(context.Context) error
	Transform(context.Context, *LogBatch) (*LogBatch, error)
	Close() error
}
