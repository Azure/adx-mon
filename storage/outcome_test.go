package storage

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/multierr"
)

func TestSplitOutcomeDoesNotMaskFailure(t *testing.T) {
	failure := errors.New("export unavailable")
	o, err := SplitOutcome(multierr.Combine(&WriteOutcome{Accepted: 2, Rejected: 3}, failure))
	require.EqualValues(t, 2, o.Accepted)
	require.EqualValues(t, 3, o.Rejected)
	require.ErrorIs(t, err, failure)

	o, err = SplitOutcome(&WriteOutcome{Rejected: 4})
	require.NoError(t, err)
	require.EqualValues(t, 4, o.Rejected)

	o, err = SplitOutcome(nil)
	require.NoError(t, err)
	require.Zero(t, o.Rejected)
}

// A wrapped outcome must still report its shed counts so the data is not
// double-counted as lost, while the wrapper is preserved as a failure.
func TestSplitOutcomeHandlesWrappedReports(t *testing.T) {
	outcome := &WriteOutcome{Accepted: 1, Rejected: 7}
	wrapped := fmt.Errorf("flush failed: %w", outcome)

	o, err := SplitOutcome(wrapped)

	require.EqualValues(t, 7, o.Rejected, "shed counts survive wrapping")
	require.Error(t, err, "wrapping signals a condition beyond deliberate shedding")
	require.ErrorIs(t, err, outcome)
}

func TestSplitOutcomeAggregatesMultipleReports(t *testing.T) {
	o, err := SplitOutcome(multierr.Combine(
		&WriteOutcome{Accepted: 1, Rejected: 2},
		&WriteOutcome{Accepted: 3, Rejected: 4},
	))

	require.NoError(t, err)
	require.EqualValues(t, 4, o.Accepted)
	require.EqualValues(t, 6, o.Rejected)
}
