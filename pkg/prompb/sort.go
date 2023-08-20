package prompb

import (
	"bytes"
	"sort"
	"unicode"
	"unicode/utf8"
)

// IsSorted return true if the labels are sorted according to Sort.
func IsSorted(l []Label) bool {
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
func Sort(l []Label) {
	sort.Slice(l, func(i, j int) bool {
		return labelLess(l[i].Name, l[j].Name)
	})
}

func labelLess(a, b []byte) bool {
	if bytes.Equal(a, []byte("__name__")) {
		return true
	} else if bytes.Equal(b, []byte("__name__")) {
		return false
	}
	return compareLower(a, b) < 0
}

func compareLower(sa, sb []byte) int {
	for {
		rb, nb := utf8.DecodeRune(sb)
		ra, na := utf8.DecodeRune(sa)

		if na == 0 && nb > 0 {
			return -1
		} else if na > 0 && nb == 0 {
			return 1
		} else if na == 0 && nb == 0 {
			return 0
		}

		rb = unicode.ToLower(rb)
		ra = unicode.ToLower(ra)

		if ra < rb {
			return -1
		} else if ra > rb {
			return 1
		}

		// Trim rune from the beginning of each string.
		sa = sa[na:]
		sb = sb[nb:]
	}
}
