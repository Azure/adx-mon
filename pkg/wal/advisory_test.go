package wal

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Sustained throttling is the signal that distinguishes a transient burst from
// a destination that needs a larger budget.
func TestThrottledDurationIsReported(t *testing.T) {
	d := Destination{Database: "DB", Table: "A"}
	a := NewAdmission(AdmissionOpts{Enforce: true, Default: Usage{Bytes: 100}})

	r, err := a.Reserve("a", d, 100, 1)
	require.NoError(t, err)
	r.Finish(100)
	r.Release()

	_, err = a.Reserve("a", d, 10, 0)
	require.Error(t, err)

	status := a.Status()[d]
	require.True(t, status.Throttled)
	require.GreaterOrEqual(t, status.ThrottledFor, time.Duration(0))

	budget := a.Table(d)
	budget.mu.Lock()
	budget.throttledSince = time.Now().Add(-10 * time.Minute)
	budget.mu.Unlock()

	require.InDelta(t, (10 * time.Minute).Seconds(), a.Status()[d].ThrottledFor.Seconds(), 5)
}

// Recovery must clear the advisory so an alert does not latch.
func TestThrottledDurationClearsAfterDrain(t *testing.T) {
	d := Destination{Database: "DB", Table: "A"}
	a := NewAdmission(AdmissionOpts{Enforce: true, Default: Usage{Bytes: 100}})

	r, err := a.Reserve("a", d, 100, 1)
	require.NoError(t, err)
	r.Finish(100)
	r.Release()
	_, err = a.Reserve("a", d, 10, 0)
	require.Error(t, err)
	require.True(t, a.Status()[d].Throttled)

	a.Forget(d, "a")

	r, err = a.Reserve("b", d, 10, 1)
	require.NoError(t, err, "capacity returned")
	r.Finish(10)
	r.Release()

	status := a.Status()[d]
	require.False(t, status.Throttled)
	require.Zero(t, status.ThrottledFor, "advisory clears once the destination recovers")
}

// A destination that is not throttled must report no duration.
func TestHealthyDestinationReportsNoThrottleDuration(t *testing.T) {
	d := Destination{Database: "DB", Table: "A"}
	a := NewAdmission(AdmissionOpts{Enforce: true, Default: Usage{Bytes: 1000}})

	r, err := a.Reserve("a", d, 10, 1)
	require.NoError(t, err)
	r.Finish(10)
	r.Release()

	require.Zero(t, a.Status()[d].ThrottledFor)
}
