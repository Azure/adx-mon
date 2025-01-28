package partmap

import (
	"fmt"
	"sync"

	"github.com/cespare/xxhash"
)

// Map is a concurrent map with string keys and generic values, partitioned for better concurrency.
type Map[V any] struct {
	partitions []*syncMap[V]
}

// syncMap is a thread-safe map with string keys and generic values.
type syncMap[V any] struct {
	mu sync.RWMutex
	m  map[string]V
}

// apply applies a function to each key-value pair in the map.
func (s *syncMap[V]) apply(fn func(key string, value V) error) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for key, value := range s.m {
		if err := fn(key, value); err != nil {
			return err
		}
	}
	return nil
}

// get retrieves the value for a given key.
func (s *syncMap[V]) get(key string) (V, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	v, ok := s.m[key]
	return v, ok
}

// set sets the value for a given key.
func (s *syncMap[V]) set(key string, value V) V {
	s.mu.Lock()
	defer s.mu.Unlock()

	v, ok := s.m[key]
	if ok {
		return v
	}
	s.m[key] = value
	return value
}

// delete removes the value for a given key.
func (s *syncMap[V]) delete(key string) (V, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	v, ok := s.m[key]
	if !ok {
		var zero V
		return zero, false
	}

	delete(s.m, key)
	return v, true
}

// getOrCreate retrieves the value for a given key or creates it using the provided function.
func (s *syncMap[V]) getOrCreate(key string, fn func() (V, error)) (V, error) {
	s.mu.RLock()
	v, ok := s.m[key]
	s.mu.RUnlock()
	if ok {
		return v, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	v, ok = s.m[key]
	if ok {
		return v, nil
	}

	var err error
	v, err = fn()
	if err != nil {
		var zero V
		return zero, err
	}
	s.m[key] = v
	return v, nil
}

func (s *syncMap[V]) mutate(key string, fn func(value V) (V, error)) error {
	if fn == nil {
		return fmt.Errorf("fn is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	// Use the zero value of V if the key does not exist.
	v, _ := s.m[key]

	newValue, err := fn(v)
	if err != nil {
		return err
	}
	s.m[key] = newValue
	return nil
}

// NewMap creates a new Map with the specified number of partitions.
func NewMap[V any](partitions int) *Map[V] {
	parts := make([]*syncMap[V], partitions)
	for i := range parts {
		parts[i] = &syncMap[V]{
			m: make(map[string]V),
		}
	}
	return &Map[V]{
		partitions: parts,
	}
}

// GetOrCreate retrieves the value for a given key or creates it using the provided function.
func (m *Map[V]) GetOrCreate(key string, fn func() (V, error)) (V, error) {
	idx := m.partition(key)
	return m.partitions[idx].getOrCreate(key, fn)
}

// Get retrieves the value for a given key.
func (m *Map[V]) Get(key string) (V, bool) {
	idx := m.partition(key)
	return m.partitions[idx].get(key)
}

// Set sets the value for a given key.
func (m *Map[V]) Set(key string, value V) V {
	idx := m.partition(key)
	return m.partitions[idx].set(key, value)
}

// Delete removes the value for a given key.
func (m *Map[V]) Delete(key string) (V, bool) {
	idx := m.partition(key)
	return m.partitions[idx].delete(key)
}

// Mutate applies a function to the value for a given key and sets the returned value atomically.
func (m *Map[V]) Mutate(key string, fn func(value V) (V, error)) error {
	idx := m.partition(key)
	return m.partitions[idx].mutate(key, fn)
}

// partition calculates the partition index for a given key.
func (m *Map[V]) partition(key string) uint64 {
	return xxhash.Sum64String(key) % uint64(len(m.partitions))
}

// Each applies a function to each key-value pair in the map.
func (m *Map[V]) Each(fn func(key string, value V) error) error {
	for _, partition := range m.partitions {
		if err := partition.apply(fn); err != nil {
			return err
		}
	}
	return nil
}

// Count returns the number of key-value pairs in the map.
func (m *Map[V]) Count() int {
	var count int
	for _, partition := range m.partitions {
		partition.mu.RLock()
		count += len(partition.m)
		partition.mu.RUnlock()
	}
	return count
}
