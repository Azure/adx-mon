package wal

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
)

func TestAdmissionIsolationAndRecovery(t *testing.T) {
	a := NewAdmission(AdmissionOpts{Enforce: true, Global: Usage{200, 10}, Default: Usage{100, 2}})
	d := Destination{"DB", "A"}
	b := Destination{"DB", "B"}
	r, err := a.Reserve("a", d, 100, 1)
	require.NoError(t, err)
	r.Finish(100)
	r.Finish(100)
	_, err = a.Reserve("a", d, 1, 0)
	var quota *TableOverloadedError
	require.ErrorAs(t, err, &quota)
	r, err = a.Reserve("b", b, 50, 1)
	require.NoError(t, err)
	r.Finish(50)
	require.Equal(t, Usage{150, 2}, a.Usage())
	a.Forget(d, "a")
	a.Forget(d, "a")
	r, err = a.Reserve("a2", d, 90, 1)
	require.NoError(t, err)
	r.Finish(0)
	require.Equal(t, Usage{50, 1}, a.Usage())
}

func TestAdmissionConcurrentReservations(t *testing.T) {
	a := NewAdmission(AdmissionOpts{Enforce: true, Global: Usage{100, 10}, Default: Usage{100, 10}})
	var wg sync.WaitGroup
	for n := 0; n < 100; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			r, err := a.Reserve(fmt.Sprint(n), Destination{"DB", "A"}, 10, 1)
			if err == nil {
				r.Finish(10)
			}
		}(n)
	}
	wg.Wait()
	require.Equal(t, Usage{100, 10}, a.Usage())
}

func TestAdmissionOverridesGlobalAndHysteresis(t *testing.T) {
	d := Destination{"DB", "A"}
	a := NewAdmission(AdmissionOpts{Enforce: true, Global: Usage{100, 3}, Default: Usage{50, 1}, Overrides: []TableLimit{{"DB", "A", 90, 3}}})
	a.Restore("a", d, 80)
	a.Restore("a", d, 80)
	_, err := a.Reserve("a", d, 11, 0)
	require.Error(t, err)
	_, err = a.Reserve("a", d, 1, 0)
	require.Error(t, err) // remains throttled until low water
	a.Forget(d, "a")
	r, err := a.Reserve("a", d, 90, 1)
	require.NoError(t, err)
	r.Finish(90)
	_, err = a.Reserve("b", Destination{"DB", "B"}, 11, 1)
	require.ErrorIs(t, err, ErrMaxDiskUsageExceeded)
}

func TestReservationReleaseRollsBackAndReusesSafely(t *testing.T) {
	a := NewAdmission(AdmissionOpts{Enforce: true, Default: Usage{100, 10}})
	for n := 0; n < 100; n++ {
		r, err := a.Reserve("a", Destination{"DB", "A"}, 10, 1)
		require.NoError(t, err)
		r.Release()
		require.Equal(t, Usage{}, a.Usage())
		r, err = a.Reserve("a", Destination{"DB", "A"}, 10, 1)
		require.NoError(t, err)
		r.Finish(10)
		r.Finish(10)
		r.Release()
		require.Equal(t, Usage{10, 1}, a.Usage())
		a.Forget(Destination{"DB", "A"}, "a")
	}
}
