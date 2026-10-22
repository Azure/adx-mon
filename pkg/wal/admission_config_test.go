package wal

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAdmissionConfigDefaultsAndOverrides(t *testing.T) {
	opts, err := ResolveAdmission(Usage{1000, 100}, 0, 0, false, []TableLimit{{Database: "DB", Table: "A", MaxBytes: 200}})
	require.NoError(t, err)
	a := NewAdmission(opts)
	require.Equal(t, Usage{100, 10}, opts.Default)
	require.Equal(t, Usage{200, 10}, a.Limit(Destination{"DB", "A"}))
	opts, err = ResolveAdmission(Usage{1000, 100}, 0, 0, true, nil)
	require.NoError(t, err)
	require.Equal(t, Usage{}, opts.Default)
	for _, limits := range [][]TableLimit{{{Database: "DB", Table: "bad_name"}}, {{Database: "DB", Table: "A", MaxBytes: -1}}, {{Database: "DB", Table: "A", MaxBytes: 1001}}, {{Database: "DB", Table: "A"}, {Database: "DB", Table: "A"}}} {
		_, err := ResolveAdmission(Usage{1000, 100}, 0, 0, false, limits)
		require.Error(t, err)
	}
	_, err = ResolveAdmission(Usage{1000, 100}, -1, 0, false, nil)
	require.Error(t, err)
}

func TestAdmissionStatusAndLabelRetirement(t *testing.T) {
	a := NewAdmission(AdmissionOpts{Enforce: true, Default: Usage{100, 1}})
	d := Destination{"DB", "A"}
	a.Restore("a", d, 100)
	_, err := a.Reserve("a", d, 1, 0)
	require.Error(t, err)
	require.True(t, a.RecordRejection(d, 2, 10))
	require.False(t, a.RecordRejection(d, 2, 10))
	s := a.Status()[d]
	require.True(t, s.Throttled)
	require.EqualValues(t, 4, s.RejectedRecords)
	a.Forget(d, "a")
	require.Empty(t, a.Status())
}
