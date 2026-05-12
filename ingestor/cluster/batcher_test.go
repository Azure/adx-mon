package cluster

import (
	"path/filepath"
	"testing"
	"time"

	flakeutil "github.com/Azure/adx-mon/pkg/flake"
	"github.com/Azure/adx-mon/pkg/partmap"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/davidnarayan/go-flake"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

// newTestMetrics creates no-op metrics for testing
func newTestMetrics() (prometheus.Gauge, prometheus.Gauge, prometheus.Gauge) {
	return prometheus.NewGauge(prometheus.GaugeOpts{Name: "test_segments_count"}),
		prometheus.NewGauge(prometheus.GaugeOpts{Name: "test_segments_size_bytes"}),
		prometheus.NewGauge(prometheus.GaugeOpts{Name: "test_segments_max_age"})
}

func TestBatcher_ClosedSegments(t *testing.T) {
	dir := t.TempDir()

	idx := wal.NewIndex()
	idx.Add(wal.SegmentInfo{
		Prefix:    "db_Cpu",
		Ulid:      "aaaa",
		Path:      filepath.Join(dir, wal.Filename("db", "Cpu", "", "aaaa")),
		Size:      100,
		CreatedAt: time.Unix(1, 0),
	})

	idx.Add(wal.SegmentInfo{
		Prefix:    "db_Cpu",
		Ulid:      "bbbb",
		Path:      filepath.Join(dir, wal.Filename("db", "Cpu", "", "bbbb")),
		Size:      100,
		CreatedAt: time.Unix(0, 0),
	})

	countMetric, sizeMetric, ageMetric := newTestMetrics()
	a := &batcher{
		hostname:                "node1",
		storageDir:              dir,
		Partitioner:             &fakePartitioner{owner: "node1"},
		Segmenter:               idx,
		segments:                partmap.NewMap[int](64),
		minUploadSize:           1,
		segmentsCountMetric:     countMetric,
		segmentsSizeBytesMetric: sizeMetric,
		segmentsMaxAgeMetric:    ageMetric,
	}
	owner, notOwned, err := a.processSegments()
	require.NoError(t, err)
	require.Equal(t, []string{filepath.Join(dir, wal.Filename("db", "Cpu", "", "aaaa"))}, owner[0].Paths())
	require.Equal(t, 0, len(notOwned))

	requireValidBatch(t, owner)
	requireValidBatch(t, notOwned)
}

func TestBatcher_NodeOwned(t *testing.T) {
	dir := t.TempDir()

	idgen, err := flake.New()
	require.NoError(t, err)
	now := idgen.NextId()

	created, err := flakeutil.ParseFlakeID(now.String())
	require.NoError(t, err)

	idx := wal.NewIndex()
	f1 := wal.SegmentInfo{
		Prefix:    "db_Cpu",
		Ulid:      now.String(),
		Path:      filepath.Join(dir, wal.Filename("db", "Cpu", "", now.String())),
		Size:      100,
		CreatedAt: created,
	}
	idx.Add(f1)

	now = idgen.NextId()
	created, err = flakeutil.ParseFlakeID(now.String())
	require.NoError(t, err)

	f2 := wal.SegmentInfo{
		Prefix:    "db_Cpu",
		Ulid:      now.String(),
		Path:      filepath.Join(dir, wal.Filename("db", "Cpu", "", now.String())),
		Size:      100,
		CreatedAt: created,
	}
	idx.Add(f2)

	countMetric, sizeMetric, ageMetric := newTestMetrics()
	a := &batcher{
		hostname:                "node1",
		storageDir:              dir,
		maxTransferAge:          30 * time.Second,
		maxTransferSize:         100 * 1024 * 1024,
		minUploadSize:           100 * 1024 * 1024,
		maxBatchSegments:        25,
		Partitioner:             &fakePartitioner{owner: "node2"},
		Segmenter:               idx,
		health:                  &fakeHealthChecker{healthy: true},
		segments:                partmap.NewMap[int](64),
		segmentsCountMetric:     countMetric,
		segmentsSizeBytesMetric: sizeMetric,
		segmentsMaxAgeMetric:    ageMetric,
	}
	owner, notOwned, err := a.processSegments()
	require.NoError(t, err)
	require.Equal(t, 0, len(owner))
	require.Equal(t, []string{f1.Path, f2.Path}, notOwned[0].Paths())

	requireValidBatch(t, owner)
	requireValidBatch(t, notOwned)
}

func TestBatcher_NewestFirst(t *testing.T) {
	dir := t.TempDir()

	idgen, err := flake.New()
	require.NoError(t, err)

	// Disk is created first so it sorts as the oldest prefix; processSegments
	// then processes the newer Cpu prefix first.
	diskFlake := idgen.NextId()
	diskID := diskFlake.String()
	diskCreated, err := flakeutil.ParseFlakeID(diskID)
	require.NoError(t, err)

	cpuFlake := idgen.NextId()
	cpuID := cpuFlake.String()
	cpuCreated, err := flakeutil.ParseFlakeID(cpuID)
	require.NoError(t, err)

	idx := wal.NewIndex()
	f1 := wal.SegmentInfo{
		Prefix:    "db_Cpu",
		Ulid:      cpuID,
		Path:      filepath.Join(dir, wal.Filename("db", "Cpu", "", cpuID)),
		Size:      100,
		CreatedAt: cpuCreated,
	}
	idx.Add(f1)

	f2 := wal.SegmentInfo{
		Prefix:    "db_Disk",
		Ulid:      diskID,
		Path:      filepath.Join(dir, wal.Filename("db", "Disk", "", diskID)),
		Size:      100,
		CreatedAt: diskCreated,
	}
	idx.Add(f2)

	countMetric, sizeMetric, ageMetric := newTestMetrics()
	a := &batcher{
		hostname:                "node1",
		storageDir:              dir,
		Partitioner:             &fakePartitioner{owner: "node1"},
		Segmenter:               idx,
		segments:                partmap.NewMap[int](64),
		minUploadSize:           1,
		segmentsCountMetric:     countMetric,
		segmentsSizeBytesMetric: sizeMetric,
		segmentsMaxAgeMetric:    ageMetric,
	}
	owner, notOwned, err := a.processSegments()
	require.NoError(t, err)
	require.Equal(t, 2, len(owner))
	require.Equal(t, 0, len(notOwned))

	require.Equal(t, []string{f1.Path}, owner[0].Paths())
	require.Equal(t, "db", owner[0].Database)
	require.Equal(t, "Cpu", owner[0].Table)
	require.Equal(t, []string{f2.Path}, owner[1].Paths())
	require.Equal(t, "db", owner[1].Database)
	require.Equal(t, "Disk", owner[1].Table)

	requireValidBatch(t, owner)
	requireValidBatch(t, notOwned)
}

func TestBatcher_BigFileBatch(t *testing.T) {
	dir := t.TempDir()

	idgen, err := flake.New()
	require.NoError(t, err)

	// Disk is created first so it sorts as the oldest prefix; processSegments
	// then processes the newer Cpu prefix first.
	diskFlake := idgen.NextId()
	diskID := diskFlake.String()
	diskCreated, err := flakeutil.ParseFlakeID(diskID)
	require.NoError(t, err)

	cpu1Flake := idgen.NextId()
	cpu1ID := cpu1Flake.String()
	cpu1Created, err := flakeutil.ParseFlakeID(cpu1ID)
	require.NoError(t, err)

	cpu2Flake := idgen.NextId()
	cpu2ID := cpu2Flake.String()
	cpu2Created, err := flakeutil.ParseFlakeID(cpu2ID)
	require.NoError(t, err)

	idx := wal.NewIndex()
	f1 := wal.SegmentInfo{
		Prefix:    "db_Cpu",
		Ulid:      cpu1ID,
		Path:      filepath.Join(dir, wal.Filename("db", "Cpu", "", cpu1ID)),
		Size:      100 * 1024, // Meets min transfer size, separate batch
		CreatedAt: cpu1Created,
	}
	idx.Add(f1)

	f2 := wal.SegmentInfo{
		Prefix:    "db_Cpu",
		Ulid:      cpu2ID,
		Path:      filepath.Join(dir, wal.Filename("db", "Cpu", "", cpu2ID)),
		Size:      100, // This should be in a new batch
		CreatedAt: cpu2Created,
	}
	idx.Add(f2)

	f3 := wal.SegmentInfo{
		Prefix:    "db_Disk",
		Ulid:      diskID,
		Path:      filepath.Join(dir, wal.Filename("db", "Disk", "", diskID)),
		Size:      100,
		CreatedAt: diskCreated,
	}
	idx.Add(f3)

	countMetric, sizeMetric, ageMetric := newTestMetrics()
	a := &batcher{
		hostname:                "node1",
		storageDir:              dir,
		maxTransferSize:         100 * 1024,
		minUploadSize:           100 * 1024,
		maxTransferAge:          time.Minute,
		Partitioner:             &fakePartitioner{owner: "node1"},
		Segmenter:               idx,
		health:                  fakeHealthChecker{healthy: true},
		segments:                partmap.NewMap[int](64),
		segmentsCountMetric:     countMetric,
		segmentsSizeBytesMetric: sizeMetric,
		segmentsMaxAgeMetric:    ageMetric,
	}
	owned, notOwned, err := a.processSegments()

	require.NoError(t, err)
	require.Equal(t, 3, len(owned))
	require.Equal(t, 0, len(notOwned))

	require.Equal(t, []string{f1.Path}, owned[0].Paths())
	require.Equal(t, []string{f2.Path}, owned[1].Paths())
	require.Equal(t, []string{f3.Path}, owned[2].Paths())

	requireValidBatch(t, owned)
	requireValidBatch(t, notOwned)
}

func TestBatcher_BigBatch(t *testing.T) {
	dir := t.TempDir()

	idgen, err := flake.New()
	require.NoError(t, err)

	// Disk is created first so it sorts as the oldest prefix; processSegments
	// then processes the newer Cpu prefix first.
	diskFlake := idgen.NextId()
	diskID := diskFlake.String()
	diskCreated, err := flakeutil.ParseFlakeID(diskID)
	require.NoError(t, err)

	cpu1Flake := idgen.NextId()
	cpu1ID := cpu1Flake.String()
	cpu1Created, err := flakeutil.ParseFlakeID(cpu1ID)
	require.NoError(t, err)

	cpu2Flake := idgen.NextId()
	cpu2ID := cpu2Flake.String()
	cpu2Created, err := flakeutil.ParseFlakeID(cpu2ID)
	require.NoError(t, err)

	cpu3Flake := idgen.NextId()
	cpu3ID := cpu3Flake.String()
	cpu3Created, err := flakeutil.ParseFlakeID(cpu3ID)
	require.NoError(t, err)

	idx := wal.NewIndex()
	f1 := wal.SegmentInfo{
		Prefix:    "db_Cpu",
		Ulid:      cpu1ID,
		Path:      filepath.Join(dir, wal.Filename("db", "Cpu", "", cpu1ID)),
		Size:      50 * 1024,
		CreatedAt: cpu1Created,
	}
	idx.Add(f1)

	f2 := wal.SegmentInfo{
		Prefix:    "db_Cpu",
		Ulid:      cpu2ID,
		Path:      filepath.Join(dir, wal.Filename("db", "Cpu", "", cpu2ID)),
		Size:      50 * 1024,
		CreatedAt: cpu2Created,
	}
	idx.Add(f2)

	f3 := wal.SegmentInfo{
		Prefix:    "db_Cpu",
		Ulid:      cpu3ID,
		Path:      filepath.Join(dir, wal.Filename("db", "Cpu", "", cpu3ID)),
		Size:      1024,
		CreatedAt: cpu3Created,
	}
	idx.Add(f3)

	f4 := wal.SegmentInfo{
		Prefix:    "db_Disk",
		Ulid:      diskID,
		Path:      filepath.Join(dir, wal.Filename("db", "Disk", "", diskID)),
		Size:      100,
		CreatedAt: diskCreated,
	}
	idx.Add(f4)

	countMetric, sizeMetric, ageMetric := newTestMetrics()
	a := &batcher{
		hostname:                "node1",
		storageDir:              dir,
		maxTransferSize:         100 * 1024,
		minUploadSize:           100 * 1024,
		maxTransferAge:          1000 * 24 * time.Hour,
		maxBatchSegments:        25,
		Partitioner:             &fakePartitioner{owner: "node1"},
		Segmenter:               idx,
		health:                  fakeHealthChecker{healthy: true},
		segments:                partmap.NewMap[int](64),
		segmentsCountMetric:     countMetric,
		segmentsSizeBytesMetric: sizeMetric,
		segmentsMaxAgeMetric:    ageMetric,
	}
	owned, notOwned, err := a.processSegments()

	require.NoError(t, err)
	require.Equal(t, 3, len(owned))
	require.Equal(t, 0, len(notOwned))

	require.Equal(t, []string{f1.Path, f2.Path}, owned[0].Paths())
	require.Equal(t, []string{f3.Path}, owned[1].Paths())
	require.Equal(t, []string{f4.Path}, owned[2].Paths())

	requireValidBatch(t, owned)
	requireValidBatch(t, notOwned)
}

func TestBatcher_MaxSegmentCount(t *testing.T) {
	dir := t.TempDir()

	idgen, err := flake.New()
	require.NoError(t, err)

	// Disk is created first so it sorts as the oldest prefix; processSegments
	// then processes the newer Cpu prefix first.
	diskFlake := idgen.NextId()
	diskID := diskFlake.String()
	diskCreated, err := flakeutil.ParseFlakeID(diskID)
	require.NoError(t, err)

	cpu1Flake := idgen.NextId()
	cpu1ID := cpu1Flake.String()
	cpu1Created, err := flakeutil.ParseFlakeID(cpu1ID)
	require.NoError(t, err)

	cpu2Flake := idgen.NextId()
	cpu2ID := cpu2Flake.String()
	cpu2Created, err := flakeutil.ParseFlakeID(cpu2ID)
	require.NoError(t, err)

	cpu3Flake := idgen.NextId()
	cpu3ID := cpu3Flake.String()
	cpu3Created, err := flakeutil.ParseFlakeID(cpu3ID)
	require.NoError(t, err)

	idx := wal.NewIndex()
	f1 := wal.SegmentInfo{
		Prefix:    "db_Cpu",
		Ulid:      cpu1ID,
		Path:      filepath.Join(dir, wal.Filename("db", "Cpu", "", cpu1ID)),
		Size:      50 * 1024,
		CreatedAt: cpu1Created,
	}
	idx.Add(f1)

	f2 := wal.SegmentInfo{
		Prefix:    "db_Cpu",
		Ulid:      cpu2ID,
		Path:      filepath.Join(dir, wal.Filename("db", "Cpu", "", cpu2ID)),
		Size:      50 * 1024,
		CreatedAt: cpu2Created,
	}
	idx.Add(f2)

	f3 := wal.SegmentInfo{
		Prefix:    "db_Cpu",
		Ulid:      cpu3ID,
		Path:      filepath.Join(dir, wal.Filename("db", "Cpu", "", cpu3ID)),
		Size:      1024,
		CreatedAt: cpu3Created,
	}
	idx.Add(f3)

	f4 := wal.SegmentInfo{
		Prefix:    "db_Disk",
		Ulid:      diskID,
		Path:      filepath.Join(dir, wal.Filename("db", "Disk", "", diskID)),
		Size:      100,
		CreatedAt: diskCreated,
	}
	idx.Add(f4)

	countMetric, sizeMetric, ageMetric := newTestMetrics()
	a := &batcher{
		hostname:                "node1",
		storageDir:              dir,
		maxTransferSize:         100 * 1024,
		minUploadSize:           100 * 1024,
		maxBatchSegments:        1,
		Partitioner:             &fakePartitioner{owner: "node1"},
		Segmenter:               idx,
		health:                  fakeHealthChecker{healthy: true},
		segments:                partmap.NewMap[int](64),
		segmentsCountMetric:     countMetric,
		segmentsSizeBytesMetric: sizeMetric,
		segmentsMaxAgeMetric:    ageMetric,
	}
	owned, notOwned, err := a.processSegments()

	require.NoError(t, err)
	require.Equal(t, 4, len(owned))
	require.Equal(t, 0, len(notOwned))

	require.Equal(t, []string{f1.Path}, owned[0].Paths())
	require.Equal(t, []string{f2.Path}, owned[1].Paths())
	require.Equal(t, []string{f3.Path}, owned[2].Paths())
	require.Equal(t, []string{f4.Path}, owned[3].Paths())

	requireValidBatch(t, owned)
	requireValidBatch(t, notOwned)
}

func TestBatcher_Stats(t *testing.T) {
	dir := t.TempDir()

	idgen, err := flake.New()
	require.NoError(t, err)

	diskFlake := idgen.NextId()
	diskID := diskFlake.String()
	diskCreated, err := flakeutil.ParseFlakeID(diskID)
	require.NoError(t, err)

	cpu1Flake := idgen.NextId()
	cpu1ID := cpu1Flake.String()
	cpu1Created, err := flakeutil.ParseFlakeID(cpu1ID)
	require.NoError(t, err)

	cpu2Flake := idgen.NextId()
	cpu2ID := cpu2Flake.String()
	cpu2Created, err := flakeutil.ParseFlakeID(cpu2ID)
	require.NoError(t, err)

	cpu3Flake := idgen.NextId()
	cpu3ID := cpu3Flake.String()
	cpu3Created, err := flakeutil.ParseFlakeID(cpu3ID)
	require.NoError(t, err)

	var segments []wal.SegmentInfo

	idx := wal.NewIndex()
	f1 := wal.SegmentInfo{
		Prefix:    "db_Cpu",
		Ulid:      cpu1ID,
		Path:      filepath.Join(dir, wal.Filename("db", "Cpu", "", cpu1ID)),
		Size:      50 * 1024,
		CreatedAt: cpu1Created,
	}
	idx.Add(f1)
	segments = append(segments, f1)

	f2 := wal.SegmentInfo{
		Prefix:    "db_Cpu",
		Ulid:      cpu2ID,
		Path:      filepath.Join(dir, wal.Filename("db", "Cpu", "", cpu2ID)),
		Size:      50 * 1024,
		CreatedAt: cpu2Created,
	}
	idx.Add(f2)
	segments = append(segments, f2)

	f3 := wal.SegmentInfo{
		Prefix:    "db_Cpu",
		Ulid:      cpu3ID,
		Path:      filepath.Join(dir, wal.Filename("db", "Cpu", "", cpu3ID)),
		Size:      1024,
		CreatedAt: cpu3Created,
	}
	idx.Add(f3)
	segments = append(segments, f3)

	f4 := wal.SegmentInfo{
		Prefix:    "db_Disk",
		Ulid:      diskID,
		Path:      filepath.Join(dir, wal.Filename("db", "Disk", "", diskID)),
		Size:      100,
		CreatedAt: diskCreated,
	}
	idx.Add(f4)
	segments = append(segments, f4)

	countMetric, sizeMetric, ageMetric := newTestMetrics()
	a := &batcher{
		hostname:                "node1",
		storageDir:              dir,
		maxTransferSize:         100 * 1024,
		minUploadSize:           100 * 1024,
		maxTransferAge:          time.Minute,
		Partitioner:             &fakePartitioner{owner: "node1"},
		Segmenter:               idx,
		health:                  fakeHealthChecker{healthy: true},
		segments:                partmap.NewMap[int](64),
		segmentsCountMetric:     countMetric,
		segmentsSizeBytesMetric: sizeMetric,
		segmentsMaxAgeMetric:    ageMetric,
	}

	owned, _, err := a.processSegments()
	require.NoError(t, err)
	require.Equal(t, int64(4), a.SegmentsTotal())

	var sz int64
	for _, s := range segments {
		sz += s.Size
	}
	require.Equal(t, sz, a.SegmentsSize())

	_, _, err = a.processSegments()
	require.NoError(t, err)

	// No batches should be returned because existing segments are already assigned to batched and not released
	require.Equal(t, int64(4), a.SegmentsTotal())
	require.Equal(t, int64(103524), a.SegmentsSize())

	// Release all the segments so they can re-assigned to new batches
	for _, b := range owned {
		b.Release()
	}

	owned, _, err = a.processSegments()
	require.NoError(t, err)

	// No batches should be returned because existing segments are already assigned to batched and not released
	require.Equal(t, int64(4), a.SegmentsTotal())
	require.Equal(t, sz, a.SegmentsSize())

	idx.Remove(f1)
	segments = segments[1:]
	sz = 0
	for _, s := range segments {
		sz += s.Size
	}

	// Release all the segments so they can re-assigned to new batches
	for _, b := range owned {
		b.Release()
	}

	_, _, err = a.processSegments()
	require.NoError(t, err)
	require.Equal(t, int64(3), a.SegmentsTotal())
	require.Equal(t, sz, a.SegmentsSize())

}

func requireValidBatch(t *testing.T, batch []*Batch) {
	for _, o := range batch {
		require.NotEmptyf(t, o.Table, "batch segment %v has no ID", o)
		require.NotEmptyf(t, o.Database, "batch segment %v has no ID", o)
		require.NotNilf(t, o.batcher, "batch segment %v has no batcher", o)
		require.True(t, len(o.Paths()) > 0, "batch segment %v has no paths", o)
	}
}

type fakePartitioner struct {
	owner string
	addr  string
}

func (f *fakePartitioner) Owner(b []byte) (string, string) {
	return f.owner, f.addr
}

type fakeSegmenter struct {
	active string
}

func (f *fakeSegmenter) IsActiveSegment(path string) bool {
	return path == f.active
}

type fakeHealthChecker struct {
	healthy bool
}

func (f fakeHealthChecker) IsPeerHealthy(peer string) bool { return true }
func (f fakeHealthChecker) SetPeerUnhealthy(peer string)   {}
func (f fakeHealthChecker) SetPeerHealthy(peer string)     {}
