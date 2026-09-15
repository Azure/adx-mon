package prompb

import (
	"bytes"
	"iter"
	"math/bits"
	"regexp"
)

// LabelDropRule drops matching label names from matching metrics.
type LabelDropRule struct {
	Metric *regexp.Regexp
	Label  *regexp.Regexp
}

// LabelFilter contains immutable request-level label filtering rules.
type LabelFilter struct {
	Drop []LabelDropRule
	Keep [][]byte
}

// Drops reports whether name is excluded for metricName. The metric name
// label is always retained, and explicit keep entries take precedence.
func (f *LabelFilter) Drops(metricName, name []byte) bool {
	dropMask, useMask := f.matchingDropRules(metricName)
	return f.dropsWithMask(metricName, name, dropMask, useMask)
}

func (f *LabelFilter) matchingDropRules(metricName []byte) (uint64, bool) {
	if f == nil {
		return 0, true
	}
	if len(f.Drop) > 64 {
		return 0, false
	}

	var matches uint64
	for i, rule := range f.Drop {
		if rule.Metric.Match(metricName) {
			matches |= uint64(1) << i
		}
	}
	return matches, true
}

func (f *LabelFilter) dropsWithMask(metricName, name []byte, dropMask uint64, useMask bool) bool {
	if f == nil || bytes.Equal(name, nameBytes) {
		return false
	}
	for _, keep := range f.Keep {
		if bytes.Equal(keep, name) {
			return false
		}
	}
	if useMask {
		for dropMask != 0 {
			i := bits.TrailingZeros64(dropMask)
			if f.Drop[i].Label.Match(name) {
				return true
			}
			dropMask &= dropMask - 1
		}
		return false
	}

	for _, rule := range f.Drop {
		if rule.Label.Match(name) && rule.Metric.Match(metricName) {
			return true
		}
	}
	return false
}

// MergedLabels returns an iterator over the sorted merge of seriesLabels and
// commonLabels without materializing the merged slice. If both inputs contain
// the same label name, the per-series label takes precedence.
//
// Both input slices must be sorted according to Sort and must not be modified
// while the iterator is running.
func MergedLabels(seriesLabels, commonLabels []*Label) iter.Seq[*Label] {
	return mergedLabelsWithFilter(seriesLabels, commonLabels, nil, nil)
}

// Labels returns the effective sorted labels for series, including common
// labels and request-level filtering. Per-series labels take precedence when
// both inputs contain the same name.
func (wr *WriteRequest) Labels(series *TimeSeries) iter.Seq[*Label] {
	return mergedLabelsWithFilter(series.Labels, wr.CommonLabels, MetricName(series), wr.LabelFilter)
}

func mergedLabelsWithFilter(seriesLabels, commonLabels []*Label, metricName []byte, filter *LabelFilter) iter.Seq[*Label] {
	return func(yield func(*Label) bool) {
		dropMask, useMask := filter.matchingDropRules(metricName)
		series, common := seriesLabels, commonLabels
		for len(series) > 0 && len(common) > 0 {
			if bytes.Equal(series[0].Name, common[0].Name) {
				label := series[0]
				series = series[1:]
				common = common[1:]
				if filter.dropsWithMask(metricName, label.Name, dropMask, useMask) {
					continue
				}
				if !yield(label) {
					return
				}
				continue
			}

			if labelLess(common[0].Name, series[0].Name) {
				label := common[0]
				common = common[1:]
				if filter.dropsWithMask(metricName, label.Name, dropMask, useMask) {
					continue
				}
				if !yield(label) {
					return
				}
				continue
			}

			label := series[0]
			series = series[1:]
			if filter.dropsWithMask(metricName, label.Name, dropMask, useMask) {
				continue
			}
			if !yield(label) {
				return
			}
		}

		for _, label := range series {
			if filter.dropsWithMask(metricName, label.Name, dropMask, useMask) {
				continue
			}
			if !yield(label) {
				return
			}
		}
		for _, label := range common {
			if filter.dropsWithMask(metricName, label.Name, dropMask, useMask) {
				continue
			}
			if !yield(label) {
				return
			}
		}
	}
}
