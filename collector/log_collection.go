package collector

import (
	"github.com/Azure/adx-mon/collector/logs"
	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/pkg/http"
	"github.com/Azure/adx-mon/storage"
)

// LogCollectorOpts is the options for creating a log collector.
type LogCollectorOpts struct {
	Create createFunc
}

// HttpLogCollectorOpts is the options for creating a log collector with an HTTP endpoint.
type HttpLogCollectorOpts struct {
	CreateHTTPSvc createHttpFunc
}

type createFunc func(store storage.Store) (*logs.Service, error)

type createHttpFunc func(store storage.Store, health *cluster.Health) (*logs.Service, *http.HttpHandler, error)
