package storage

import (
	"errors"
	"fmt"

	"github.com/Azure/adx-mon/pkg/prompb"
	"go.uber.org/multierr"
)

// WriteOutcome reports records that were intentionally shed because a table
// exceeded its budget. It is carried through the error return of the storage
// and remote-write interfaces, but it is not a failure: the records are
// deliberately discarded and must not be retried by the producer.
//
// Callers extract it with SplitOutcome rather than treating it as an error.
// RejectedSeries is only valid until the caller returns those series to their
// pool.
type WriteOutcome struct {
	Accepted       int64
	Rejected       int64
	RejectedSeries []*prompb.TimeSeries
}

func (o *WriteOutcome) Error() string {
	return fmt.Sprintf("table quota: accepted %d, shed %d records", o.Accepted, o.Rejected)
}

func (o *WriteOutcome) add(other *WriteOutcome) {
	o.Accepted += other.Accepted
	o.Rejected += other.Rejected
	o.RejectedSeries = append(o.RejectedSeries, other.RejectedSeries...)
}

// SplitOutcome separates intentional shedding from failures that require a
// retry. A successful local write must never mask a failure reported by
// another destination.
//
// A wrapped outcome still contributes its shed counts, but it is also treated
// as a failure: wrapping signals that the caller attached a condition beyond
// deliberate shedding, and silently swallowing that would drop data.
func SplitOutcome(err error) (WriteOutcome, error) {
	var result WriteOutcome
	var failure error
	for _, e := range multierr.Errors(err) {
		var outcome *WriteOutcome
		if errors.As(e, &outcome) {
			result.add(outcome)
			if e == error(outcome) {
				continue
			}
		}
		failure = multierr.Append(failure, e)
	}
	return result, failure
}
