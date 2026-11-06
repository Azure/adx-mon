package storage

import (
	"testing"

	"github.com/Azure/adx-mon/pkg/ingestpolicy"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/stretchr/testify/require"
)

func newOverBudgetStore(t *testing.T, global, reserved int64) *LocalStore {
	t.Helper()
	policy, err := ingestpolicy.New([]ingestpolicy.Table{{Database: "DB", Table: "Fast"}})
	require.NoError(t, err)
	a := wal.NewAdmission(wal.AdmissionOpts{Global: wal.Usage{Bytes: global}, Policy: policy, RealtimeReservedBytes: reserved})
	return &LocalStore{opts: StoreOpts{Admission: a, Policy: policy}}
}

// When the realtime and queued parts of a request each fit but not together, the queued part is shed so the
// realtime part is written, never the other way around.
func TestOverBudgetKeepsRealtimeWhenBothDoNotFitTogether(t *testing.T) {
	s := newOverBudgetStore(t, 64<<10, 32<<10)
	r, err := s.opts.Admission.Reserve("DB_Other_1.wal", wal.Destination{Database: "DB", Table: "Other"}, 10<<10, 0)
	require.NoError(t, err)
	defer r.Release()
	keys := [][]byte{[]byte("DB_Fast"), []byte("DB_Slow")}
	sizes := []int64{40 << 10, 20 << 10} // realtime fits the whole budget, queued fits its share, but not both.

	full, err := s.overBudget(2, func(i int) int64 { return sizes[i] }, func(i int) []byte { return keys[i] })
	require.NoError(t, err)
	require.Equal(t, []bool{false, true}, full, "the queued table should be shed and the realtime one kept")
}

// Realtime data that does not fit is rejected with a retryable error rather than shed, even when the queued part
// of the same request would fit.
func TestOverBudgetRejectsRealtimeThatDoesNotFit(t *testing.T) {
	s := newOverBudgetStore(t, 64<<10, 32<<10)
	r, err := s.opts.Admission.Reserve("DB_Fast_1.wal", wal.Destination{Database: "DB", Table: "Fast"}, 40<<10, 0)
	require.NoError(t, err)
	defer r.Release()
	keys := [][]byte{[]byte("DB_Fast"), []byte("DB_Slow")}
	sizes := []int64{30 << 10, 1 << 10}

	_, err = s.overBudget(2, func(i int) int64 { return sizes[i] }, func(i int) []byte { return keys[i] })
	require.ErrorIs(t, err, wal.ErrMaxDiskUsageExceeded)
}
