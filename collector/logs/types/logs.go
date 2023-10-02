package types

// Log represents a single log entry
type Log struct {
	// Timestamp of the event in nanoseconds since the unix epoch
	Timestamp uint64
	// Timestamp when this event was ingested in nanoseconds since the unix epoch
	ObservedTimestamp uint64

	// Body of the log entry
	Body map[string]any

	// Attributes of the log entry, not included in the log body.
	Attributes map[string]any
}

func NewLog() *Log {
	return &Log{
		Body:       map[string]any{},
		Attributes: map[string]any{},
	}
}

func (l *Log) Reset() {
	clear(l.Body)
	clear(l.Attributes)
}

// Copy returns a distinct copy of the log. This is useful for splitting logs.
func (l *Log) Copy() *Log {
	copy := LogPool.Get(1).(*Log)
	copy.Reset()
	copy.Timestamp = l.Timestamp
	copy.ObservedTimestamp = l.ObservedTimestamp
	for k, v := range l.Attributes {
		copy.Attributes[k] = v
	}
	for k, v := range l.Body {
		copy.Body[k] = v
	}
	return copy
}

// LogBatch represents a batch of logs
type LogBatch struct {
	Logs []*Log
}

func (l *LogBatch) Reset() {
	for i := range l.Logs {
		l.Logs[i] = nil
	}
	l.Logs = l.Logs[:0]
}
