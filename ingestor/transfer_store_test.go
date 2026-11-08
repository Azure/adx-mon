package ingestor

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/adx-mon/storage"
	"github.com/stretchr/testify/require"
)

// A transfer is stored by the ingestor that receives it, whichever ingestor owns its table and whether it came from a
// collector or a peer.  A segment of a table owned by another ingestor is transferred to the owner once it rotates.
func TestHandleTransferStoresTransfersLocally(t *testing.T) {
	for name, header := range map[string]http.Header{
		"collector": nil,
		"peer":      {cluster.PeerTransferHeader: {"true"}},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			dir := t.TempDir()
			store := storage.NewLocalStore(storage.StoreOpts{StorageDir: dir, MaxDiskUsage: 1 << 30})
			require.NoError(t, store.Open(ctx))
			defer store.Close()
			s := &Service{store: store, health: &fakeHealthChecker{healthy: true}, databases: map[string]struct{}{"DB": {}}}

			seg, err := wal.NewSegment(t.TempDir(), "DB_Cpu")
			require.NoError(t, err)
			_, err = seg.Write(context.Background(), []byte("1,2,3\n"))
			require.NoError(t, err)
			require.NoError(t, seg.Close())
			payload, err := os.ReadFile(seg.Path())
			require.NoError(t, err)

			r := httptest.NewRequest("POST", "/transfer?filename="+wal.Filename("DB", "Cpu", "", "2f2c74ffa19e6000"), bytes.NewReader(payload))
			for k, v := range header {
				r.Header[k] = v
			}
			resp := httptest.NewRecorder()
			s.HandleTransfer(resp, r)
			require.Equal(t, http.StatusAccepted, resp.Code)

			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			var stored []string
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), "DB_Cpu_") {
					stored = append(stored, e.Name())
				}
			}
			require.NotEmpty(t, stored, "the transfer is stored in this ingestor's WAL")
		})
	}
}
