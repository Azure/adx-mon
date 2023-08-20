package counter

import (
	"encoding/binary"
	"sync"

	"github.com/tylertreat/BoomFilters"
)

type Estimator struct {
	mu  sync.RWMutex
	hll *boom.HyperLogLog
	buf [8]byte
}

func NewEstimator() *Estimator {
	hll, err := boom.NewDefaultHyperLogLog(0.01)
	if err != nil {
		panic(err)
	}
	return &Estimator{
		hll: hll,
	}
}

func (e *Estimator) Count() uint64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.hll.Count()
}

func (e *Estimator) Add(i uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	binary.LittleEndian.PutUint64(e.buf[:8], i)
	e.hll.Add(e.buf[:8])
}

func (e *Estimator) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.hll.Reset()
}
