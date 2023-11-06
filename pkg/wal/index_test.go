package wal

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIndex_Oldest(t *testing.T) {
	i := NewIndex()

	require.Equal(t, "", i.OldestPrefix())
	i.Add(SegmentInfo{Prefix: "test", Ulid: "test", Path: "test", Size: 1, CreatedAt: time.Unix(1, 0)})

	require.Equal(t, "test", i.OldestPrefix())

	i.Add(SegmentInfo{Prefix: "test1", Ulid: "test", Path: "test", Size: 2, CreatedAt: time.Unix(2, 0)})
	require.Equal(t, "test", i.OldestPrefix())

	i.Add(SegmentInfo{Prefix: "test2", Ulid: "test", Path: "test", Size: 0, CreatedAt: time.Unix(0, 0)})
	require.Equal(t, "test2", i.OldestPrefix())
}

func TestIndex_Remove(t *testing.T) {
	i := NewIndex()

	require.Equal(t, "", i.OldestPrefix())
	i.Add(SegmentInfo{Prefix: "test", Ulid: "test", Path: "/test", Size: 1, CreatedAt: time.Unix(1, 0)})

	require.Equal(t, "test", i.OldestPrefix())

	i.Add(SegmentInfo{Prefix: "test1", Ulid: "test", Path: "/test1", Size: 2, CreatedAt: time.Unix(2, 0)})
	require.Equal(t, "test", i.OldestPrefix())

	s := SegmentInfo{Prefix: "test2", Ulid: "test", Path: "/test2", Size: 0, CreatedAt: time.Unix(0, 0)}
	i.Add(s)
	require.Equal(t, "test2", i.OldestPrefix())

	i.Remove(s)
	require.Equal(t, "test", i.OldestPrefix())
}

func TestIndex_LargetSizePrefix(t *testing.T) {
	i := NewIndex()

	require.Equal(t, "", i.LargestSizePrefix())
	i.Add(SegmentInfo{Prefix: "test", Ulid: "test", Path: "/test", Size: 1, CreatedAt: time.Unix(1, 0)})

	require.Equal(t, "test", i.LargestSizePrefix())

	i.Add(SegmentInfo{Prefix: "test1", Ulid: "test", Path: "/test1", Size: 2, CreatedAt: time.Unix(2, 0)})
	require.Equal(t, "test1", i.LargestSizePrefix())

	i.Add(SegmentInfo{Prefix: "test2", Ulid: "test", Path: "/test2", Size: 0, CreatedAt: time.Unix(0, 0)})
	require.Equal(t, "test1", i.LargestSizePrefix())
}

func TestIndex_LargetCountPrefix(t *testing.T) {
	i := NewIndex()

	require.Equal(t, "", i.LargestCountPrefix())
	i.Add(SegmentInfo{Prefix: "test", Ulid: "test", Path: "/test", Size: 1, CreatedAt: time.Unix(1, 0)})

	require.Equal(t, "test", i.LargestCountPrefix())

	i.Add(SegmentInfo{Prefix: "test1", Ulid: "test", Path: "/test1", Size: 2, CreatedAt: time.Unix(2, 0)})

	// Ties go to segments created first
	require.Equal(t, "test", i.LargestCountPrefix())

	i.Add(SegmentInfo{Prefix: "test1", Ulid: "test", Path: "/test2", Size: 0, CreatedAt: time.Unix(0, 0)})
	require.Equal(t, "test1", i.LargestCountPrefix())
}

func TestIndex_TotalSegments(t *testing.T) {
	i := NewIndex()

	require.Equal(t, 0, i.TotalSegments())
	i.Add(SegmentInfo{Prefix: "test", Ulid: "test", Path: "/test", Size: 1, CreatedAt: time.Unix(1, 0)})

	require.Equal(t, 1, i.TotalSegments())

	i.Add(SegmentInfo{Prefix: "test1", Ulid: "test", Path: "/test1", Size: 2, CreatedAt: time.Unix(2, 0)})

	require.Equal(t, 2, i.TotalSegments())
}

