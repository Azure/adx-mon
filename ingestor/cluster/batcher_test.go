package cluster

import (
	"path/filepath"
	"testing"
	"time"

	flakeutil "github.com/Azure/adx-mon/pkg/flake"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/davidnarayan/go-flake"
	"github.com/stretchr/testify/require"
)

func TestBatcher_ClosedSegments(t *testing.T) {
	dir := t.TempDir()

	idx := wal.NewIndex()
	idx.Add(wal.SegmentInfo{
		Prefix:    "db_Cpu",
		Ulid:      "aaaa",
		Path:      filepath.Join(dir, wal.Filename("db", "Cpu", "aaaa")),
		Size:      100,
		CreatedAt: time.Unix(1, 0),
	})

	idx.Add(wal.SegmentInfo{
		Prefix:    "db_Cpu",
		Ulid:      "bbbb",
		Path:      filepath.Join(dir, wal.Filename("db", "Cpu", "bbbb")),
		Size:      100,
		CreatedAt: time.Unix(0, 0),
	})

	a := &batcher{
		hostname:    "node1",
		storageDir:  dir,
		Partitioner: &fakePartitioner{owner: "node1"},
		Segmenter:   idx,
		segments:    make(map[string]int),
	}
	owner, notOwned, err := a.processSegments()
	require.NoError(t, err)
	require.Equal(t, []string{filepath.Join(dir, wal.Filename("db", "Cpu", "aaaa"))}, owner[0].Paths())
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
		Path:      filepath.Join(dir, wal.Filename("db", "Cpu", now.String())),
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
		Path:      filepath.Join(dir, wal.Filename("db", "Cpu", now.String())),
		Size:      100,
		CreatedAt: created,
	}
	idx.Add(f2)

	a := &batcher{
		hostname:        "node1",
		storageDir:      dir,
		maxTransferAge:  30 * time.Second,
		maxTransferSize: 100 * 1024 * 1024,
		minUploadSize:   100 * 1024 * 1024,
		Partitioner:     &fakePartitioner{owner: "node2"},
		Segmenter:       idx,
		health:          &fakeHealthChecker{healthy: true},
		segments:        make(map[string]int),
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

	created, err := flakeutil.ParseFlakeID("2359cdac7d6f0001")
	require.NoError(t, err)

	idx := wal.NewIndex()
	f1 := wal.SegmentInfo{
		Prefix:    "db_Cpu",
		Ulid:      "2359cdac7d6f0001",
		Path:      filepath.Join(dir, wal.Filename("db", "Cpu", "2359cdac7d6f0001")),
		Size:      100,
		CreatedAt: created,
	}
	idx.Add(f1)

	created, err = flakeutil.ParseFlakeID("2359cd7e3aef0001")
	require.NoError(t, err)

	f2 := wal.SegmentInfo{
		Prefix:    "db_Disk",
		Ulid:      "2359cd7e3aef0001",
		Path:      filepath.Join(dir, wal.Filename("db", "Disk", "2359cd7e3aef0001")),
		Size:      100,
		CreatedAt: created,
	}
	idx.Add(f2)

	a := &batcher{
		hostname:    "node1",
		storageDir:  dir,
		Partitioner: &fakePartitioner{owner: "node1"},
		Segmenter:   idx,
		segments:    make(map[string]int),
	}
	owner, notOwned, err := a.processSegments()
	require.NoError(t, err)
	require.Equal(t, 2, len(owner))
	require.Equal(t, 0, len(notOwned))

	require.Equal(t, []string{filepath.Join(dir, wal.Filename("db", "Cpu", "2359cdac7d6f0001"))}, owner[0].Paths())
	require.Equal(t, "db", owner[0].Database)
	require.Equal(t, "Cpu", owner[0].Table)
	require.Equal(t, []string{filepath.Join(dir, wal.Filename("db", "Disk", "2359cd7e3aef0001"))}, owner[1].Paths())
	require.Equal(t, "db", owner[1].Database)
	require.Equal(t, "Disk", owner[1].Table)

	requireValidBatch(t, owner)
	requireValidBatch(t, notOwned)
}

