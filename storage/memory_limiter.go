package storage

import (
	"sync/atomic"
)

// MemoryLimiter bounds the total bytes buffered by concurrent intake requests.
// Disk budgets alone do not protect the process: each transfer buffers its body
// before admission, so without an aggregate ceiling enough concurrent requests
// can exhaust memory regardless of how much disk capacity remains.
type MemoryLimiter struct {
	limit int64
	used  atomic.Int64
}

func NewMemoryLimiter(limit int64) *MemoryLimiter {
	return &MemoryLimiter{limit: limit}
}

// Reserve claims n bytes of intake memory. It reports false when the claim
// would exceed the ceiling, in which case the caller must shed or retry later.
func (m *MemoryLimiter) Reserve(n int64) bool {
	if m == nil || m.limit <= 0 {
		return true
	}
	for {
		current := m.used.Load()
		if current+n > m.limit {
			return false
		}
		if m.used.CompareAndSwap(current, current+n) {
			return true
		}
	}
}

// Release returns previously reserved bytes.
func (m *MemoryLimiter) Release(n int64) {
	if m == nil || m.limit <= 0 {
		return
	}
	m.used.Add(-n)
}

// InFlight reports the bytes currently reserved.
func (m *MemoryLimiter) InFlight() int64 {
	if m == nil {
		return 0
	}
	return m.used.Load()
}
