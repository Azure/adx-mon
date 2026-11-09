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
