package cluster

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/ingestpolicy"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/stretchr/testify/require"
)

// agedSegments returns segments of db_Cpu in path order, created age ago each.
func agedSegments(t *testing.T, ages ...time.Duration) []wal.SegmentInfo {
	t.Helper()
	dir := t.TempDir()
	var segments []wal.SegmentInfo
	for i, age := range ages {
		id := fmt.Sprintf("2f2c74ffa19e%04d", i)
		segments = append(segments, wal.SegmentInfo{
			Prefix:    "db_Cpu",
			Ulid:      id,
			Path:      filepath.Join(dir, wal.Filename("db", "Cpu", "", id)),
			Size:      100,
			CreatedAt: time.Now().Add(-age),
			Priority:  ingestpolicy.PriorityQueued,
		})
	}
	return segments
}

func batchPaths(batches []*Batch) [][]string {
	var out [][]string
	for _, b := range batches {
		var paths []string
		for _, s := range b.Segments {
			paths = append(paths, filepath.Base(s.Path))
		}
		out = append(out, paths)
	}
	return out
}

func names(segments []wal.SegmentInfo, i ...int) []string {
	var out []string
	for _, n := range i {
		out = append(out, filepath.Base(segments[n].Path))
	}
	return out
}

// Without a transfer age, a peer's segments are transferred in one batch however old they are.
func TestBatcher_SplitPrefixWithoutTransferAge(t *testing.T) {
	b := newPriorityTestBatcher(t, wal.NewIndex(), "node2")
	b.maxTransferAge = 0
	segments := agedSegments(t, time.Hour, time.Hour, time.Second)

	owned, notOwned := b.splitPrefix("db_Cpu", segments)
	require.Empty(t, owned)
	require.Equal(t, [][]string{names(segments, 0, 1, 2)}, batchPaths(notOwned))
}

// Segments older than the transfer age are uploaded here rather than transferred; younger ones are transferred.
func TestBatcher_SplitPrefixUploadsSegmentsPastTransferAge(t *testing.T) {
	b := newPriorityTestBatcher(t, wal.NewIndex(), "node2")
	segments := agedSegments(t, time.Hour, time.Hour, time.Hour, time.Second, time.Second)

	owned, notOwned := b.splitPrefix("db_Cpu", segments)
	require.Equal(t, [][]string{names(segments, 0), names(segments, 1), names(segments, 2)}, batchPaths(owned))
	require.Equal(t, [][]string{names(segments, 3, 4)}, batchPaths(notOwned))
}

// A table owned here is uploaded here, and its segments past the transfer age are each uploaded alone.
func TestBatcher_SplitPrefixOwnedSegmentsPastTransferAge(t *testing.T) {
	b := newPriorityTestBatcher(t, wal.NewIndex(), "node1")
	segments := agedSegments(t, time.Hour, time.Hour, time.Second, time.Second)

	owned, notOwned := b.splitPrefix("db_Cpu", segments)
	require.Empty(t, notOwned)
	require.Equal(t, [][]string{names(segments, 0), names(segments, 1), names(segments, 2, 3)}, batchPaths(owned))
}
