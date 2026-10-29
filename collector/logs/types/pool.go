package types

import (
	"github.com/Azure/adx-mon/pkg/pool"
)

var (
	// LogBatchPool reuses batches while allowing idle backing arrays to be
	// garbage collected. New batches have room for the source batch size.
	LogBatchPool = pool.NewResettingPool(func() *LogBatch {
		return &LogBatch{
			Logs: make([]*Log, 0, 1024),
			Ack:  noop,
		}
	})
	// LogPool clears payload references on return and allows idle logs and
	// their map storage to be garbage collected.
	LogPool = pool.NewResettingPool(NewLog)
)
