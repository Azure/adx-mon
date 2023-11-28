package data

import (
	"fmt"
	"math/rand"
	"sort"
	"time"

	"github.com/Azure/adx-mon/pkg/prompb"
)

type SetOptions struct {
	NumMetrics  int
	Cardinality int
	Database    string
}

type Metric struct {
	Labels      []prompb.Label
	value       float64
	cardinality int
	n           int
}

func (m *Metric) Next(timestamp time.Time) prompb.TimeSeries {
	// Randomize the cardinality index so that we get more distinct sets of active series sent concurrently.
	if m.n == 0 {
		m.n = rand.Intn(m.cardinality)
	}
	ts := prompb.TimeSeries{
		Labels: m.Labels,
		Samples: []prompb.Sample{{
			Value:     m.value,
			Timestamp: timestamp.UnixNano() / 1e6,
		}},
	}
	m.value += 1
	m.n += 1
	return ts
}

type Set struct {
	metrics []*Metric
	i       int

	opts SetOptions
}

func (s *Set) Next(timestamp time.Time) prompb.TimeSeries {
	metric := s.metrics[s.i%len(s.metrics)]
	ts := metric.Next(timestamp)
	ts.Labels = append(ts.Labels, prompb.Label{
		Name:  []byte("adxmon_database"),
		Value: []byte(s.opts.Database),
	})
	ts.Labels = append(ts.Labels, prompb.Label{
		Name:  []byte("host"),
		Value: []byte(fmt.Sprintf("host_%d", metric.n%metric.cardinality)),
	})
	sort.Slice(ts.Labels, func(i, j int) bool {
		return string(ts.Labels[i].Name) < string(ts.Labels[j].Name)
	})
	s.i += 1
	return ts
}

func NewDataSet(opts SetOptions) *Set {
	s := &Set{
		opts: opts,
		metrics: []*Metric{
			{Labels: []prompb.Label{
				{[]byte("__name__"), []byte("cpu")},
				{[]byte("region"), []byte("eastus")},
			},
				cardinality: opts.Cardinality,
			},
			{Labels: []prompb.Label{
				{[]byte("__name__"), []byte("mem")},
				{[]byte("region"), []byte("eastus")},
			},
				cardinality: opts.Cardinality,
			},
			{Labels: []prompb.Label{
				{[]byte("__name__"), []byte("mem_total")},
				{[]byte("region"), []byte("eastus")},
			},
				cardinality: opts.Cardinality,
			},
			{Labels: []prompb.Label{
				{[]byte("__name__"), []byte("mem_free")},
				{[]byte("region"), []byte("eastus")},
			},
				cardinality: opts.Cardinality,
			},

			{Labels: []prompb.Label{
				{[]byte("__name__"), []byte("mem_cached")},
				{[]byte("region"), []byte("eastus")},
			},
				cardinality: opts.Cardinality,
			},

			{Labels: []prompb.Label{
				{[]byte("__name__"), []byte("mem_buffers")},
				{[]byte("region"), []byte("eastus")},
			},
				cardinality: opts.Cardinality,
			},

			{Labels: []prompb.Label{
				{[]byte("__name__"), []byte("disk")},
				{[]byte("region"), []byte("eastus")},
			},
				cardinality: opts.Cardinality,
			},

			{Labels: []prompb.Label{
				{[]byte("__name__"), []byte("disk_total")},
				{[]byte("region"), []byte("eastus")},
			},
				cardinality: opts.Cardinality,
			},

			{Labels: []prompb.Label{
				{[]byte("__name__"), []byte("disk_used")},
				{[]byte("region"), []byte("eastus")},
			},
				cardinality: opts.Cardinality,
			},

			{Labels: []prompb.Label{
				{[]byte("__name__"), []byte("disk_free")},
				{[]byte("region"), []byte("eastus")},
			},
				cardinality: opts.Cardinality,
			},

			{Labels: []prompb.Label{
				{[]byte("__name__"), []byte("net")},
				{[]byte("region"), []byte("eastus")},
			},
				cardinality: opts.Cardinality,
			},

			{Labels: []prompb.Label{
				{[]byte("__name__"), []byte("netio")},
				{[]byte("region"), []byte("eastus")},
			},
				cardinality: opts.Cardinality,
			},

			{Labels: []prompb.Label{
				{[]byte("__name__"), []byte("diskio")},
				{[]byte("region"), []byte("eastus")},
			},
				cardinality: opts.Cardinality,
			},

			{Labels: []prompb.Label{
				{[]byte("__name__"), []byte("cpu_util")},
				{[]byte("region"), []byte("eastus")},
			},
				cardinality: opts.Cardinality,
			},

			{Labels: []prompb.Label{
				{[]byte("__name__"), []byte("cpu_load1")},
				{[]byte("region"), []byte("eastus")},
			},
				cardinality: opts.Cardinality,
			},

			{Labels: []prompb.Label{
				{[]byte("__name__"), []byte("cpu_load5")},
				{[]byte("region"), []byte("eastus")},
			},
				cardinality: opts.Cardinality,
			},

			{Labels: []prompb.Label{
				{[]byte("__name__"), []byte("cpu_load15")},
				{[]byte("region"), []byte("eastus")},
			},
				cardinality: opts.Cardinality,
			},
		},
	}

	for i := 0; i < 1000; i++ {
		s.metrics = append(s.metrics,
			&Metric{Labels: []prompb.Label{
				{[]byte("__name__"), []byte(fmt.Sprintf("metric%d", i))},
				{[]byte("region"), []byte("eastus")},
			},
				cardinality: opts.Cardinality,
			})
	}
	if opts.NumMetrics > len(s.metrics) {
		return s
	}
	s.metrics = s.metrics[:opts.NumMetrics]
	return s
}
