package prompb

import (
	"hash"
	"iter"
	"testing"

	"github.com/cespare/xxhash"
	"github.com/stretchr/testify/require"
)

func TestMergedLabels(t *testing.T) {
	seriesLabels := []*Label{
		newMergeTestLabel("__name__", "requests_total"),
		newMergeTestLabel("cluster", "cluster-1"),
		newMergeTestLabel("method", "GET"),
		newMergeTestLabel("status", "200"),
	}
	commonLabels := []*Label{
		newMergeTestLabel("adxmon_database", "Metrics"),
		newMergeTestLabel("Cloud", "Public"),
		newMergeTestLabel("Environment", "prod"),
		newMergeTestLabel("Host", "collector-1"),
		newMergeTestLabel("RPTenant", "tenant-1"),
	}

	var actual []string
	for label := range MergedLabels(seriesLabels, commonLabels) {
		actual = append(actual, string(label.Name))
	}

	require.Equal(t, []string{
		"__name__",
		"adxmon_database",
		"Cloud",
		"cluster",
		"Environment",
		"Host",
		"method",
		"RPTenant",
		"status",
	}, actual)
}

func TestMergedLabelsSeriesLabelTakesPrecedence(t *testing.T) {
	seriesLabel := newMergeTestLabel("cluster", "series-value")
	commonLabel := newMergeTestLabel("cluster", "common-value")

	var actual []*Label
	for label := range MergedLabels([]*Label{seriesLabel}, []*Label{commonLabel}) {
		actual = append(actual, label)
	}

	require.Equal(t, []*Label{seriesLabel}, actual)
}

func TestMergedLabelsEmptyInputs(t *testing.T) {
	label := newMergeTestLabel("cluster", "cluster-1")

	tests := []struct {
		name   string
		series []*Label
		common []*Label
	}{
		{name: "both empty"},
		{name: "series empty", common: []*Label{label}},
		{name: "common empty", series: []*Label{label}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var actual []*Label
			for label := range MergedLabels(tt.series, tt.common) {
				actual = append(actual, label)
			}

			require.Len(t, actual, len(tt.series)+len(tt.common))
			if len(actual) > 0 {
				require.Same(t, label, actual[0])
			}
		})
	}
}

func TestMergedLabelsCanStopAndRestart(t *testing.T) {
	labels := MergedLabels(
		[]*Label{newMergeTestLabel("a", "1")},
		[]*Label{newMergeTestLabel("b", "2")},
	)

	var first []string
	for label := range labels {
		first = append(first, string(label.Name))
		break
	}
	var second []string
	for label := range labels {
		second = append(second, string(label.Name))
	}

	require.Equal(t, []string{"a"}, first)
	require.Equal(t, []string{"a", "b"}, second)
}

func TestMergedLabelsDoesNotMutateInputs(t *testing.T) {
	seriesLabels := []*Label{newMergeTestLabel("a", "1")}
	commonLabels := []*Label{newMergeTestLabel("b", "2")}

	for range MergedLabels(seriesLabels, commonLabels) {
	}

	require.Equal(t, "a", string(seriesLabels[0].Name))
	require.Equal(t, "b", string(commonLabels[0].Name))
}

func BenchmarkMergedLabels(b *testing.B) {
	seriesLabels := []*Label{
		newMergeTestLabel("__name__", "http_requests_total"),
		newMergeTestLabel("agentpool", "systempool"),
		newMergeTestLabel("cluster", "cluster-1"),
		newMergeTestLabel("code", "200"),
		newMergeTestLabel("container", "api"),
		newMergeTestLabel("instance", "10.0.0.1:8080"),
		newMergeTestLabel("job", "apiserver"),
		newMergeTestLabel("method", "GET"),
		newMergeTestLabel("namespace", "default"),
		newMergeTestLabel("pod", "api-12345"),
		newMergeTestLabel("region", "eastus2"),
		newMergeTestLabel("status", "success"),
	}
	commonLabels := []*Label{
		newMergeTestLabel("adxmon_container", "collector"),
		newMergeTestLabel("adxmon_database", "AKSCCPMetrics"),
		newMergeTestLabel("adxmon_namespace", "default"),
		newMergeTestLabel("adxmon_pod", "collector-12345"),
		newMergeTestLabel("Cloud", "Public"),
		newMergeTestLabel("Environment", "prod"),
		newMergeTestLabel("Host", "collector-12345"),
		newMergeTestLabel("RPTenant", "tenant-1"),
		newMergeTestLabel("UnderlayName", "underlay-1"),
	}

	materializedLabels := append([]*Label(nil), seriesLabels...)
	materializedLabels = append(materializedLabels, commonLabels...)
	Sort(materializedLabels)

	b.Run("materialized", func(b *testing.B) {
		benchmarkHashLabels(b, func(hasher hash.Hash64) {
			for _, label := range materializedLabels {
				_, _ = hasher.Write(label.Name)
				_, _ = hasher.Write(label.Value)
			}
		})
	})

	b.Run("merged", func(b *testing.B) {
		benchmarkHashLabels(b, func(hasher hash.Hash64) {
			for label := range MergedLabels(seriesLabels, commonLabels) {
				_, _ = hasher.Write(label.Name)
				_, _ = hasher.Write(label.Value)
			}
		})
	})

	b.Run("pull-iterator", func(b *testing.B) {
		benchmarkHashLabels(b, func(hasher hash.Hash64) {
			next, stop := iter.Pull(MergedLabels(seriesLabels, commonLabels))
			for {
				label, ok := next()
				if !ok {
					break
				}
				_, _ = hasher.Write(label.Name)
				_, _ = hasher.Write(label.Value)
			}
			stop()
		})
	})

	b.Run("materialize-and-sort", func(b *testing.B) {
		benchmarkHashLabels(b, func(hasher hash.Hash64) {
			labels := make([]*Label, 0, len(seriesLabels)+len(commonLabels))
			labels = append(labels, seriesLabels...)
			labels = append(labels, commonLabels...)
			Sort(labels)
			for _, label := range labels {
				_, _ = hasher.Write(label.Name)
				_, _ = hasher.Write(label.Value)
			}
		})
	})
}

func benchmarkHashLabels(b *testing.B, writeLabels func(hash.Hash64)) {
	b.Helper()
	b.ReportAllocs()
	hasher := xxhash.New()

	b.ResetTimer()
	for range b.N {
		hasher.Reset()
		writeLabels(hasher)
		mergedLabelsHash = hasher.Sum64()
	}
}

func newMergeTestLabel(name, value string) *Label {
	return &Label{Name: []byte(name), Value: []byte(value)}
}

var mergedLabelsHash uint64