func TestIndex_TotalPrefixes(t *testing.T) {
	i := NewIndex()

	require.Equal(t, 0, i.TotalPrefixes())
	i.Add(SegmentInfo{Prefix: "test", Ulid: "test", Path: "/test", Size: 1, CreatedAt: time.Unix(1, 0)})

	require.Equal(t, 1, i.TotalPrefixes())

	info := SegmentInfo{Prefix: "test1", Ulid: "test", Path: "/test1", Size: 2, CreatedAt: time.Unix(2, 0)}
	i.Add(info)

	require.Equal(t, 2, i.TotalPrefixes())

	i.Remove(info)
	require.Equal(t, 1, i.TotalPrefixes())
}

func TestIndex_PrefixedBySize(t *testing.T) {
	i := NewIndex()

	require.Equal(t, 0, len(i.PrefixesBySize()))
	i.Add(SegmentInfo{Prefix: "test", Ulid: "test", Path: "/test", Size: 1, CreatedAt: time.Unix(1, 0)})

	require.Equal(t, 1, len(i.PrefixesBySize()))
	require.Equal(t, "test", i.PrefixesBySize()[0])

	i.Add(SegmentInfo{Prefix: "test1", Ulid: "test", Path: "/test1", Size: 2, CreatedAt: time.Unix(2, 0)})
	require.Equal(t, 2, len(i.PrefixesBySize()))
	require.Equal(t, "test", i.PrefixesBySize()[0])
	require.Equal(t, "test1", i.PrefixesBySize()[1])

	i.Add(SegmentInfo{Prefix: "test2", Ulid: "test", Path: "/test2", Size: 0, CreatedAt: time.Unix(0, 0)})
	require.Equal(t, 3, len(i.PrefixesBySize()))
	require.Equal(t, "test2", i.PrefixesBySize()[0])
	require.Equal(t, "test", i.PrefixesBySize()[1])
	require.Equal(t, "test1", i.PrefixesBySize()[2])
}

func TestIndes_PrefixesByAge(t *testing.T) {
	i := NewIndex()

	require.Equal(t, 0, len(i.PrefixesByAge()))
	i.Add(SegmentInfo{Prefix: "test", Ulid: "test", Path: "/test", Size: 1, CreatedAt: time.Unix(1, 0)})

	require.Equal(t, 1, len(i.PrefixesByAge()))
	require.Equal(t, "test", i.PrefixesByAge()[0])

	i.Add(SegmentInfo{Prefix: "test1", Ulid: "test", Path: "/test1", Size: 2, CreatedAt: time.Unix(2, 0)})
	require.Equal(t, 2, len(i.PrefixesByAge()))
	require.Equal(t, "test", i.PrefixesByAge()[0])
	require.Equal(t, "test1", i.PrefixesByAge()[1])

	i.Add(SegmentInfo{Prefix: "test2", Ulid: "test", Path: "/test2", Size: 0, CreatedAt: time.Unix(0, 0)})
	require.Equal(t, 3, len(i.PrefixesByAge()))
	require.Equal(t, "test2", i.PrefixesByAge()[0])
	require.Equal(t, "test", i.PrefixesByAge()[1])
	require.Equal(t, "test1", i.PrefixesByAge()[2])
}

func TestIndex_PrefixesByCount(t *testing.T) {
	i := NewIndex()

	require.Equal(t, 0, len(i.PrefixesByCount()))
	i.Add(SegmentInfo{Prefix: "test", Ulid: "test", Path: "/test", Size: 1, CreatedAt: time.Unix(1, 0)})

	require.Equal(t, 1, len(i.PrefixesByCount()))
	require.Equal(t, "test", i.PrefixesByCount()[0])

	i.Add(SegmentInfo{Prefix: "test1", Ulid: "test", Path: "/test1", Size: 2, CreatedAt: time.Unix(2, 0)})
	require.Equal(t, 2, len(i.PrefixesByCount()))
	require.Equal(t, "test", i.PrefixesByCount()[0])
	require.Equal(t, "test1", i.PrefixesByCount()[1])

	i.Add(SegmentInfo{Prefix: "test1", Ulid: "test", Path: "/test2", Size: 0, CreatedAt: time.Unix(0, 0)})
	require.Equal(t, 2, len(i.PrefixesByCount()))
	require.Equal(t, "test", i.PrefixesByCount()[0])
	require.Equal(t, "test1", i.PrefixesByCount()[1])

}
