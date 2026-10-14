package wal

import (
	"container/heap"
	"fmt"
	"io"
	"sort"
	"sync"
	"sync/atomic"
	"text/tabwriter"
	"time"

	"github.com/Azure/adx-mon/pkg/ingestpolicy"
)

// Index provides overview of all segments in a repository.
type Index struct {
	mu       sync.RWMutex
	segments map[string][]SegmentInfo

	totalSize int64

	// sizeByPriority is the total size of segments for each ingestion priority.
	sizeByPriority [ingestpolicy.NumPriorities]int64

	// ages orders segments by creation time so the oldest segment is found without scanning all segments.
	// agesByPath finds a segment's entry for removal, linking entries for duplicate paths.  Both are guarded by mu.
	ages       ageHeap
	agesByPath map[string]*ageEntry

	// subscribers is a copy-on-write list read without locking by Add.  subMu serializes updates.
	subMu       sync.Mutex
	subscribers atomic.Pointer[[]*indexSubscriber]
}

type indexSubscriber struct {
	fn func(SegmentInfo)
}

// NewIndex returns a new index.
func NewIndex() *Index {
	return &Index{
		segments:   make(map[string][]SegmentInfo),
		agesByPath: make(map[string]*ageEntry),
	}
}

// Add adds a closed segment to the index and notifies subscribers.
func (i *Index) Add(s SegmentInfo) {
	i.mu.Lock()
	atomic.AddInt64(&i.totalSize, s.Size)
	atomic.AddInt64(i.prioritySize(s.Priority), s.Size)
	i.segments[s.Prefix] = append(i.segments[s.Prefix], s)
	e := &ageEntry{createdAt: s.CreatedAt, next: i.agesByPath[s.Path]}
	heap.Push(&i.ages, e)
	i.agesByPath[s.Path] = e
	i.mu.Unlock()

	if subs := i.subscribers.Load(); subs != nil {
		for _, sub := range *subs {
			sub.fn(s)
		}
	}
}

// Subscribe registers fn to be called each time a closed segment is added to the index.  fn is called synchronously
// by the goroutine that added the segment, after the index lock is released, so it must return quickly and must not
// block.  Segments added before Subscribe are not replayed; callers should read the index after subscribing to
// discover existing segments.  The returned func unsubscribes fn.
func (i *Index) Subscribe(fn func(SegmentInfo)) (unsubscribe func()) {
	sub := &indexSubscriber{fn: fn}

	i.subMu.Lock()
	var subs []*indexSubscriber
	if cur := i.subscribers.Load(); cur != nil {
		subs = append(subs, *cur...)
	}
	subs = append(subs, sub)
	i.subscribers.Store(&subs)
	i.subMu.Unlock()

	return func() {
		i.subMu.Lock()
		defer i.subMu.Unlock()

		cur := i.subscribers.Load()
		if cur == nil {
			return
		}
		subs := make([]*indexSubscriber, 0, len(*cur))
		for _, v := range *cur {
			if v != sub {
				subs = append(subs, v)
			}
		}
		i.subscribers.Store(&subs)
	}
}

// Get returns all segments for a given prefix.
func (i *Index) Get(infos []SegmentInfo, prefix string) []SegmentInfo {
	i.mu.RLock()
	defer i.mu.RUnlock()

	infos = append(infos[:0], i.segments[prefix]...)

	return infos
}

// Remove removes a segment from the index.
func (i *Index) Remove(s SegmentInfo) {
	i.mu.Lock()
	defer i.mu.Unlock()

	segments := i.segments[s.Prefix]
	for idx, seg := range segments {
		if seg.Path == s.Path {
			segments = append(segments[:idx], segments[idx+1:]...)
			atomic.AddInt64(&i.totalSize, -s.Size)
			// Use the indexed priority since callers may not set it.
			atomic.AddInt64(i.prioritySize(seg.Priority), -s.Size)
			i.removeAge(seg.Path, seg.CreatedAt)

			if len(segments) == 0 {
				delete(i.segments, s.Prefix)
				break
			}
			i.segments[s.Prefix] = segments
			break
		}
	}
}

