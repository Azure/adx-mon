package storage

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/stretchr/testify/require"
)

func TestStoreRetiresDrainedWALDuringQuietPeriod(t *testing.T) {
	// Generate the on-disk ID on the real clock (the process-wide flake
	// generator predates synctest's clock), then restore it in the bubble.
	dir := t.TempDir()
	seg, err := wal.NewSegment(dir, "DB_A_schema")
	require.NoError(t, err)
	_, err = seg.Write(t.Context(), []byte("payload"))
	require.NoError(t, err)
	require.NoError(t, seg.Close())
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		a := wal.NewAdmission(wal.AdmissionOpts{})
		s := NewLocalStore(StoreOpts{StorageDir: dir, Admission: a})
		require.NoError(t, s.Open(ctx))
		defer s.Close()
		for _, si := range s.Index().Get(nil, "DB_A_schema") {
			require.NoError(t, s.Remove(si.Path))
			s.Index().Remove(si)
		}
		time.Sleep(6 * time.Minute)
		require.Zero(t, s.WALCount(), "the normal sweep retires drained state without another write")
		require.Equal(t, wal.Usage{}, a.Usage())
	})
}
