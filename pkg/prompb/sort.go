package prompb

import (
	"bytes"
	"sort"
)

// IsSorted return true if the labels are sorted according to Sort.
func IsSorted(l []Label) bool {
	return sort.SliceIsSorted(l, func(i, j int) bool {
		return labelCompare(l[i].Name, l[j].Name)
	})
}

// Sort sorts labels ensuring the __name__ is first the remaining labels or ordered by name.
func Sort(l []Label) {
	sort.Slice(l, func(i, j int) bool {
		return labelCompare(l[i].Name, l[j].Name)
	})
}

func labelCompare(a, b []byte) bool {
	if bytes.Equal(a, []byte("__name__")) {
		return true
	} else if bytes.Equal(b, []byte("__name__")) {
		return false
	}
	return bytes.Compare(bytes.ToLower(a), bytes.ToLower(b)) < 0
}
