package types

import (
	"github.com/Azure/adx-mon/pkg/pool"
)

var (
	LogBatchPool = pool.NewGeneric(200, func(sz int) interface{} {
		return &LogBatch{
			Logs: make([]*Log, 0, sz),
		}
	})
	LogPool = pool.NewGeneric(1024, func(sz int) interface{} {
		return NewLog()
	})
)
