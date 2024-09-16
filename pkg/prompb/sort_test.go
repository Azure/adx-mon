package prompb

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSortedLabels(t *testing.T) {
	l := []*Label{
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

func TestCompareLower(t *testing.T) {
	for _, tc := range []struct {
		a, b     []byte
		expected int
	}{
		{[]byte("a"), []byte("b"), -1},
		{[]byte("b"), []byte("a"), 1},
		{[]byte("a"), []byte("a"), 0},
		{[]byte("A"), []byte("a"), 0},
		{[]byte("a"), []byte("A"), 0},
		{[]byte("A"), []byte("A"), 0},
		{[]byte("a"), []byte("aa"), -1},
		{[]byte("aa"), []byte("a"), 1},
		{[]byte("aa"), []byte("aa"), 0},
		{[]byte("aa"), []byte("ab"), -1},
		{[]byte("ab"), []byte("aa"), 1},
		{[]byte("ab"), []byte("ab"), 0},
	} {
		require.Equal(t, tc.expected, CompareLower(tc.a, tc.b), "%s %s", tc.a, tc.b)
	}
}

func BenchmarkIsSorted(b *testing.B) {
	l := []*Label{
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
