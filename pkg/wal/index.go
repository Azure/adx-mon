package wal

import (
	"fmt"
	"io"
	"sort"
	"sync"
	"sync/atomic"
	"text/tabwriter"
	"time"
)

// Index provides overview of all segments in a repository.
type Index struct {
	mu       sync.RWMutex
	segments map[string][]SegmentInfo

	totalSize int64
}

// NewIndex returns a new index.
func NewIndex() *Index {
	return &Index{
		segments: make(map[string][]SegmentInfo),
	}
}

// Add adds a segment to the index.
func (i *Index) Add(s SegmentInfo) {
	i.mu.Lock()
	defer i.mu.Unlock()

	atomic.AddInt64(&i.totalSize, s.Size)
	i.segments[s.Prefix] = append(i.segments[s.Prefix], s)
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

// OldestSegmentAge returns the age of the oldest segment in the index.
func (i *Index) OldestSegmentAge() time.Duration {
	i.mu.RLock()
	defer i.mu.RUnlock()

	var dur time.Duration
	for _, segments := range i.segments {
		for _, seg := range segments {
			age := time.Since(seg.CreatedAt)
			if dur.Seconds() == 0 || age > dur {
				dur = age
			}
		}
	}

	if dur == 0 {
		return 0
	}
	return dur
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
