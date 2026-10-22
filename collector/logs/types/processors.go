package types

import "context"

// Source is a component that produces *LogBatch instances.
type Source interface {
	Open(context.Context) error
	Close() error
	Name() string
}

// Transformer is a component that transforms a LogBatch. Transform is potentially called by multiple goroutines concurrently.
type Transformer interface {
	Open(context.Context) error
	Transform(context.Context, *LogBatch) (*LogBatch, error)
	Close() error
	Name() string
}

// Sink is a component that receives a LogBatch. Send is potentially called by multiple goroutines concurrently.
type Sink interface {
	Open(context.Context) error
	Send(context.Context, *LogBatch) error
	Close() error
	Name() string
}

// DurableSink is implemented by sinks whose acceptance determines the fate of
// the data. A failure from a sink that is not durable must not make a producer
// resend records that a durable sink already stored.
type DurableSink interface {
	Sink
	Durable() bool
}

// IsDurable reports whether a sink owns the durability of the data it receives.
func IsDurable(s Sink) bool {
	d, ok := s.(DurableSink)
	return ok && d.Durable()
}
