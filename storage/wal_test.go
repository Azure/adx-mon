package storage_test

import (
	"bytes"
	"context"
	"github.com/Azure/adx-mon/prompb"
	"github.com/Azure/adx-mon/storage"
	"github.com/stretchr/testify/require"
	"sort"
	"testing"
)

func TestNewWAL(t *testing.T) {
	w, err := storage.NewWAL(storage.WALOpts{
		StorageDir: t.TempDir(),
	})
	require.NoError(t, err)
	require.NoError(t, w.Open())

	w.Write(context.Background(), []prompb.TimeSeries{newTimeSeries("foo", nil, 1, 1)})
	w.Write(context.Background(), []prompb.TimeSeries{newTimeSeries("foo", nil, 1, 1)})
	w.Write(context.Background(), []prompb.TimeSeries{newTimeSeries("foo", nil, 1, 1)})
	require.Equal(t, 1, w.Size())
}

func TestWAL_Segment(t *testing.T) {
	w, err := storage.NewWAL(storage.WALOpts{
		StorageDir: t.TempDir(),
	})
	require.NoError(t, err)
	require.NoError(t, w.Open())

	series := newTimeSeries("foo", nil, 1, 1)
	w.Write(context.Background(), []prompb.TimeSeries{series})
	w.Write(context.Background(), []prompb.TimeSeries{newTimeSeries("foo", nil, 2, 2)})

	require.Equal(t, 1, w.Size())

	seg := w.Segment()
	require.NotNil(t, seg)

	b, err := seg.Bytes()
	require.NoError(t, err)
	require.Equal(t, `1970-01-01T00:00:00.001Z,3320404949056263377,{},1.000000000
1970-01-01T00:00:00.002Z,3320404949056263377,{},2.000000000
`, string(b))

}

func TestWAL_OpenSegments(t *testing.T) {
	dir := t.TempDir()
	w, err := storage.NewWAL(storage.WALOpts{
		Prefix:     "Foo",
		StorageDir: dir,
	})
	require.NoError(t, err)
	require.NoError(t, w.Open())
	w.Write(context.Background(), []prompb.TimeSeries{newTimeSeries("foo", nil, 1, 1)})
	require.Equal(t, 1, w.Size())

	require.NoError(t, w.Close())

	w, err = storage.NewWAL(storage.WALOpts{
		Prefix:     "Foo",
		StorageDir: dir,
	})
	require.NoError(t, err)
	require.NoError(t, w.Open())
	require.Equal(t, 0, w.Size())

	w.Write(context.Background(), []prompb.TimeSeries{newTimeSeries("foo", nil, 2, 2)})
	require.Equal(t, 1, w.Size())

}

func newTimeSeries(name string, labels map[string]string, ts int64, val float64) prompb.TimeSeries {
	l := []prompb.Label{
		{
			Name:  []byte("__name__"),
			Value: []byte(name),
		},
	}
	for k, v := range labels {
		l = append(l, prompb.Label{Name: []byte(k), Value: []byte(v)})
	}
	sort.Slice(l, func(i, j int) bool {
		return bytes.Compare(l[i].Name, l[j].Name) < 0
	})

	return prompb.TimeSeries{
		Labels: l,

		Samples: []prompb.Sample{
			{
				Timestamp: ts,
				Value:     val,
			},
		},
	}
}