func TestBatcher_BigFileBatch(t *testing.T) {
	dir := t.TempDir()

	created, err := flakeutil.ParseFlakeID("2359cdac8d6f0001")
	require.NoError(t, err)

	idx := wal.NewIndex()
	f1 := wal.SegmentInfo{
		Prefix:    "db_Cpu",
		Ulid:      "2359cdac8d6f0001",
		Path:      filepath.Join(dir, wal.Filename("db", "Cpu", "2359cdac8d6f0001")),
		Size:      100 * 1024, // Meets min transfer size, separate batch
		CreatedAt: created,
	}
	idx.Add(f1)

	created, err = flakeutil.ParseFlakeID("2359cdac9d6f0001")
	require.NoError(t, err)

	f2 := wal.SegmentInfo{
		Prefix:    "db_Cpu",
		Ulid:      "2359cdac9d6f0001",
		Path:      filepath.Join(dir, wal.Filename("db", "Cpu", "2359cdac9d6f0001")),
		Size:      100, // This should be in a new batch
		CreatedAt: created,
	}
	idx.Add(f2)

	created, err = flakeutil.ParseFlakeID("2359cd7e3aef0001")
	require.NoError(t, err)

	f3 := wal.SegmentInfo{
		Prefix:    "db_Disk",
		Ulid:      "2359cd7e3aef0001",
		Path:      filepath.Join(dir, wal.Filename("db", "Disk", "2359cd7e3aef0001")),
		Size:      100,
		CreatedAt: created,
	}
	idx.Add(f3)

	a := &batcher{
		hostname:        "node1",
		storageDir:      dir,
		maxTransferSize: 100 * 1024,
		minUploadSize:   100 * 1024,
		maxTransferAge:  time.Minute,
		Partitioner:     &fakePartitioner{owner: "node1"},
		Segmenter:       idx,
		health:          fakeHealthChecker{healthy: true},
		segments:        make(map[string]int),
	}
	owned, notOwned, err := a.processSegments()

	require.NoError(t, err)
	require.Equal(t, 3, len(owned))
	require.Equal(t, 0, len(notOwned))

	require.Equal(t, []string{f1.Path}, owned[0].Paths())
	require.Equal(t, []string{f2.Path}, owned[1].Paths())
	require.Equal(t, []string{filepath.Join(dir, wal.Filename("db", "Disk", "2359cd7e3aef0001"))}, owned[2].Paths())

	requireValidBatch(t, owned)
	requireValidBatch(t, notOwned)
}

func TestBatcher_BigBatch(t *testing.T) {
	dir := t.TempDir()

	created, err := flakeutil.ParseFlakeID("2359cdac8d6f0001")
	require.NoError(t, err)

	idx := wal.NewIndex()
	f1 := wal.SegmentInfo{
		Prefix:    "db_Cpu",
		Ulid:      "2359cdac8d6f0001",
		Path:      filepath.Join(dir, wal.Filename("db", "Cpu", "2359cdac8d6f0001")),
		Size:      50 * 1024,
		CreatedAt: created,
	}
	idx.Add(f1)

	created, err = flakeutil.ParseFlakeID("2359cdac9d6f0001")
	require.NoError(t, err)

	f2 := wal.SegmentInfo{
		Prefix:    "db_Cpu",
		Ulid:      "2359cdac9d6f0001",
		Path:      filepath.Join(dir, wal.Filename("db", "Cpu", "2359cdac9d6f0001")),
		Size:      50 * 1024,
		CreatedAt: created,
	}
	idx.Add(f2)

	created, err = flakeutil.ParseFlakeID("2359cdacad6f0001")
	require.NoError(t, err)

	f3 := wal.SegmentInfo{
		Prefix:    "db_Cpu",
		Ulid:      "2359cdacad6f0001",
		Path:      filepath.Join(dir, wal.Filename("db", "Cpu", "2359cdacad6f0001")),
		Size:      1024,
		CreatedAt: created,
	}
	idx.Add(f3)

	created, err = flakeutil.ParseFlakeID("2359cd7e3aef0001")
	require.NoError(t, err)

	f4 := wal.SegmentInfo{
		Prefix:    "db_Disk",
		Ulid:      "2359cd7e3aef0001",
		Path:      filepath.Join(dir, wal.Filename("db", "Disk", "2359cd7e3aef0001")),
		Size:      100,
		CreatedAt: created,
	}
	idx.Add(f4)

	a := &batcher{
		hostname:        "node1",
		storageDir:      dir,
		maxTransferSize: 100 * 1024,
		minUploadSize:   100 * 1024,
		maxTransferAge:  time.Minute,
		Partitioner:     &fakePartitioner{owner: "node1"},
		Segmenter:       idx,
		health:          fakeHealthChecker{healthy: true},
		segments:        make(map[string]int),
	}
	owned, notOwned, err := a.processSegments()

	require.NoError(t, err)
	require.Equal(t, 3, len(owned))
	require.Equal(t, 0, len(notOwned))

	require.Equal(t, []string{f1.Path, f2.Path}, owned[0].Paths())
	require.Equal(t, []string{f3.Path}, owned[1].Paths())
	require.Equal(t, []string{filepath.Join(dir, wal.Filename("db", "Disk", "2359cd7e3aef0001"))}, owned[2].Paths())

	requireValidBatch(t, owned)
	requireValidBatch(t, notOwned)
}

