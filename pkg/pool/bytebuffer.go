package pool

import (
	gbp "github.com/libp2p/go-buffer-pool"
)

var BytesBufferPool = &bytesBufferPool{NewGeneric(1000, func(sz int) any {
	return &gbp.Buffer{Pool: gbp.GlobalPool}
})}

// bytesBufferPool is a pool of gbp.Buffers.
type bytesBufferPool struct {
	*Generic
}

// Put returns c to the pool after handing its memory back to gbp's pool, which the garbage collector can free.  The
// pool keeps up to 1000 buffers and never releases them, so a buffer that kept the memory it grew to while holding a
// request body would keep it for good: after a flood of large bodies, every pooled buffer would hold one.
func (p *bytesBufferPool) Put(c any) {
	if b, ok := c.(*gbp.Buffer); ok {
		b.Reset()
	}
	p.Generic.Put(c)
}
