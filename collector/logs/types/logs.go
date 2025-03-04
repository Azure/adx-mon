package types

import "sync/atomic"

// Log represents a single log entry
// It is not safe for concurrent updates
// It is safe to read from multiple goroutines after all modifications have been completed.
// Freeze is best effort and should be used to indicate that the log is no longer being modified.
type Log struct {
	// Timestamp of the event in nanoseconds since the unix epoch
	Timestamp uint64
	// Timestamp when this event was ingested in nanoseconds since the unix epoch
	ObservedTimestamp uint64

	// Body of the log entry
	Body map[string]any

	// Attributes of the log entry, not included in the log body.
	Attributes map[string]any

	// Resource that has collected the log
	Resource map[string]any

	// frozen tracks if this object should not be modified
	// updated atomically
	frozen uint32
}

func NewLog() *Log {
	return &Log{
		Body:       map[string]any{},
		Attributes: map[string]any{},
		Resource:   map[string]any{},
		frozen:     0,
	}
}

func (l *Log) Reset() {
	clear(l.Body)
	clear(l.Attributes)
	clear(l.Resource)
	atomic.StoreUint32(&l.frozen, 0)
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
	for k, v := range l.Resource {
		copy.Resource[k] = v
	}
	return copy
}

func (l *Log) SetTimestamp(ts uint64) {
	if atomic.LoadUint32(&l.frozen) == 1 {
		panic("log is frozen")
	}
	l.Timestamp = ts
}
func (l *Log) SetObservedTimestamp(ts uint64) {
	if atomic.LoadUint32(&l.frozen) == 1 {
		panic("log is frozen")
	}
	l.ObservedTimestamp = ts
}
func (l *Log) SetAttributeValue(key string, value any) {
	if atomic.LoadUint32(&l.frozen) == 1 {
		panic("log is frozen")
	}
	l.Attributes[key] = value
}
func (l *Log) SetBodyValue(key string, value any) {
	if atomic.LoadUint32(&l.frozen) == 1 {
		panic("log is frozen")
	}
	l.Body[key] = value
}
func (l *Log) SetResourcevalue(key string, value any) {
	if atomic.LoadUint32(&l.frozen) == 1 {
		panic("log is frozen")
	}
	l.Resource[key] = value
}

func (l *Log) Freeze() {
	atomic.StoreUint32(&l.frozen, 1)
}

func (l *Log) GetTimestamp() uint64 {
	return l.Timestamp
}

func (l *Log) GetObservedTimestamp() uint64 {
	return l.ObservedTimestamp
}

func (l *Log) GetAttributeValue(key string) (any, bool) {
	v, ok := l.Attributes[key]
	return v, ok
}

func (l *Log) GetBodyValue(key string) (any, bool) {
	v, ok := l.Body[key]
	return v, ok
}

func (l *Log) GetResourceValue(key string) (any, bool) {
	v, ok := l.Resource[key]
	return v, ok
}

func (l *Log) ForEachAttribute(f func(string, any) error) error {
	for k, v := range l.Attributes {
		if err := f(k, v); err != nil {
			return err
		}
	}
	return nil
}

func (l *Log) ForEachBody(f func(string, any) error) error {
	for k, v := range l.Body {
		if err := f(k, v); err != nil {
			return err
		}
	}
	return nil
}

func (l *Log) ForEachResource(f func(string, any) error) error {
	for k, v := range l.Resource {
		if err := f(k, v); err != nil {
			return err
		}
	}
	return nil
}

func (l *Log) AttributeLen() int {
	return len(l.Attributes)
}

func (l *Log) BodyLen() int {
	return len(l.Body)
}

func (l *Log) ResourceLen() int {
	return len(l.Resource)
}

// ROLog is a read-only view of a log
// Do not modify any values stored within attributes, body, or resource unless copied
type ROLog interface {
	GetTimestamp() uint64
	GetObservedTimestamp() uint64
	GetAttributeValue(string) (any, bool)
	GetBodyValue(string) (any, bool)
	GetResourceValue(string) (any, bool)
	ForEachAttribute(func(string, any) error) error
	ForEachBody(func(string, any) error) error
	ForEachResource(func(string, any) error) error
	AttributeLen() int
	BodyLen() int
	ResourceLen() int
	Copy() *Log
}

// LogBatch represents a batch of logs
type LogBatch struct {
	Logs []*Log
	Ack  func()
}

func (l *LogBatch) Reset() {
	for i := range l.Logs {
		l.Logs[i] = nil
	}
	l.Logs = l.Logs[:0]
	l.Ack = noop
}

func (l *LogBatch) ForEach(f func(ROLog)) {
	for _, log := range l.Logs {
		f(log)
	}
}

type ROLogBatch interface {
	ForEach(func(ROLog))
}

func noop() {}
