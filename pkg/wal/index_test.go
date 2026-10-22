package wal

import (
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/ingestpolicy"
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

func TestIndex_TotalSize(t *testing.T) {
	i := NewIndex()

	require.Equal(t, int64(0), i.TotalSize())
	i.Add(SegmentInfo{Prefix: "test", Ulid: "test", Path: "/test", Size: 1, CreatedAt: time.Unix(1, 0)})
	require.Equal(t, int64(1), i.TotalSize())
	remove1 := SegmentInfo{Prefix: "test", Ulid: "test1", Path: "/test1", Size: 1, CreatedAt: time.Unix(1, 0)}
	i.Add(remove1)

	require.Equal(t, int64(2), i.TotalSize())

	info := SegmentInfo{Prefix: "test1", Ulid: "test", Path: "/test1", Size: 2, CreatedAt: time.Unix(2, 0)}
	i.Add(info)
	require.Equal(t, int64(4), i.TotalSize())

	i.Add(SegmentInfo{Prefix: "test2", Ulid: "test", Path: "/test2", Size: 3, CreatedAt: time.Unix(0, 0)})
	require.Equal(t, int64(7), i.TotalSize())

	i.Remove(info)
	require.Equal(t, int64(5), i.TotalSize())
	i.Remove(remove1)
	require.Equal(t, int64(4), i.TotalSize())
}

func TestIndex_OldestSegmentAge(t *testing.T) {
	i := NewIndex()
	require.Equal(t, time.Duration(0), i.OldestSegmentAge())

	now := time.Now()
	i.Add(SegmentInfo{Prefix: "a", Ulid: "1", Path: "/a", Size: 1, CreatedAt: now.Add(-10 * time.Second)})
	i.Add(SegmentInfo{Prefix: "b", Ulid: "2", Path: "/b", Size: 2, CreatedAt: now.Add(-20 * time.Second)})
	i.Add(SegmentInfo{Prefix: "c", Ulid: "3", Path: "/c", Size: 3, CreatedAt: now.Add(-5 * time.Second)})

	age := i.OldestSegmentAge()
	require.GreaterOrEqual(t, age, 20*time.Second)
	require.Less(t, age, 21*time.Second)

	// Remove the oldest, check next oldest
	i.Remove(SegmentInfo{Prefix: "b", Ulid: "2", Path: "/b", Size: 2, CreatedAt: now.Add(-20 * time.Second)})
	age = i.OldestSegmentAge()
	require.GreaterOrEqual(t, age, 10*time.Second)
	require.Less(t, age, 11*time.Second)
}

func TestIndex_SubscribeNotifiesOnAdd(t *testing.T) {
	i := NewIndex()
	var got []SegmentInfo
	unsubscribe := i.Subscribe(func(si SegmentInfo) { got = append(got, si) })

	a := SegmentInfo{Prefix: "db_a", Path: "/a", Size: 1, Priority: 1}
	b := SegmentInfo{Prefix: "db_b", Path: "/b", Size: 2}
	i.Add(a)
	i.Add(b)
	require.Equal(t, []SegmentInfo{a, b}, got)

	unsubscribe()
	i.Add(SegmentInfo{Prefix: "db_c", Path: "/c"})
	require.Len(t, got, 2)

	// Unsubscribing twice is a no-op.
	unsubscribe()
}

func TestIndex_SubscribeMultiple(t *testing.T) {
	i := NewIndex()
	var a, b int
	unsubA := i.Subscribe(func(SegmentInfo) { a++ })
	i.Subscribe(func(SegmentInfo) { b++ })

	i.Add(SegmentInfo{Prefix: "db_a", Path: "/a"})
	unsubA()
	i.Add(SegmentInfo{Prefix: "db_a", Path: "/b"})

	require.Equal(t, 1, a)
	require.Equal(t, 2, b)
}

func TestIndex_SubscribeDoesNotReplay(t *testing.T) {
	i := NewIndex()
	i.Add(SegmentInfo{Prefix: "db_a", Path: "/a"})

	called := false
	i.Subscribe(func(SegmentInfo) { called = true })
	require.False(t, called)
}

func TestIndex_SubscriberCanReadIndex(t *testing.T) {
	// Subscribers are called after the index lock is released so they may query the index.
	i := NewIndex()
	var segments []SegmentInfo
	i.Subscribe(func(si SegmentInfo) {
		segments = i.Get(nil, si.Prefix)
	})

	i.Add(SegmentInfo{Prefix: "db_a", Path: "/a", Size: 1})
	require.Len(t, segments, 1)
}

func TestIndex_SubscribeConcurrent(t *testing.T) {
	i := NewIndex()
	var count atomic.Int64
	i.Subscribe(func(SegmentInfo) { count.Add(1) })

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for n := 0; n < 1000; n++ {
				i.Add(SegmentInfo{Prefix: fmt.Sprintf("db_%d", g), Path: fmt.Sprintf("/%d/%d", g, n)})
				if n%100 == 0 {
					unsub := i.Subscribe(func(SegmentInfo) {})
					unsub()
				}
			}
		}(g)
	}
	wg.Wait()

	require.Equal(t, int64(8000), count.Load())
	require.Equal(t, 8000, i.TotalSegments())
}

