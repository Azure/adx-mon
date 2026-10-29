package pool

import "sync"

// Resetter is an object that can be cleared for reuse.
type Resetter interface {
	Reset()
}

// ResettingPool is a typed sync.Pool that resets items on return. Unlike
// Generic, unused items may be garbage collected. A ResettingPool must not be
// copied after first use.
type ResettingPool[T Resetter] struct {
	pool sync.Pool
}

// NewResettingPool returns a pool that creates items with new when empty. Use
// pointer types to avoid allocating when storing items in sync.Pool's interface
// values.
func NewResettingPool[T Resetter](new func() T) *ResettingPool[T] {
	return &ResettingPool[T]{pool: sync.Pool{New: func() any { return new() }}}
}

// Get returns an initialized item ready for use. Items returned to the pool
// have already been reset by Put.
func (p *ResettingPool[T]) Get() T {
	return p.pool.Get().(T)
}

// Put resets item before returning it to the pool, releasing references to
// payloads even if the item is not reused. The caller must no longer use item.
func (p *ResettingPool[T]) Put(item T) {
	item.Reset()
	p.pool.Put(item)
}
