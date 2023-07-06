package counter

import (
	"sync/atomic"
)

// RollingEstimator is a cardinality estimator that can track cardinality over time without big drops in counts when
// counting windows roll over. There are two HyperLogLog (HLL) counters that are used to track counts.  One is active
// for reading, but both are written to.  When the Roll func is called, the active HLL switches to the in-active one
// and all items that were added in the prior widow are retained.
type RollingEstimator struct {
	i          uint64
	estimators [2]*Estimator
}

func NewRollingEstimator() *RollingEstimator {
	return &RollingEstimator{
		estimators: [2]*Estimator{
			NewEstimator(),
			NewEstimator(),
		},
	}
}

// Count returns the current count of items.
func (e *RollingEstimator) Count() uint64 {
	idx := atomic.LoadUint64(&e.i)
	return e.estimators[idx%2].Count()
}

// Add adds an item to the estimator.
func (e *RollingEstimator) Add(i uint64) {
	idx := atomic.LoadUint64(&e.i)

	e.estimators[idx%2].Add(i)
	e.estimators[(idx+1)%2].Add(i)
}

// Reset resets the estimator.
func (e *RollingEstimator) Reset() {
	e.estimators[0].Reset()
	e.estimators[1].Reset()
}

// Roll switches the active counter.  This should be called on some cadence to periodically expire items no longer
// being counted.
func (e *RollingEstimator) Roll() {
	idx := atomic.AddUint64(&e.i, 1)
	e.estimators[(idx-1)%2].Reset()
}