func BenchmarkIndex_Add(b *testing.B) {
	for _, subs := range []int{0, 1} {
		b.Run(fmt.Sprintf("subscribers=%d", subs), func(b *testing.B) {
			i := NewIndex()
			for n := 0; n < subs; n++ {
				i.Subscribe(func(SegmentInfo) {})
			}
			info := SegmentInfo{Prefix: "db_table", Path: "/p", Size: 1}

			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				i.Add(info)
				if len(i.segments["db_table"]) > 1024 {
					i.segments["db_table"] = i.segments["db_table"][:0]
				}
			}
		})
	}
}

func TestIndex_TotalSizeByPriority(t *testing.T) {
	i := NewIndex()
	i.Add(SegmentInfo{Prefix: "db_a", Path: "/a", Size: 10})
	i.Add(SegmentInfo{Prefix: "db_b", Path: "/b", Size: 20, Priority: ingestpolicy.PriorityRealtime})
	i.Add(SegmentInfo{Prefix: "db_b", Path: "/c", Size: 5, Priority: ingestpolicy.PriorityRealtime})

	require.Equal(t, int64(35), i.TotalSize())
	require.Equal(t, int64(10), i.TotalSizeByPriority(ingestpolicy.PriorityQueued))
	require.Equal(t, int64(25), i.TotalSizeByPriority(ingestpolicy.PriorityRealtime))

	// Remove uses the indexed priority even if the caller does not set it.
	i.Remove(SegmentInfo{Prefix: "db_b", Path: "/b", Size: 20})
	require.Equal(t, int64(15), i.TotalSize())
	require.Equal(t, int64(10), i.TotalSizeByPriority(ingestpolicy.PriorityQueued))
	require.Equal(t, int64(5), i.TotalSizeByPriority(ingestpolicy.PriorityRealtime))

	// Removing an unknown segment does not change sizes.
	i.Remove(SegmentInfo{Prefix: "db_b", Path: "/missing", Size: 100})
	require.Equal(t, int64(15), i.TotalSize())
}

func TestIndex_UnknownPriorityCountedAsQueued(t *testing.T) {
	i := NewIndex()
	i.Add(SegmentInfo{Prefix: "db_a", Path: "/a", Size: 10, Priority: 99})
	require.Equal(t, int64(10), i.TotalSizeByPriority(ingestpolicy.PriorityQueued))
	require.Equal(t, int64(10), i.TotalSizeByPriority(99))

	i.Remove(SegmentInfo{Prefix: "db_a", Path: "/a", Size: 10})
	require.Zero(t, i.TotalSizeByPriority(ingestpolicy.PriorityQueued))
}

func TestIndex_Prefixes(t *testing.T) {
	i := NewIndex()
	require.Empty(t, i.Prefixes(nil))

	i.Add(SegmentInfo{Prefix: "db_b", Path: "/b1", CreatedAt: time.Unix(2, 0)})
	i.Add(SegmentInfo{Prefix: "db_a", Path: "/a1", CreatedAt: time.Unix(1, 0)})
	i.Add(SegmentInfo{Prefix: "db_a", Path: "/a2", CreatedAt: time.Unix(3, 0)})

	prefixes := i.Prefixes([]string{"existing"})
	require.ElementsMatch(t, []string{"existing", "db_a", "db_b"}, prefixes)
	require.ElementsMatch(t, i.PrefixesByAge(), i.Prefixes(nil))
}

func newBenchmarkIndex(segments int) *Index {
	i := NewIndex()
	base := time.Now().Add(-time.Hour)
	for n := 0; n < segments; n++ {
		i.Add(SegmentInfo{
			Prefix:    fmt.Sprintf("db_t%d", n%1000),
			Path:      fmt.Sprintf("/wal/%d", n),
			Size:      1024,
			CreatedAt: base.Add(time.Duration(n) * time.Millisecond),
		})
	}
	return i
}

func BenchmarkIndex_OldestSegmentAge(b *testing.B) {
	for _, segments := range []int{1000, 10000} {
		b.Run(fmt.Sprintf("segments=%d", segments), func(b *testing.B) {
			i := newBenchmarkIndex(segments)
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				_ = i.OldestSegmentAge()
			}
		})
	}
}

func BenchmarkIndex_AddRemove(b *testing.B) {
	i := newBenchmarkIndex(10000)
	si := SegmentInfo{Prefix: "db_t1", Path: "/wal/new", Size: 1024, CreatedAt: time.Now()}
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		i.Add(si)
		i.Remove(si)
	}
}

// bruteOldest returns the creation time of the oldest segment by scanning all segments.
func bruteOldest(i *Index) (time.Time, bool) {
	var (
		oldest time.Time
		found  bool
	)
	for _, segments := range i.segments {
		for _, seg := range segments {
			if !found || seg.CreatedAt.Before(oldest) {
				oldest, found = seg.CreatedAt, true
			}
		}
	}
	return oldest, found
}

