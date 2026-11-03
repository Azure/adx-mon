package collector

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The collector has a single load-balanced ingestor peer, so one failed transfer must only pause shipping briefly
// rather than for the full one minute UnhealthyTimeout.
func TestServiceRemotePeerRecoversQuicklyAfterTransientFailure(t *testing.T) {
	s, err := NewService(&ServiceOpts{
		StorageDir:  t.TempDir(),
		ListenAddr:  MetricListenAddr,
		Endpoint:    "http://127.0.0.1:1",
		DisableGzip: true,
	})
	require.NoError(t, err)

	s.health.SetPeerUnhealthy("remote")
	require.False(t, s.health.IsPeerHealthy("remote"))
	require.Eventually(t, func() bool { return s.health.IsPeerHealthy("remote") }, 3*time.Second, 50*time.Millisecond)
}
