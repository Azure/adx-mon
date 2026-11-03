// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package adx

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/adx-mon/storage"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

// newAuditTestStore opens a store with admission accounting, as the ingestor configures it.
func newAuditTestStore(t *testing.T, dir string) *storage.LocalStore {
	t.Helper()
	store := storage.NewLocalStore(storage.StoreOpts{
		StorageDir:     dir,
		SegmentMaxAge:  time.Hour,
		SegmentMaxSize: 1 << 30,
		Admission:      wal.NewAdmission(wal.AdmissionOpts{}),
	})
	require.NoError(t, store.Open(context.Background()))
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// writeActiveSegments writes to n distinct tables so each has an active, unrotated segment on disk.
func writeActiveSegments(t *testing.T, store *storage.LocalStore, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		w, err := store.GetWAL(context.Background(), []byte(fmt.Sprintf("db_table%d", i)))
		require.NoError(t, err)
		require.NoError(t, w.Write(context.Background(), []byte("some,csv,data\n")))
		require.NoError(t, w.Flush())
	}
}

func newAuditTestBatcher(t *testing.T, store *storage.LocalStore) cluster.Batcher {
	t.Helper()
	batcher, err := cluster.NewBatcher(cluster.BatcherOpts{
		StorageDir:              t.TempDir(),
		Segmenter:               store.Index(),
		Usage:                   store,
		SegmentsCountMetric:     prometheus.NewGauge(prometheus.GaugeOpts{Name: "count"}),
		SegmentsSizeBytesMetric: prometheus.NewGauge(prometheus.GaugeOpts{Name: "size"}),
		SegmentsMaxAgeMetric:    prometheus.NewGauge(prometheus.GaugeOpts{Name: "age"}),
	})
	require.NoError(t, err)
	return batcher
}

// TestAuditDiskSpaceTask_ActiveSegmentsAreNotAMismatch reproduces the audit warning seen on ingestors where the
// number of segment files on disk was hundreds more than expected.  Each table that received data recently has an
// active segment on disk that is not in the index of closed segments, so comparing the disk with the index reports
// every active segment as a mismatch even though nothing is orphaned.
func TestAuditDiskSpaceTask_ActiveSegmentsAreNotAMismatch(t *testing.T) {
	dir := t.TempDir()
	store := newAuditTestStore(t, dir)
	batcher := newAuditTestBatcher(t, store)

	// More active tables than the audit's segment count tolerance.
	writeActiveSegments(t, store, 25)

	// None of the segments are closed, so an audit against the index alone expects nothing on disk.
	require.Zero(t, store.Index().TotalSegments())

	task := NewAuditDiskSpaceTask(store, batcher.(BatchTracker), dir, time.Hour)
	res, err := task.audit()
	require.NoError(t, err)
	require.Equal(t, int64(25), res.ActualCount)
	require.Equal(t, res.ActualCount, res.ExpectedCount)
	require.Zero(t, res.UntrackedCount)
	require.Falsef(t, res.Mismatch, "every segment on disk is an active segment the ingestor knows about: %+v", res)

	// The batcher's metrics report the same totals as the audit and the health status.
	require.Equal(t, res.ExpectedCount, batcher.SegmentsTotal())
	require.Equal(t, res.ExpectedSize, batcher.SegmentsSize())
}

// TestAuditDiskSpaceTask_ReportsUntrackedSegments verifies that a segment file the ingestor does not account for is
// reported by path, even when it is well within the count and size tolerances.
func TestAuditDiskSpaceTask_ReportsUntrackedSegments(t *testing.T) {
	dir := t.TempDir()
	store := newAuditTestStore(t, dir)
	writeActiveSegments(t, store, 3)

	old := time.Now().Add(-time.Hour)
	orphan := filepath.Join(dir, "db_orphan_2f2bb698bb432000.wal")
	require.NoError(t, os.WriteFile(orphan, []byte("ADXWAL\x00\x00"), 0o600))
	require.NoError(t, os.Chtimes(orphan, old, old))

	// A brand new untracked file is not reported since a segment file is created before it is accounted for.
	fresh := filepath.Join(dir, "db_fresh_2f2bb698bb432001.wal")
	require.NoError(t, os.WriteFile(fresh, []byte("ADXWAL\x00\x00"), 0o600))

	task := NewAuditDiskSpaceTask(store, nil, dir, 0)
	res, err := task.audit()
	require.NoError(t, err)
	require.True(t, res.Mismatch)
	require.Equal(t, 1, res.UntrackedCount)
	require.Equal(t, []string{orphan}, res.Untracked)
}

type fakeBatchTracker map[string]bool

func (f fakeBatchTracker) IsBatched(path string) bool { return f[path] }

// TestAuditDiskSpaceTask_ReportsStaleSegments verifies that closed segments older than the stale age are reported with
// whether they are held by an in-flight batch, which identifies segments that are stuck and keep the oldest segment
// age growing.
func TestAuditDiskSpaceTask_ReportsStaleSegments(t *testing.T) {
	dir := t.TempDir()
	store := newAuditTestStore(t, dir)

	now := time.Now()
	stuck := wal.SegmentInfo{Prefix: "db_stuck", Path: filepath.Join(dir, "db_stuck_1.wal"), Size: 10, CreatedAt: now.Add(-3 * time.Hour)}
	old := wal.SegmentInfo{Prefix: "db_old", Path: filepath.Join(dir, "db_old_1.wal"), Size: 20, CreatedAt: now.Add(-2 * time.Hour)}
	recent := wal.SegmentInfo{Prefix: "db_recent", Path: filepath.Join(dir, "db_recent_1.wal"), Size: 30, CreatedAt: now}
	for _, si := range []wal.SegmentInfo{recent, old, stuck} {
		store.Index().Add(si)
	}

	task := NewAuditDiskSpaceTask(store, fakeBatchTracker{stuck.Path: true}, dir, time.Hour)
	res, err := task.audit()
	require.NoError(t, err)
	require.Len(t, res.Stale, 2)
	require.Equal(t, stuck.Path, res.Stale[0].Path)
	require.True(t, res.Stale[0].Batched)
	require.Equal(t, old.Path, res.Stale[1].Path)
	require.False(t, res.Stale[1].Batched)

	// Nothing is reported while the oldest segment is younger than the stale age.
	task.StaleSegmentAge = 4 * time.Hour
	res, err = task.audit()
	require.NoError(t, err)
	require.Empty(t, res.Stale)
}

// transientAccounting under-reports the segment count on its first read, as when segments are created between the
// audit reading the disk and reading the accounting.
type transientAccounting struct {
	SegmentAccounting
	reads int
}

func (a *transientAccounting) SegmentCount() int64 {
	a.reads++
	if a.reads == 1 {
		return 0
	}
	return a.SegmentAccounting.SegmentCount()
}

// TestAuditDiskSpaceTask_RechecksTransientMismatch verifies that a mismatch that is gone when the audit checks again is
// not reported.
func TestAuditDiskSpaceTask_RechecksTransientMismatch(t *testing.T) {
	dir := t.TempDir()
	store := newAuditTestStore(t, dir)
	writeActiveSegments(t, store, 25)

	task := NewAuditDiskSpaceTask(&transientAccounting{SegmentAccounting: store}, nil, dir, 0)
	task.RecheckDelay = time.Millisecond

	res, err := task.audit()
	require.NoError(t, err)
	require.True(t, res.Mismatch, "the first audit sees the transient mismatch")

	res, err = task.auditWithRecheck(context.Background())
	require.NoError(t, err)
	require.False(t, res.Mismatch, "res=%+v", res)
}
