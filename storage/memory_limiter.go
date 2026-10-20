package storage

import (
	"io"
	"sync/atomic"
)

// intakeStep is how much memory an import claims ahead of each read. Claiming
// in steps charges a request for what it actually buffers, so a small transfer
// is admitted even when its destination's disk budget exceeds the ceiling.
const intakeStep = 64 * 1024

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

// intakeReader charges every byte read from r to m before reading it, so the
// buffered body never exceeds the claim. Read returns ErrIntakeMemoryExhausted
// once the ceiling is reached. The caller must call Release when the buffered
// body is no longer needed.
type intakeReader struct {
	r        io.Reader
	m        *MemoryLimiter
	reserved int64
	read     int64
}

func (r *intakeReader) Read(p []byte) (int, error) {
	if r.read >= r.reserved {
		step := int64(intakeStep)
		if r.m != nil && r.m.limit > 0 {
			step = min(step, r.m.limit)
		}
		if !r.m.Reserve(step) {
			// A body that ends exactly at the claim is only known to be
			// complete after one more read, so probe before rejecting it.
			var probe [1]byte
			if n, err := r.r.Read(probe[:]); n == 0 && err != nil {
				return 0, err
			}
			return 0, ErrIntakeMemoryExhausted
		}
		r.reserved += step
	}
	if avail := r.reserved - r.read; int64(len(p)) > avail {
		p = p[:avail]
	}
	n, err := r.r.Read(p)
	r.read += int64(n)
	return n, err
}

// Release returns the memory claimed by all reads.
func (r *intakeReader) Release() {
	r.m.Release(r.reserved)
	r.reserved = 0
}
