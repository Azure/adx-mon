package clickhouse

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigValidateErrors(t *testing.T) {
	t.Parallel()

	cfg := Config{}
	err := cfg.Validate()
	require.Error(t, err)
	require.ErrorContains(t, err, "database is required")
	require.ErrorContains(t, err, "DSN is required")
}

func TestNewUploaderAppliesDefaults(t *testing.T) {
	t.Parallel()

	withFakeConnectionManager(t, &fakeConnection{})

	cfg := Config{
		Database: "metrics",
		DSN:      "clickhouse://localhost:9000/default",
	}

	u, err := NewUploader(cfg, nil)
	require.NoError(t, err)

	impl, ok := u.(*uploader)
	require.True(t, ok)
	require.Equal(t, DefaultQueueCapacity, cap(impl.queue))
	require.Equal(t, DefaultBatchMaxRows, impl.cfg.Batch.MaxRows)
	require.Equal(t, DefaultBatchFlushInterval, impl.cfg.Batch.FlushInterval)
	require.Equal(t, cfg.Database, impl.Database())
	require.Equal(t, cfg.DSN, impl.DSN())
	require.NotNil(t, impl.log)
	require.NotNil(t, impl.schemas)
}

func TestNewUploaderValidatesTLSPairing(t *testing.T) {
	t.Parallel()

	withFakeConnectionManager(t, &fakeConnection{})

	cfg := Config{
		Database: "metrics",
		DSN:      "clickhouse://localhost:9000/default",
		TLS: TLSConfig{
			CertFile: "cert.pem",
		},
	}

	_, err := NewUploader(cfg, nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "tls cert file")
}

func TestOpenCloseIsIdempotent(t *testing.T) {
	t.Parallel()

	fake := &fakeConnection{}
	withFakeConnectionManager(t, fake)

	cfg := Config{
		Database:      "metrics",
		DSN:           "clickhouse://localhost:9000/default",
		QueueCapacity: 16,
	}

	u, err := NewUploader(cfg, nil)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, u.Open(ctx))
	require.NoError(t, u.Open(ctx))
	require.NoError(t, u.Close())
	require.NoError(t, u.Close())
}

func TestSchemasReturnsCopy(t *testing.T) {
	t.Parallel()

	withFakeConnectionManager(t, &fakeConnection{})

	cfg := Config{Database: "metrics", DSN: "clickhouse://localhost"}
	u, err := NewUploader(cfg, nil)
	require.NoError(t, err)

	impl := u.(*uploader)
	first := impl.Schemas()
	first["metrics"] = Schema{Table: "hacked"}

	second := impl.Schemas()
	require.NotEqual(t, "hacked", second["metrics"].Table)
}

type fakeConnection struct {
	pingErr error
}

func (f *fakeConnection) Ping(context.Context) error { return f.pingErr }
func (f *fakeConnection) Close() error               { return nil }

func withFakeConnectionManager(t *testing.T, conn connectionManager) {
	t.Helper()
	original := newConnectionManager
	newConnectionManager = func(Config) (connectionManager, error) {
		return conn, nil
	}
	t.Cleanup(func() {
		newConnectionManager = original
	})
}