// Oldest returns the prefix of the oldest segment.
func (i *Index) OldestPrefix() string {
	i.mu.RLock()
	defer i.mu.RUnlock()

	var oldest SegmentInfo
	for _, segments := range i.segments {
		for _, seg := range segments {
			if oldest.CreatedAt.IsZero() || seg.CreatedAt.Before(oldest.CreatedAt) {
				oldest = seg
			}
		}
	}
	return oldest.Prefix
}

// LargestSizePrefix returns the prefix of the segment with the largest total size.
func (i *Index) LargestSizePrefix() string {
	i.mu.RLock()
	defer i.mu.RUnlock()

	var (
		prefix string
		size   int64
	)

	for _, segments := range i.segments {
		var sum int64
		for _, seg := range segments {
			sum += seg.Size
		}

		if sum > size || prefix == "" {
			size = sum
			prefix = segments[0].Prefix
		}
	}

	return prefix
}

// LargestCountPrefix returns the prefix of the segment with the largest total count.
func (i *Index) LargestCountPrefix() string {
	i.mu.RLock()
	defer i.mu.RUnlock()

	var (
		prefix string
		count  int
		minAge time.Time
	)

	for _, segments := range i.segments {
		var age time.Time
		for _, seg := range segments {
			if age.IsZero() || seg.CreatedAt.Before(age) {
				age = seg.CreatedAt
			}
		}

		if len(segments) > count || prefix == "" {
			count = len(segments)
			prefix = segments[0].Prefix
			minAge = age
			continue
		}

		// If there is a tie, use the oldest segment.
		if len(segments) == count && age.Before(minAge) {
			count = len(segments)
			prefix = segments[0].Prefix
			minAge = age
		}
	}

	return prefix
}

// TotalSegments returns the total number of segments in the index.
func (i *Index) TotalSegments() int {
	i.mu.RLock()
	defer i.mu.RUnlock()

	var count int
	for _, segments := range i.segments {
		count += len(segments)
	}
	return count
}

// TotalPrefixes returns the total number of prefixes in the index.
func (i *Index) TotalPrefixes() int {
	i.mu.RLock()
	defer i.mu.RUnlock()

	return len(i.segments)
}

// PrefixesBySize returns all prefixes sorted by total size least to greatest.
func (i *Index) PrefixesBySize() []string {
	i.mu.RLock()
	defer i.mu.RUnlock()

	var prefixes []string
	for prefix := range i.segments {
		prefixes = append(prefixes, prefix)
	}

	sizes := make(map[string]int64)
	for _, prefix := range prefixes {
		for _, seg := range i.segments[prefix] {
			sizes[prefix] += seg.Size
		}
	}

	sort.Slice(prefixes, func(i, j int) bool {
		return sizes[prefixes[i]] < sizes[prefixes[j]]
	})

	return prefixes
}

// Prefixes appends all prefixes to dst in no particular order and returns it.
func (i *Index) Prefixes(dst []string) []string {
	i.mu.RLock()
	defer i.mu.RUnlock()

	for prefix := range i.segments {
		dst = append(dst, prefix)
	}
	return dst
}

// PrefixesByAge returns all prefixes sorted by oldest to newest.
func (i *Index) PrefixesByAge() []string {
	i.mu.RLock()
	defer i.mu.RUnlock()

	var prefixes []string
	for prefix := range i.segments {
		prefixes = append(prefixes, prefix)
	}

	ages := make(map[string]time.Time)
	for _, prefix := range prefixes {
		for _, seg := range i.segments[prefix] {
			if ages[prefix].IsZero() || seg.CreatedAt.Before(ages[prefix]) {
				ages[prefix] = seg.CreatedAt
			}
		}
	}

	sort.Slice(prefixes, func(i, j int) bool {
		return ages[prefixes[i]].Before(ages[prefixes[j]])
	})

	return prefixes
}

