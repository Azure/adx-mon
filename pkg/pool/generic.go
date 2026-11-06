package pool

// Generic is a pool of types that can be re-used.  Items in
// this pool will not be garbage collected when not in use.
type Generic struct {
	pool chan any
	fn   func(sz int) any
}

// NewGeneric returns a Generic pool with capacity for max items
// to be pool.
func NewGeneric(max int, fn func(sz int) any) *Generic {
	return &Generic{
		pool: make(chan any, max),
		fn:   fn,
	}
}

// Get returns an item from the pool or a new instance if the pool
// is empty.  Items returned may not be in the zero state and should
// be reset by the caller.
func (p *Generic) Get(sz int) any {
	var c any
	select {
	case c = <-p.pool:
	default:
		c = p.fn(sz)
	}

	return c
}

// Put returns an item back to the pool.  If the pool is full, the item
// is discarded.
func (p *Generic) Put(c any) {
	select {
	case p.pool <- c:
	default:
	}
}
