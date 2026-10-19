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
	Labels      []*prompb.Label
	value       float64
	cardinality int
	n           int
}

func (m *Metric) Next(timestamp time.Time) *prompb.TimeSeries {
	// Randomize the cardinality index so that we get more distinct sets of active series sent concurrently.
	if m.n == 0 {
		m.n = rand.Intn(m.cardinality)
	}
	ts := &prompb.TimeSeries{
		Labels: m.Labels,
		Samples: []*prompb.Sample{{
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

func (s *Set) Next(timestamp time.Time) *prompb.TimeSeries {
	metric := s.metrics[s.i%len(s.metrics)]
	ts := metric.Next(timestamp)
	ts.Labels = append(ts.Labels, &prompb.Label{
		Name:  []byte("adxmon_database"),
		Value: []byte(s.opts.Database),
	})
	ts.Labels = append(ts.Labels, &prompb.Label{
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
			{Labels: []*prompb.Label{
				{Name: []byte("__name__"), Value: []byte("cpu")},
				{Name: []byte("region"), Value: []byte("eastus")},
			},
				cardinality: opts.Cardinality,
			},
			{Labels: []*prompb.Label{
				{Name: []byte("__name__"), Value: []byte("mem")},
				{Name: []byte("region"), Value: []byte("eastus")},
			},
				cardinality: opts.Cardinality,
			},
			{Labels: []*prompb.Label{
				{Name: []byte("__name__"), Value: []byte("mem_total")},
				{Name: []byte("region"), Value: []byte("eastus")},
			},
				cardinality: opts.Cardinality,
			},
			{Labels: []*prompb.Label{
				{Name: []byte("__name__"), Value: []byte("mem_free")},
				{Name: []byte("region"), Value: []byte("eastus")},
			},
				cardinality: opts.Cardinality,
			},

			{Labels: []*prompb.Label{
				{Name: []byte("__name__"), Value: []byte("mem_cached")},
				{Name: []byte("region"), Value: []byte("eastus")},
			},
				cardinality: opts.Cardinality,
			},

			{Labels: []*prompb.Label{
				{Name: []byte("__name__"), Value: []byte("mem_buffers")},
				{Name: []byte("region"), Value: []byte("eastus")},
			},
				cardinality: opts.Cardinality,
			},

			{Labels: []*prompb.Label{
				{Name: []byte("__name__"), Value: []byte("disk")},
				{Name: []byte("region"), Value: []byte("eastus")},
			},
				cardinality: opts.Cardinality,
			},

			{Labels: []*prompb.Label{
				{Name: []byte("__name__"), Value: []byte("disk_total")},
				{Name: []byte("region"), Value: []byte("eastus")},
			},
				cardinality: opts.Cardinality,
			},

			{Labels: []*prompb.Label{
				{Name: []byte("__name__"), Value: []byte("disk_used")},
				{Name: []byte("region"), Value: []byte("eastus")},
			},
				cardinality: opts.Cardinality,
			},

			{Labels: []*prompb.Label{
				{Name: []byte("__name__"), Value: []byte("disk_free")},
				{Name: []byte("region"), Value: []byte("eastus")},
			},
				cardinality: opts.Cardinality,
			},

			{Labels: []*prompb.Label{
				{Name: []byte("__name__"), Value: []byte("net")},
				{Name: []byte("region"), Value: []byte("eastus")},
			},
				cardinality: opts.Cardinality,
			},

			{Labels: []*prompb.Label{
				{Name: []byte("__name__"), Value: []byte("netio")},
				{Name: []byte("region"), Value: []byte("eastus")},
			},
				cardinality: opts.Cardinality,
			},

			{Labels: []*prompb.Label{
				{Name: []byte("__name__"), Value: []byte("diskio")},
				{Name: []byte("region"), Value: []byte("eastus")},
			},
				cardinality: opts.Cardinality,
			},

			{Labels: []*prompb.Label{
				{Name: []byte("__name__"), Value: []byte("cpu_util")},
				{Name: []byte("region"), Value: []byte("eastus")},
			},
				cardinality: opts.Cardinality,
			},

			{Labels: []*prompb.Label{
				{Name: []byte("__name__"), Value: []byte("cpu_load1")},
				{Name: []byte("region"), Value: []byte("eastus")},
			},
				cardinality: opts.Cardinality,
			},

			{Labels: []*prompb.Label{
				{Name: []byte("__name__"), Value: []byte("cpu_load5")},
				{Name: []byte("region"), Value: []byte("eastus")},
			},
				cardinality: opts.Cardinality,
			},

			{Labels: []*prompb.Label{
				{Name: []byte("__name__"), Value: []byte("cpu_load15")},
				{Name: []byte("region"), Value: []byte("eastus")},
			},
				cardinality: opts.Cardinality,
			},
		},
	}

	for i := 0; i < 1000; i++ {
		s.metrics = append(s.metrics,
			&Metric{Labels: []*prompb.Label{
				{Name: []byte("__name__"), Value: []byte(fmt.Sprintf("metric%d", i))},
				{Name: []byte("region"), Value: []byte("eastus")},
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
