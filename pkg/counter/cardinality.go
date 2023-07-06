package counter

import (
	"encoding/binary"
	"sync"

	"github.com/tylertreat/BoomFilters"
)

type Estimator struct {
	mu  sync.RWMutex
	hll *boom.HyperLogLog
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

	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:8], i)
	e.hll.Add(buf[:8])
}

func (e *Estimator) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.hll.Reset()
}