func requireAgesConsistent(t *testing.T, i *Index) {
	t.Helper()
	oldest, found := bruteOldest(i)
	if !found {
		require.Empty(t, i.ages)
		require.Empty(t, i.agesByPath)
		require.Zero(t, i.OldestSegmentAge())
		return
	}
	require.True(t, oldest.Equal(i.ages[0].createdAt), "heap %s, scan %s", i.ages[0].createdAt, oldest)

	var segments, entries int
	for _, s := range i.segments {
		segments += len(s)
	}
	for _, e := range i.agesByPath {
		for ; e != nil; e = e.next {
			entries++
		}
	}
	require.Equal(t, segments, len(i.ages))
	require.Equal(t, segments, entries)
	for n, e := range i.ages {
		require.Equal(t, n, e.index)
	}
}

func TestIndex_OldestSegmentAgeRemoveAll(t *testing.T) {
	i := NewIndex()
	require.Zero(t, i.OldestSegmentAge())

	now := time.Now()
	i.Add(SegmentInfo{Prefix: "db_a", Path: "/a1", CreatedAt: now.Add(-time.Minute)})
	i.Add(SegmentInfo{Prefix: "db_b", Path: "/b1", CreatedAt: now.Add(-time.Hour)})
	i.Add(SegmentInfo{Prefix: "db_a", Path: "/a2", CreatedAt: now.Add(-time.Second)})
	require.InDelta(t, time.Hour.Seconds(), i.OldestSegmentAge().Seconds(), 1)

	i.Remove(SegmentInfo{Prefix: "db_b", Path: "/b1"})
	require.InDelta(t, time.Minute.Seconds(), i.OldestSegmentAge().Seconds(), 1)

	// Removing an unknown segment does not change the oldest age.
	i.Remove(SegmentInfo{Prefix: "db_a", Path: "/missing"})
	require.InDelta(t, time.Minute.Seconds(), i.OldestSegmentAge().Seconds(), 1)

	i.Remove(SegmentInfo{Prefix: "db_a", Path: "/a1"})
	i.Remove(SegmentInfo{Prefix: "db_a", Path: "/a2"})
	requireAgesConsistent(t, i)
}

func TestIndex_OldestSegmentAgeDuplicatePaths(t *testing.T) {
	i := NewIndex()
	now := time.Now()
	i.Add(SegmentInfo{Prefix: "db_a", Path: "/a", CreatedAt: now.Add(-time.Hour)})
	i.Add(SegmentInfo{Prefix: "db_a", Path: "/a", CreatedAt: now.Add(-time.Minute)})
	i.Add(SegmentInfo{Prefix: "db_a", Path: "/a", CreatedAt: now.Add(-time.Second)})
	requireAgesConsistent(t, i)

	// Remove deletes the first segment with the path, so its creation time is removed from the heap.
	i.Remove(SegmentInfo{Prefix: "db_a", Path: "/a"})
	requireAgesConsistent(t, i)
	require.InDelta(t, time.Minute.Seconds(), i.OldestSegmentAge().Seconds(), 1)

	i.Remove(SegmentInfo{Prefix: "db_a", Path: "/a"})
	requireAgesConsistent(t, i)
	require.InDelta(t, time.Second.Seconds(), i.OldestSegmentAge().Seconds(), 1)

	i.Remove(SegmentInfo{Prefix: "db_a", Path: "/a"})
	requireAgesConsistent(t, i)
}

func TestIndex_OldestSegmentAgeMatchesScan(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	i := NewIndex()
	base := time.Now().Add(-time.Hour)

	var added []SegmentInfo
	for op := 0; op < 5000; op++ {
		if len(added) == 0 || r.Intn(3) != 0 {
			si := SegmentInfo{
				Prefix: fmt.Sprintf("db_t%d", r.Intn(5)),
				// Reuse paths and creation times to exercise duplicates and ties.
				Path:      fmt.Sprintf("/wal/%d", r.Intn(10)),
				Size:      1,
				CreatedAt: base.Add(time.Duration(r.Intn(1000)) * time.Second),
			}
			si.Path = si.Prefix + si.Path
			i.Add(si)
			added = append(added, si)
		} else {
			n := r.Intn(len(added))
			i.Remove(added[n])
			added = append(added[:n], added[n+1:]...)
		}
		requireAgesConsistent(t, i)
	}

	for _, si := range added {
		i.Remove(si)
	}
	requireAgesConsistent(t, i)
}

func TestIndex_OldestSegmentAgeConcurrent(t *testing.T) {
	i := NewIndex()
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for n := 0; n < 500; n++ {
				si := SegmentInfo{Prefix: "db_t", Path: fmt.Sprintf("/%d/%d", g, n), CreatedAt: time.Now()}
				i.Add(si)
				_ = i.OldestSegmentAge()
				i.Remove(si)
			}
		}(g)
	}
	wg.Wait()
	requireAgesConsistent(t, i)
}
