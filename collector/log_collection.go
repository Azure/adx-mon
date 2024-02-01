package collector

import (
	"github.com/Azure/adx-mon/collector/logs"
	"github.com/Azure/adx-mon/ingestor/storage"
)

type LogCollectorOpts struct {
	Create createFunc
}

type createFunc func(store storage.Store) (*logs.Service, error)
