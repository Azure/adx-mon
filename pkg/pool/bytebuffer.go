package pool

import (
	gbp "github.com/libp2p/go-buffer-pool"
)

var BytesBufferPool = NewGeneric(1000, func(sz int) any {
	return &gbp.Buffer{Pool: gbp.GlobalPool}
})
