package pool

import (
	"runtime"
	"sync"
	"testing"
	"weak"

	"github.com/stretchr/testify/require"
)

type pooledItem struct {
	values []string
}

func (i *pooledItem) Reset() {
	clear(i.values)
	i.values = i.values[:0]
}

func TestResettingPool(t *testing.T) {
	p := NewResettingPool(func() *pooledItem {
		return &pooledItem{values: make([]string, 0, 4)}
	})
	item := p.Get()
	require.Empty(t, item.values)
	require.Equal(t, 4, cap(item.values))
	item.values = append(item.values, "payload")
	backing := item.values
	p.Put(item)

	// Verify reset on return without assuming sync.Pool will reuse this item.
	require.Equal(t, "", backing[0])
	item = p.Get()
	require.Empty(t, item.values)
	require.Equal(t, 4, cap(item.values))
	p.Put(item)
}

func TestResettingPoolConcurrent(t *testing.T) {
	p := NewResettingPool(func() *pooledItem { return &pooledItem{} })
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 1000 {
				item := p.Get()
				if len(item.values) != 0 {
					t.Error("pooled item was not reset")
					return
				}
				item.values = append(item.values, "payload")
				p.Put(item)
			}
		})
	}
	wg.Wait()
}

func TestResettingPoolReleasesIdleItems(t *testing.T) {
	p := NewResettingPool(func() *pooledItem {
		return &pooledItem{values: make([]string, 0, 4)}
	})
	ref := func() weak.Pointer[pooledItem] {
		item := p.Get()
		ref := weak.Make(item)
		p.Put(item)
		return ref
	}()

	// sync.Pool keeps a victim cache for one GC cycle. A second collection
	// releases idle items even while the pool itself remains reachable.
	runtime.GC()
	runtime.GC()
	require.Nil(t, ref.Value())

	item := p.Get()
	require.NotNil(t, item)
	require.Equal(t, 4, cap(item.values))
	p.Put(item)
	runtime.KeepAlive(p)
}
