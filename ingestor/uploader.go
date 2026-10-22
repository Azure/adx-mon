package ingestor

import (
	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/pkg/service"
)

// Uploader captures the minimal contract required by the ingestor service to
// interact with storage backends. Both the legacy ADX uploader and the new
// ClickHouse uploader satisfy this interface.
type Uploader interface {
	service.Component
	UploadQueue() chan *cluster.Batch
	Database() string
}
