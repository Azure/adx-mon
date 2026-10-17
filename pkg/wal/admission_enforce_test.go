package wal

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestObserveOnlyAdmitsAndReports(t *testing.T) {
	d := Destination{Database: "DB", Table: "A"}

	opts, err := ResolveAdmission(Usage{Bytes: 1000, Segments: 100}, 10, 1, false, nil)
	require.NoError(t, err)
	require.False(t, opts.Enforce, "table limits must default to observe-only")

	a := NewAdmission(opts)
	r, err := a.Reserve("a", d, 10, 1)
	require.NoError(t, err)
	r.Finish(10)
	r.Release()

	// Over budget: admitted, but reported as a would-be rejection.
	r, err = a.Reserve("a", d, 500, 0)
	require.NoError(t, err, "observe-only must not reject")
	r.Finish(500)
	r.Release()

	status := a.Status()[d]
	require.False(t, status.Enforced)
	require.EqualValues(t, 1, status.WouldReject)
	require.True(t, status.Throttled, "throttle state is still reported")
	require.EqualValues(t, 510, status.Bytes, "usage accounting is unaffected")
}

func TestEnforcementRejectsWhenEnabled(t *testing.T) {
	d := Destination{Database: "DB", Table: "A"}

	opts, err := ResolveAdmissionWithEnforcement(Usage{Bytes: 1000, Segments: 100}, 10, 1, false, true, nil)
	require.NoError(t, err)
	require.True(t, opts.Enforce)

	a := NewAdmission(opts)
	r, err := a.Reserve("a", d, 10, 1)
	require.NoError(t, err)
	r.Finish(10)
	r.Release()

	_, err = a.Reserve("a", d, 500, 0)
	var quota *TableOverloadedError
	require.ErrorAs(t, err, &quota)
	require.True(t, a.Status()[d].Enforced)
}

func TestObserveOnlyStillEnforcesGlobalLimits(t *testing.T) {
	a := NewAdmission(AdmissionOpts{Global: Usage{Bytes: 100}, Default: Usage{Bytes: 10}})
	d := Destination{Database: "DB", Table: "A"}

	r, err := a.Reserve("a", d, 100, 1)
	require.NoError(t, err, "table budget is observe-only")
	r.Finish(100)
	r.Release()

	_, err = a.Reserve("b", Destination{Database: "DB", Table: "B"}, 1, 1)
	require.ErrorIs(t, err, ErrMaxDiskUsageExceeded, "global capacity still protects the instance")
}
