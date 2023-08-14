package service

import (
	"context"
	"net/http"
)

type Telemetry interface {
	Open(ctx context.Context) error
	Close() error
	HandleReceive() (string, http.Handler)
	HandleTransfer() (string, http.Handler)
	UploadSegments() error
	DisableWrites() error
}