// PrefixesByCount returns all prefixes sorted by total count least to greatest.  If there is a tie, the prefix
// that is lexicographically first is returned.
func (i *Index) PrefixesByCount() []string {
	i.mu.RLock()
	defer i.mu.RUnlock()

	var prefixes []string
	for prefix := range i.segments {
		prefixes = append(prefixes, prefix)
	}

	counts := make(map[string]int)
	for _, prefix := range prefixes {
		counts[prefix] = len(i.segments[prefix])
	}

	sort.Slice(prefixes, func(i, j int) bool {
		if counts[prefixes[i]] == counts[prefixes[j]] {
			return prefixes[i] < prefixes[j]
		}
		return counts[prefixes[i]] < counts[prefixes[j]]
	})

	return prefixes
}

// TotalSize returns the total size of all segments in the index.
func (i *Index) TotalSize() int64 {
	return atomic.LoadInt64(&i.totalSize)
}

// TotalSizeByPriority returns the total size of segments in the index with the given ingestion priority.
func (i *Index) TotalSizeByPriority(p ingestpolicy.Priority) int64 {
	return atomic.LoadInt64(i.prioritySize(p))
}

// prioritySize returns the size counter for p.  Unknown priorities are counted as queued.
func (i *Index) prioritySize(p ingestpolicy.Priority) *int64 {
	if int(p) >= len(i.sizeByPriority) {
		p = ingestpolicy.PriorityQueued
	}
	return &i.sizeByPriority[p]
}

// OldestSegmentAge returns the age of the oldest segment in the index or 0 if the index is empty.
func (i *Index) OldestSegmentAge() time.Duration {
	i.mu.RLock()
	defer i.mu.RUnlock()

	if len(i.ages) == 0 {
		return 0
	}
	return time.Since(i.ages[0].createdAt)
}

// removeAge removes the age entry for the segment at path created at createdAt.  i.mu must be held for writing.
func (i *Index) removeAge(path string, createdAt time.Time) {
	var prev *ageEntry
	for e := i.agesByPath[path]; e != nil; prev, e = e, e.next {
		if !e.createdAt.Equal(createdAt) {
			continue
		}
		heap.Remove(&i.ages, e.index)
		switch {
		case prev != nil:
			prev.next = e.next
		case e.next != nil:
			i.agesByPath[path] = e.next
		default:
			delete(i.agesByPath, path)
		}
		return
	}
}

// ageEntry is a segment's creation time in an ageHeap.
type ageEntry struct {
	createdAt time.Time
	index     int
	// next links entries for segments with the same path.
	next *ageEntry
}

// ageHeap is a min-heap of segment creation times.
type ageHeap []*ageEntry

func (h ageHeap) Len() int { return len(h) }

func (h ageHeap) Less(a, b int) bool { return h[a].createdAt.Before(h[b].createdAt) }

func (h ageHeap) Swap(a, b int) {
	h[a], h[b] = h[b], h[a]
	h[a].index = a
	h[b].index = b
}

func (h *ageHeap) Push(x any) {
	e := x.(*ageEntry)
	e.index = len(*h)
	*h = append(*h, e)
}

func (h *ageHeap) Pop() any {
	old := *h
	n := len(old)
	e := old[n-1]
	old[n-1] = nil
	*h = old[:n-1]
	return e
}

func (i *Index) WriteDebug(w io.Writer) error {
	_, _ = fmt.Fprintf(w, "Index: Disk Usage: %d, Segments: %d, Prefixes: %d\n\n", i.TotalSize(), i.TotalSegments(), i.TotalPrefixes())
	tw := tabwriter.NewWriter(w, 4, 0, 2, ' ', 0)
	tw.Write([]byte("Prefix\tSegments\tSize\tCreatedAt\n"))
	i.mu.RLock()
	for prefix, segments := range i.segments {
		var size int64
		for _, seg := range segments {
			size += seg.Size
		}
		tw.Write([]byte(fmt.Sprintf("%s\t%d\t%d\t%s\n", prefix, len(segments), size, segments[0].CreatedAt.Format(time.RFC3339))))
	}
	i.mu.RUnlock()
	tw.Flush()
	w.Write([]byte("\n"))
	return nil
}
