package partmap_test

import (
	"testing"

	"github.com/Azure/adx-mon/pkg/partmap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMap_SetGet(t *testing.T) {
	m := partmap.NewMap[int](64)

	m.Set("key1", 100)
	m.Set("key2", 200)

	value, ok := m.Get("key1")
	require.True(t, ok)
	require.Equal(t, 100, value)

	value, ok = m.Get("key2")
	require.True(t, ok)
	require.Equal(t, 200, value)

	value, ok = m.Get("key3")
	require.False(t, ok)
}

func TestMap_Delete(t *testing.T) {
	m := partmap.NewMap[int](64)

	m.Set("key1", 100)
	m.Set("key2", 200)

	value, ok := m.Delete("key1")
	require.True(t, ok)
	require.Equal(t, 100, value)

	value, ok = m.Get("key1")
	require.False(t, ok)

	value, ok = m.Get("key2")
	require.True(t, ok)
	require.Equal(t, 200, value)
}

func TestMap_GetOrCreate(t *testing.T) {
	m := partmap.NewMap[int](64)

	value, err := m.GetOrCreate("key1", func() (int, error) {
		return 100, nil
	})
	require.NoError(t, err)
	require.Equal(t, 100, value)

	value, ok := m.Get("key1")
	require.True(t, ok)
	require.Equal(t, 100, value)
}

func TestMap_Each(t *testing.T) {
	m := partmap.NewMap[int](64)

	m.Set("key1", 100)
	m.Set("key2", 200)

	keys := make(map[string]int)
	err := m.Each(func(key string, value int) error {
		keys[key] = value
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 2, len(keys))
	require.Equal(t, 100, keys["key1"])
	require.Equal(t, 200, keys["key2"])
}

func TestMap_Count(t *testing.T) {
	m := partmap.NewMap[int](64)

	require.Equal(t, 0, m.Count())

	m.Set("key1", 100)
	m.Set("key2", 200)

	require.Equal(t, 2, m.Count())
}

func TestMap_Mutate(t *testing.T) {
	m := partmap.NewMap[int](64)

	t.Run("key does not exist", func(t *testing.T) {
		err := m.Mutate("key1", func(value int) (int, error) {
			return value + 100, nil
		})
		require.NoError(t, err)
		value, ok := m.Get("key1")
		require.True(t, ok)
		require.Equal(t, 100, value)
	})

	t.Run("key exists", func(t *testing.T) {
		m.Set("key2", 200)

		err := m.Mutate("key2", func(value int) (int, error) {
			return value + 100, nil
		})
		require.NoError(t, err)

		value, ok := m.Get("key2")
		require.True(t, ok)
		require.Equal(t, 300, value)
	})

	t.Run("error", func(t *testing.T) {
		err := m.Mutate("key3", func(value int) (int, error) {
			return 0, assert.AnError
		})
		require.Error(t, err)
		value, ok := m.Get("key3")
		require.False(t, ok)
		require.Equal(t, 0, value)

	})

	t.Run("nil fn", func(t *testing.T) {
		err := m.Mutate("key3", nil)
		require.Error(t, err)
	})
}
