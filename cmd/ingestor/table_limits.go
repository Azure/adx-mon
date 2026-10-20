package main

import (
	"fmt"

	"github.com/Azure/adx-mon/ingestor"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/urfave/cli/v2"
)

// GenericFlag preserves commas inside JSON while accepting repeated overrides.
type tableLimitValues struct{ limits []wal.TableLimit }

func (v *tableLimitValues) String() string { return fmt.Sprint(v.limits) }
func (v *tableLimitValues) Set(value string) error {
	limit, err := wal.ParseTableLimit(value)
	if err != nil {
		return err
	}
	v.limits = append(v.limits, limit)
	return nil
}

// tableIsolationOpts holds the table budget and intake memory flags.
type tableIsolationOpts struct {
	maxTableDiskUsage    int64
	maxTableSegmentCount int64
	disableTableLimits   bool
	enforceTableLimits   bool
	tableLimits          []wal.TableLimit
	maxIntakeMemory      int64
}

// parseTableIsolationOpts reads and validates the table isolation flags, so an
// invalid override fails before any clients are created.
func parseTableIsolationOpts(ctx *cli.Context) (tableIsolationOpts, error) {
	opts := tableIsolationOpts{
		maxTableDiskUsage:    ctx.Int64("max-table-disk-usage"),
		maxTableSegmentCount: ctx.Int64("max-table-segment-count"),
		disableTableLimits:   ctx.Bool("disable-table-limits"),
		enforceTableLimits:   ctx.Bool("enforce-table-limits"),
		tableLimits:          ctx.Generic("table-limit").(*tableLimitValues).limits,
		maxIntakeMemory:      ctx.Int64("max-intake-memory"),
	}
	global := wal.Usage{Bytes: ctx.Int64("max-disk-usage"), Segments: ctx.Int64("max-segment-count")}
	if _, err := wal.ResolveAdmissionWithEnforcement(global, opts.maxTableDiskUsage, opts.maxTableSegmentCount, opts.disableTableLimits, opts.enforceTableLimits, opts.tableLimits); err != nil {
		return tableIsolationOpts{}, err
	}
	return opts, nil
}

// apply copies the table isolation settings into the service options.
func (o tableIsolationOpts) apply(svc *ingestor.ServiceOpts) {
	svc.MaxTableDiskUsage = o.maxTableDiskUsage
	svc.MaxTableSegmentCount = o.maxTableSegmentCount
	svc.DisableTableLimits = o.disableTableLimits
	svc.EnforceTableLimits = o.enforceTableLimits
	svc.TableLimits = o.tableLimits
	svc.MaxIntakeMemory = o.maxIntakeMemory
}
