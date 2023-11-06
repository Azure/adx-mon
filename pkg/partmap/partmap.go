package partmap

import (
	"sync"

	"github.com/cespare/xxhash"
)

type Map struct {
	partitions []syncMap
}

type syncMap struct {
	mu sync.RWMutex
	m  map[string]any
}

func (s *syncMap) get(key string) (any, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	v, ok := s.m[key]
	return v, ok
}

func (s *syncMap) set(key string, value any) any {
	s.mu.Lock()
	defer s.mu.Unlock()

	v, ok := s.m[key]
	if ok {
		return v
	}
	s.m[key] = value
	return value
}

func (s *syncMap) delete(key string) (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	v := s.m[key]
	if v == nil {
		return nil, false
	}

	delete(s.m, key)
	return v, true
}

func (s *syncMap) getOrCreate(key string, fn func() (any, error)) (any, error) {
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
		return nil, err
	}
	s.m[key] = v
	return v, nil
}

func NewMap(partitons int) *Map {
	partitions := make([]syncMap, partitons)
	for i := range partitions {
		partitions[i] = syncMap{
			m: make(map[string]any),
		}
	}
	return &Map{
		partitions: partitions,
	}
}

func (m *Map) GetOrCreate(key string, fn func() (any, error)) (any, error) {
	idx := m.partition(key)
	return m.partitions[idx].getOrCreate(key, fn)
}

func (m *Map) Get(key string) (any, bool) {
	idx := m.partition(key)
	return m.partitions[idx].get(key)
}

func (m *Map) Set(key string, value any) any {
	idx := m.partition(key)
	return m.partitions[idx].set(key, value)
}

func (m *Map) Delete(key string) (any, bool) {
	idx := m.partition(key)
	return m.partitions[idx].delete(key)
}

func (m *Map) partition(key string) uint64 {
	return xxhash.Sum64String(key) % uint64(len(m.partitions))
}

func (m *Map) Each(fn func(key string, value any) error) error {
	for _, partition := range m.partitions {
		partition.mu.RLock()
		for key, value := range partition.m {
			if err := fn(key, value); err != nil {
				partition.mu.RUnlock()
				return err
			}
		}
		partition.mu.RUnlock()
	}
	return nil
}

func (m *Map) Count() int {
	var count int
	for _, partition := range m.partitions {
		partition.mu.RLock()
		count += len(partition.m)
		partition.mu.RUnlock()
	}
	return count
}
