// Package pool provides pool structures to help reduce garbage collector pressure.
package pool

import (
	gbp "github.com/libp2p/go-buffer-pool"
)

var sharedPool = &gbp.BufferPool{}

// Bytes is a pool of byte slices that can be re-used.  Slices in
// this pool will not be garbage collected when not in use.
type Bytes struct{}

// NewBytes returns a Bytes pool with capacity for max byte slices
// to be pool.
func NewBytes(max int) *Bytes {
	return &Bytes{}
}

// Get returns a byte slice size with at least sz capacity. Items
// returned may not be in the zero state and should be reset by the
// caller.
func (p *Bytes) Get(sz int) []byte {
	return sharedPool.Get(sz)
}

// Put returns a slice back to the pool.  If the pool is full, the byte
// slice is discarded.
func (p *Bytes) Put(c []byte) {
	sharedPool.Put(c)
}
