package ingestor

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/adx-mon/storage"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
)

func TestTableOverloadDoesNotChangeReadinessAndRecoversAfterDrain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source, err := wal.NewSegment(t.TempDir(), "DB_A_s")
	require.NoError(t, err)
	_, err = source.Write(ctx, []byte("1,2,3\n"))
	require.NoError(t, err)
	require.NoError(t, source.Close())
	payload, err := os.ReadFile(source.Path())
	require.NoError(t, err)
	opts, err := wal.ResolveAdmissionWithEnforcement(wal.Usage{Bytes: 2000, Segments: 100}, 0, 0, false, true, []wal.TableLimit{{Database: "DB", Table: "A", MaxBytes: int64(len(payload))}})
	require.NoError(t, err)
	store := storage.NewLocalStore(storage.StoreOpts{StorageDir: t.TempDir(), Admission: wal.NewAdmission(opts)})
	require.NoError(t, store.Open(ctx))
	defer store.Close()
	health := cluster.NewHealth(cluster.HealthOpts{MaxDiskUsage: 2000, MaxSegmentCount: 100, QueueSizer: &queueSizerAdapter{store: store}})
	s := &Service{store: store, health: health, databases: map[string]struct{}{"DB": {}}}
	transfer := func(table string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/transfer?filename=DB_"+table+"_s_123.wal", bytes.NewReader(payload))
		resp := httptest.NewRecorder()
		s.HandleTransfer(resp, r)
		return resp
	}
	require.Equal(t, http.StatusAccepted, transfer("A").Code)
	resp := transfer("A")
	require.Equal(t, http.StatusTooManyRequests, resp.Code)
	require.Equal(t, "table", resp.Header().Get(cluster.OverloadScopeHeader))
	require.Equal(t, http.StatusAccepted, transfer("B").Code)
	require.Equal(t, http.StatusAccepted, transfer("C").Code)
	ready := httptest.NewRecorder()
	s.HandleReady(ready, httptest.NewRequest("GET", "/readyz", nil))
	require.Equal(t, http.StatusOK, ready.Code)
	closed := store.Index().Get(nil, "DB_A_s")
	require.Len(t, closed, 1)
	require.NoError(t, store.Remove(closed[0].Path))
	store.Index().Remove(closed[0])
	require.Equal(t, http.StatusAccepted, transfer("A").Code)
}

func TestGlobalAdmissionStillProtectsInstance(t *testing.T) {
	budget := wal.NewAdmission(wal.AdmissionOpts{Global: wal.Usage{Bytes: 100, Segments: 2}})
	budget.Restore("a", wal.Destination{Database: "DB", Table: "A"}, 100)
	_, err := budget.Reserve("b", wal.Destination{Database: "DB", Table: "B"}, 8, 1)
	require.ErrorIs(t, err, wal.ErrMaxDiskUsageExceeded)
}

type tableTestUploader struct {
	Uploader
	queue chan *cluster.Batch
}

func (u tableTestUploader) UploadQueue() chan *cluster.Batch { return u.queue }

func TestIngestorWiresTableBudgetDefaults(t *testing.T) {
	svc, err := NewService(ServiceOpts{StorageDir: t.TempDir(), MaxDiskUsage: 10000, MaxSegmentCount: 100, Uploader: tableTestUploader{queue: make(chan *cluster.Batch, 10)}, K8sCli: fake.NewSimpleClientset(), Hostname: "ingestor-0", Namespace: "adx-mon"})
	require.NoError(t, err)
	store := svc.store.(*storage.LocalStore)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, store.Open(ctx))
	defer store.Close()
	w, err := store.GetWAL(ctx, []byte("DB_A_s"))
	require.NoError(t, err)
	require.NoError(t, w.Write(ctx, []byte("data")))
	var debug bytes.Buffer
	require.NoError(t, store.WriteDebug(&debug))
	require.Contains(t, debug.String(), "/1000 segments=1/10")
	ready := httptest.NewRecorder()
	svc.HandleReady(ready, httptest.NewRequest("GET", "/readyz", nil))
	require.Equal(t, http.StatusOK, ready.Code)
}

func TestIngestorRejectsIntakeCeilingBelowTransferSize(t *testing.T) {
	_, err := NewService(ServiceOpts{StorageDir: t.TempDir(), MaxTransferSize: 2048, MaxIntakeMemory: 1024, Uploader: tableTestUploader{queue: make(chan *cluster.Batch, 10)}, K8sCli: fake.NewSimpleClientset(), Hostname: "ingestor-0", Namespace: "adx-mon"})
	require.ErrorContains(t, err, "max intake memory")
}
