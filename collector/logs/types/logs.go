package types

import "sync/atomic"

// LogLiteral is a struct that is easily converted to Log.
type LogLiteral struct {
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
}

func (l *LogLiteral) ToLog() *Log {
	return &Log{
		timestamp:         l.Timestamp,
		observedTimestamp: l.ObservedTimestamp,
		body:              l.Body,
		attributes:        l.Attributes,
		resource:          l.Resource,
		frozen:            0,
	}
}

// Log represents a single log entry
// It is not safe for concurrent updates
// It is safe to read from multiple goroutines after all modifications have been completed.
// Freeze is best effort and should be used to indicate that the log is no longer being modified.
type Log struct {
	// Timestamp of the event in nanoseconds since the unix epoch
	timestamp uint64
	// Timestamp when this event was ingested in nanoseconds since the unix epoch
	observedTimestamp uint64

	// body of the log entry
	body map[string]any

	// attributes of the log entry, not included in the log body.
	attributes map[string]any

	// resource that has collected the log
	resource map[string]any

	// frozen tracks if this object should not be modified
	// updated atomically
	frozen uint32
}

func NewLog() *Log {
	return &Log{
		body:       map[string]any{},
		attributes: map[string]any{},
		resource:   map[string]any{},
		frozen:     0,
	}
}

func (l *Log) Reset() {
	clear(l.body)
	clear(l.attributes)
	clear(l.resource)
	atomic.StoreUint32(&l.frozen, 0)
}

// Copy returns a distinct copy of the log. This is useful for splitting logs.
func (l *Log) Copy() *Log {
	copy := LogPool.Get(1).(*Log)
	copy.Reset()
	copy.timestamp = l.timestamp
	copy.observedTimestamp = l.observedTimestamp
	for k, v := range l.attributes {
		copy.attributes[k] = v
	}
	for k, v := range l.body {
		copy.body[k] = v
	}
	for k, v := range l.resource {
		copy.resource[k] = v
	}
	return copy
}

func (l *Log) SetTimestamp(ts uint64) {
	if atomic.LoadUint32(&l.frozen) == 1 {
		panic("log is frozen")
	}
	l.timestamp = ts
}
func (l *Log) SetObservedTimestamp(ts uint64) {
	if atomic.LoadUint32(&l.frozen) == 1 {
		panic("log is frozen")
	}
	l.observedTimestamp = ts
}
func (l *Log) SetAttributeValue(key string, value any) {
	if atomic.LoadUint32(&l.frozen) == 1 {
		panic("log is frozen")
	}
	l.attributes[key] = value
}
func (l *Log) SetBodyValue(key string, value any) {
	if atomic.LoadUint32(&l.frozen) == 1 {
		panic("log is frozen")
	}
	l.body[key] = value
}
func (l *Log) SetResourceValue(key string, value any) {
	if atomic.LoadUint32(&l.frozen) == 1 {
		panic("log is frozen")
	}
	l.resource[key] = value
}

func (l *Log) Freeze() {
	atomic.StoreUint32(&l.frozen, 1)
}

func (l *Log) GetTimestamp() uint64 {
	return l.timestamp
}

func (l *Log) GetObservedTimestamp() uint64 {
	return l.observedTimestamp
}

func (l *Log) GetAttributeValue(key string) (any, bool) {
	v, ok := l.attributes[key]
	return v, ok
}

func (l *Log) GetBodyValue(key string) (any, bool) {
	v, ok := l.body[key]
	return v, ok
}

func (l *Log) GetResourceValue(key string) (any, bool) {
	v, ok := l.resource[key]
	return v, ok
}

func (l *Log) ForEachAttribute(f func(string, any) error) error {
	for k, v := range l.attributes {
		if err := f(k, v); err != nil {
			return err
		}
	}
	return nil
}

func (l *Log) ForEachBody(f func(string, any) error) error {
	for k, v := range l.body {
		if err := f(k, v); err != nil {
			return err
		}
	}
	return nil
}

func (l *Log) ForEachResource(f func(string, any) error) error {
	for k, v := range l.resource {
		if err := f(k, v); err != nil {
			return err
		}
	}
	return nil
}

func (l *Log) AttributeLen() int {
	return len(l.attributes)
}

func (l *Log) BodyLen() int {
	return len(l.body)
}

func (l *Log) ResourceLen() int {
	return len(l.resource)
}

// GetAttributes returns a copy of the attributes map for read-only access
func (l *Log) GetAttributes() map[string]any {
	result := make(map[string]any, len(l.attributes))
	for k, v := range l.attributes {
		result[k] = v
	}
	return result
}

// GetBody returns a copy of the body map for read-only access
func (l *Log) GetBody() map[string]any {
	result := make(map[string]any, len(l.body))
	for k, v := range l.body {
		result[k] = v
	}
	return result
}

// GetResource returns a copy of the resource map for read-only access
func (l *Log) GetResource() map[string]any {
	result := make(map[string]any, len(l.resource))
	for k, v := range l.resource {
		result[k] = v
	}
	return result
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
	GetAttributes() map[string]any
	GetBody() map[string]any
	GetResource() map[string]any
}

// LogBatch represents a batch of logs
type LogBatch struct {
	Logs []*Log
	Ack  func()
}

func (l *LogBatch) AddLiterals(literals []*LogLiteral) {
	for _, literal := range literals {
		log := literal.ToLog()
		l.Logs = append(l.Logs, log)
	}
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
