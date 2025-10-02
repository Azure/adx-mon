package clickhouse

import (
	"context"
	"log/slog"
	"sync"

	"github.com/Azure/adx-mon/ingestor/cluster"
	monlogger "github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/service"
)

// Uploader exposes the contract shared by all storage backends so that the
// ingestor service can remain agnostic of the implementation details.
type Uploader interface {
	service.Component
	UploadQueue() chan *cluster.Batch
	Database() string
	DSN() string
}

type uploader struct {
	cfg     Config
	log     *slog.Logger
	queue   chan *cluster.Batch
	schemas map[string]Schema
	conn    connectionManager

	mu     sync.RWMutex
	cancel context.CancelFunc
	ctx    context.Context
	open   bool
}

// NewUploader constructs a ClickHouse-backed uploader
func NewUploader(cfg Config, log *slog.Logger) (Uploader, error) {
	cfg = cfg.withDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if log == nil {
		log = monlogger.Logger()
	}
	log = log.With(slog.String("component", "clickhouse-uploader"))

	conn, err := newConnectionManager(cfg)
	if err != nil {
		return nil, err
	}

	return &uploader{
		cfg:     cfg,
		log:     log,
		queue:   make(chan *cluster.Batch, cfg.QueueCapacity),
		schemas: DefaultSchemas(),
		conn:    conn,
	}, nil
}

func (u *uploader) Open(ctx context.Context) error {
	u.mu.Lock()
	defer u.mu.Unlock()

	if u.open {
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	u.ctx = ctx
	u.cancel = cancel
	u.open = true
	if err := u.conn.Ping(ctx); err != nil {
		return err
	}

	u.log.Info("clickhouse uploader ready")

	return nil
}

func (u *uploader) Close() error {
	u.mu.Lock()
	defer u.mu.Unlock()

	if !u.open {
		return nil
	}

	u.cancel()
	u.open = false

	if err := u.conn.Close(); err != nil {
		u.log.Warn("failed to close clickhouse connection", slog.String("error", err.Error()))
	}
	u.log.Info("clickhouse uploader stopped")
	return nil
}

func (u *uploader) UploadQueue() chan *cluster.Batch {
	return u.queue
}

func (u *uploader) Database() string {
	return u.cfg.Database
}

func (u *uploader) DSN() string {
	return u.cfg.DSN
}

// Schemas exposes the tables known to the uploader.
func (u *uploader) Schemas() map[string]Schema {
	u.mu.RLock()
	defer u.mu.RUnlock()

	cp := make(map[string]Schema, len(u.schemas))
	for k, v := range u.schemas {
		cp[k] = v
	}
	return cp
}
