package prompb

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSortedLables(t *testing.T) {
	l := []Label{
		{
			Name:  []byte("foo"),
			Value: []byte("bar"),
		},
		{
			Name:  []byte("__name__"),
			Value: []byte("name"),
		},
		{
			Name:  []byte("__abc__"),
			Value: []byte("abc"),
		},

		{
			Name:  []byte("Capital"),
			Value: []byte("capital"),
		},
	}

	require.False(t, IsSorted(l))
	Sort(l)
	require.True(t, IsSorted(l))
	require.Equal(t, []byte("__name__"), l[0].Name)
	require.Equal(t, []byte("__abc__"), l[1].Name)
	require.Equal(t, []byte("Capital"), l[2].Name)
	require.Equal(t, []byte("foo"), l[3].Name)
}

func BenchmarkIsSorted(b *testing.B) {
	l := []Label{
		{
			Name:  []byte("foo"),
			Value: []byte("bar"),
		},
		{
			Name:  []byte("__name__"),
			Value: []byte("name"),
		},
		{
			Name:  []byte("__abc__"),
			Value: []byte("abc"),
		},

		{
			Name:  []byte("Capital"),
			Value: []byte("capital"),
		},
	}

	for i := 0; i < b.N; i++ {
		IsSorted(l)
	}
}
