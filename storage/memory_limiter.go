package storage

import (
	"io"
	"math"
	"sync/atomic"

	gbp "github.com/libp2p/go-buffer-pool"
)

// intakeStep is the size of the buffer an import starts with.  Growing from a
// small buffer charges a request for what it actually buffers, so a small
// transfer is admitted even when its destination's disk budget exceeds the
// ceiling.
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
// Without a ceiling every claim succeeds but is still counted, so usage can be
// measured before a ceiling is chosen.
func (m *MemoryLimiter) Reserve(n int64) bool {
	if m == nil {
		return true
	}
	if m.limit <= 0 {
		m.used.Add(n)
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
	if m == nil {
		return
	}
	m.used.Add(-n)
}

// Limit reports the ceiling; zero means it is disabled.
func (m *MemoryLimiter) Limit() int64 {
	if m == nil {
		return 0
	}
	return m.limit
}

// InFlight reports the bytes currently reserved.
func (m *MemoryLimiter) InFlight() int64 {
	if m == nil {
		return 0
	}
	return m.used.Load()
}

// IntakeNeeded returns the intake memory an import of a size-byte body is charged at its peak: the buffer that holds
// it, the smallest intakeStep doubled enough times, plus the half-size buffer it grows from.
func IntakeNeeded(size int64) int64 {
	// A body this large could never be buffered; saturate rather than overflow while doubling.
	if size > math.MaxInt64/4 {
		return math.MaxInt64
	}
	c := int64(intakeStep)
	for c < size {
		c *= 2
	}
	if c == intakeStep {
		return c
	}
	return c + c/2
}

// intakeBuffer reads a request body into memory charged to a MemoryLimiter.  It charges the capacity of the buffers
// it holds rather than the bytes read: the buffer grows by doubling, and while it grows the old and the new buffer
// are both held, so charging bytes read let concurrent imports hold several times the ceiling.
type intakeBuffer struct {
	m       *MemoryLimiter
	buf     []byte
	pooled  bool
	charged int64
}

// readIntake reads r into an intakeBuffer.  It returns ErrIntakeMemoryExhausted when the buffer cannot grow within
// m's ceiling.  The caller must call Release when the body is no longer needed, also after an error.
func readIntake(r io.Reader, m *MemoryLimiter) (*intakeBuffer, error) {
	b := &intakeBuffer{m: m}
	first := int64(intakeStep)
	if m != nil && m.limit > 0 {
		first = min(first, m.limit)
	}
	if !b.grow(int(first)) {
		return b, ErrIntakeMemoryExhausted
	}
	for {
		if len(b.buf) == cap(b.buf) {
			// A body that ends exactly at the buffer's capacity is only known to be complete after one more read,
			// so probe before growing.
			var probe [1]byte
			n, err := r.Read(probe[:])
			if n == 0 {
				if err == io.EOF {
					return b, nil
				} else if err != nil {
					return b, err
				}
				continue
			}
			if !b.grow(2 * cap(b.buf)) {
				return b, ErrIntakeMemoryExhausted
			}
			b.buf = append(b.buf, probe[0])
			if err == io.EOF {
				return b, nil
			} else if err != nil {
				return b, err
			}
			continue
		}
		n, err := r.Read(b.buf[len(b.buf):cap(b.buf)])
		b.buf = b.buf[:len(b.buf)+n]
		if err == io.EOF {
			return b, nil
		} else if err != nil {
			return b, err
		}
	}
}

// grow moves the body into a buffer of size bytes, charging it before allocating, and reports false without
// growing when the charge does not fit.
func (b *intakeBuffer) grow(size int) bool {
	if !b.m.Reserve(int64(size)) {
		return false
	}
	var next []byte
	pooled := size&(size-1) == 0
	if pooled {
		next = gbp.Get(size)[:len(b.buf)]
	} else {
		next = make([]byte, len(b.buf), size)
	}
	copy(next, b.buf)
	b.free()
	b.buf, b.pooled, b.charged = next, pooled, int64(size)
	return true
}

// free returns the buffer and its charge.
func (b *intakeBuffer) free() {
	if b.buf != nil && b.pooled {
		gbp.Put(b.buf[:cap(b.buf)])
	}
	b.m.Release(b.charged)
	b.buf, b.charged = nil, 0
}

// Bytes returns the body read.
func (b *intakeBuffer) Bytes() []byte { return b.buf }

// Release returns the buffer and the memory charged for it.
func (b *intakeBuffer) Release() { b.free() }
