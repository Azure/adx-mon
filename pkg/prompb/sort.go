package prompb

import (
	"bytes"
	"sort"
)

type Labels []Label

func (l Labels) Len() int { return len(l) }

func (l Labels) Less(i, j int) bool {
	return labelLess(l[i].Name, l[j].Name)
}

func (l Labels) Swap(i, j int) {
	l[i], l[j] = l[j], l[i]
}

// IsSorted return true if the labels are sorted according to Sort.
func IsSorted(l []*Label) bool {
	if len(l) == 1 {
		return true
	}
	for i := 1; i < len(l)-1; i++ {
		if !labelLess(l[i-1].Name, l[i].Name) {
			return false
		}
	}
	return true
}

// Sort sorts labels ensuring the __name__ is first the remaining labels or ordered by name.
func Sort(l []*Label) {
	sort.Sort(labelSorter(l))
}

type labelSorter []*Label

func (ls labelSorter) Len() int           { return len(ls) }
func (ls labelSorter) Less(i, j int) bool { return labelLess(ls[i].Name, ls[j].Name) }
func (ls labelSorter) Swap(i, j int)      { ls[i], ls[j] = ls[j], ls[i] }

func labelLess(a, b []byte) bool {
	if bytes.Equal(a, nameBytes) {
		return true
	} else if bytes.Equal(b, nameBytes) {
		return false
	}
	return CompareLower(a, b) < 0
}

func CompareLower(sa, sb []byte) int {
	for len(sa) > 0 && len(sb) > 0 {
		ra := sa[0]
		rb := sb[0]

		if ra >= 'A' && ra <= 'Z' {
			ra += 'a' - 'A'
		}
		if rb >= 'A' && rb <= 'Z' {
			rb += 'a' - 'A'
		}

		if ra != rb {
			if ra < rb {
				return -1
			}
			return 1
		}

		sa = sa[1:]
		sb = sb[1:]
	}

	if len(sa) == len(sb) {
		return 0
	}
	if len(sa) < len(sb) {
		return -1
	}
	return 1
}
