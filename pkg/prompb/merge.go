package prompb

import (
	"bytes"
	"iter"
)

// MergedLabels returns an iterator over the sorted merge of seriesLabels and
// commonLabels without materializing the merged slice. If both inputs contain
// the same label name, the per-series label takes precedence.
//
// Both input slices must be sorted according to Sort and must not be modified
// while the iterator is running.
func MergedLabels(seriesLabels, commonLabels []*Label) iter.Seq[*Label] {
	return func(yield func(*Label) bool) {
		series, common := seriesLabels, commonLabels
		for len(series) > 0 && len(common) > 0 {
			if bytes.Equal(series[0].Name, common[0].Name) {
				if !yield(series[0]) {
					return
				}
				series = series[1:]
				common = common[1:]
				continue
			}

			if labelLess(common[0].Name, series[0].Name) {
				if !yield(common[0]) {
					return
				}
				common = common[1:]
				continue
			}

			if !yield(series[0]) {
				return
			}
			series = series[1:]
		}

		for _, label := range series {
			if !yield(label) {
				return
			}
		}
		for _, label := range common {
			if !yield(label) {
				return
			}
		}
	}
}