func TestBatcher_Stats(t *testing.T) {
	dir := t.TempDir()

	created, err := flakeutil.ParseFlakeID("2359cdac8d6f0001")
	require.NoError(t, err)

	segments := []wal.SegmentInfo{}

	idx := wal.NewIndex()
	f1 := wal.SegmentInfo{
		Prefix:    "db_Cpu",
		Ulid:      "2359cdac8d6f0001",
		Path:      filepath.Join(dir, wal.Filename("db", "Cpu", "2359cdac8d6f0001")),
		Size:      50 * 1024,
		CreatedAt: created,
	}
	idx.Add(f1)
	segments = append(segments, f1)

	created, err = flakeutil.ParseFlakeID("2359cdac9d6f0001")
	require.NoError(t, err)

	f2 := wal.SegmentInfo{
		Prefix:    "db_Cpu",
		Ulid:      "2359cdac9d6f0001",
		Path:      filepath.Join(dir, wal.Filename("db", "Cpu", "2359cdac9d6f0001")),
		Size:      50 * 1024,
		CreatedAt: created,
	}
	idx.Add(f2)
	segments = append(segments, f2)

	created, err = flakeutil.ParseFlakeID("2359cdacad6f0001")
	require.NoError(t, err)

	f3 := wal.SegmentInfo{
		Prefix:    "db_Cpu",
		Ulid:      "2359cdacad6f0001",
		Path:      filepath.Join(dir, wal.Filename("db", "Cpu", "2359cdacad6f0001")),
		Size:      1024,
		CreatedAt: created,
	}
	idx.Add(f3)
	segments = append(segments, f3)

	created, err = flakeutil.ParseFlakeID("2359cd7e3aef0001")
	require.NoError(t, err)

	f4 := wal.SegmentInfo{
		Prefix:    "db_Disk",
		Ulid:      "2359cd7e3aef0001",
		Path:      filepath.Join(dir, wal.Filename("db", "Disk", "2359cd7e3aef0001")),
		Size:      100,
		CreatedAt: created,
	}
	idx.Add(f4)
	segments = append(segments, f4)

	a := &batcher{
		hostname:        "node1",
		storageDir:      dir,
		maxTransferSize: 100 * 1024,
		minUploadSize:   100 * 1024,
		maxTransferAge:  time.Minute,
		Partitioner:     &fakePartitioner{owner: "node1"},
		Segmenter:       idx,
		health:          fakeHealthChecker{healthy: true},
		segments:        make(map[string]int),
	}
	_, _, err = a.processSegments()
	require.NoError(t, err)
	require.Equal(t, int64(4), a.SegmentsTotal())

	println(a.UploadQueueSize(), a.TransferQueueSize())

	var sz int64
	for _, s := range segments {
		sz += s.Size
	}
	require.Equal(t, sz, a.SegmentsSize())

	_, _, err = a.processSegments()
	require.NoError(t, err)
	require.Equal(t, int64(4), a.SegmentsTotal())
	require.Equal(t, sz, a.SegmentsSize())
	println(a.UploadQueueSize(), a.TransferQueueSize())

	idx.Remove(f1)
	segments = segments[1:]
	sz = 0
	for _, s := range segments {
		sz += s.Size
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
}

func (f *fakePartitioner) Owner(b []byte) (string, string) {
	return f.owner, ""
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
