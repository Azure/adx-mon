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
