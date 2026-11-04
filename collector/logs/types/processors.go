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

type storageWaitKey struct{}

// WithStorageWait returns a context under which a durable sink waits for storage to have room, rather than failing
// a batch when storage is full, until stop is done.  Only sources that stop reading while they wait, such as file
// and journal tailers, use it, so full storage delays their logs instead of losing them.
func WithStorageWait(ctx, stop context.Context) context.Context {
	return context.WithValue(ctx, storageWaitKey{}, stop)
}

// StorageWait returns the context that ends a durable sink's wait for storage, or nil if it must not wait.
func StorageWait(ctx context.Context) context.Context {
	stop, _ := ctx.Value(storageWaitKey{}).(context.Context)
	return stop
}

// IsDurable reports whether a sink owns the durability of the data it receives.
func IsDurable(s Sink) bool {
	d, ok := s.(DurableSink)
	return ok && d.Durable()
}
