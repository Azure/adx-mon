package clickhouse

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// DefaultQueueCapacity controls how many batches we can buffer locally before
	// backpressure propagates to the cluster batcher.
	DefaultQueueCapacity = 2048

	// DefaultBatchMaxRows defines the target number of rows per ClickHouse insert.
	DefaultBatchMaxRows = 50000

	// DefaultBatchFlushInterval determines how often we flush even if row count thresholds
	// haven't been met. Tuned during later phases but needs a sensible default now.
	DefaultBatchFlushInterval = 5 * time.Second
)

// Config captures the knobs required to establish a ClickHouse uploader.
// It purposely mirrors the shape of the future CLI flags so that wiring is mechanical.
type Config struct {
	// Database is the logical ClickHouse database that owns the destination tables.
	Database string

	// DSN represents the ClickHouse connection string (clickhouse://, tcp://, https://, etc.).
	DSN string

	// QueueCapacity bounds the local batch queue exposed to the ingestor.
	QueueCapacity int

	// TLS captures optional TLS overrides for the client.
	TLS TLSConfig

	// Auth captures explicit username/password overrides when the DSN does not include them.
	Auth AuthConfig

	// Batch controls how rows are grouped before inserts.
	Batch BatchConfig
}

// TLSConfig wraps TLS related configuration. Either both CertFile and KeyFile must be provided or neither.
type TLSConfig struct {
	CAFile             string
	CertFile           string
	KeyFile            string
	InsecureSkipVerify bool
}

// AuthConfig represents optional basic authentication credentials.
type AuthConfig struct {
	Username string
	Password string
}

// BatchConfig determines how the uploader groups rows prior to flushing.
type BatchConfig struct {
	MaxRows       int
	MaxBytes      int64
	FlushInterval time.Duration
}

func (c Config) withDefaults() Config {
	if c.QueueCapacity <= 0 {
		c.QueueCapacity = DefaultQueueCapacity
	}
	if c.Batch.MaxRows <= 0 {
		c.Batch.MaxRows = DefaultBatchMaxRows
	}
	if c.Batch.FlushInterval <= 0 {
		c.Batch.FlushInterval = DefaultBatchFlushInterval
	}
	return c
}

// Validate returns an aggregated error describing any invalid configuration fields.
func (c Config) Validate() error {
	var errs []error

	if strings.TrimSpace(c.Database) == "" {
		errs = append(errs, errors.New("database is required"))
	}
	if strings.TrimSpace(c.DSN) == "" {
		errs = append(errs, errors.New("DSN is required"))
	}

	if c.QueueCapacity <= 0 {
		errs = append(errs, fmt.Errorf("queue capacity must be positive: %d", c.QueueCapacity))
	}
	if c.Batch.MaxRows <= 0 {
		errs = append(errs, fmt.Errorf("batch max rows must be positive: %d", c.Batch.MaxRows))
	}
	if c.Batch.FlushInterval <= 0 {
		errs = append(errs, fmt.Errorf("batch flush interval must be positive: %s", c.Batch.FlushInterval))
	}

	if (c.TLS.CertFile != "" && c.TLS.KeyFile == "") || (c.TLS.CertFile == "" && c.TLS.KeyFile != "") {
		errs = append(errs, errors.New("both tls cert file and key file must be provided together"))
	}

	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}
