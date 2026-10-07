// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package config

import (
	"errors"
	"fmt"
	"time"

	"github.com/Azure/adx-mon/pkg/ingestpolicy"
)

const (
	// DefaultMaxDiskUsage is the collector's max-disk-usage when it is not set.  It must match the default applied in
	// collector.NewService.
	DefaultMaxDiskUsage int64 = 10 * 1024 * 1024 * 1024

	DefaultRealtimeMaxSegmentAge                      = 250 * time.Millisecond
	DefaultRealtimeMaxBatchLatency                    = 500 * time.Millisecond
	DefaultRealtimeMaxBatchBytes                int64 = 2 * 1024 * 1024
	DefaultRealtimeReservedDiskBytes            int64 = 1024 * 1024 * 1024
	DefaultRealtimeQueuedReservedWorkersPercent       = 10
)

// Realtime configures realtime ingestion for selected tables.
type Realtime struct {
	MaxSegmentAgeMs              int              `toml:"max-segment-age-ms,omitempty" comment:"Maximum age in milliseconds of a realtime WAL segment before it is rotated. Defaults to 250."`
	MaxBatchLatencyMs            int              `toml:"max-batch-latency-ms,omitempty" comment:"Maximum time in milliseconds a closed realtime segment waits to be batched with others before it is transferred. Defaults to 500."`
	MaxBatchBytes                int64            `toml:"max-batch-bytes,omitempty" comment:"Maximum size of a realtime transfer batch in compressed WAL bytes. Defaults to 2097152."`
	ReservedDiskBytes            int64            `toml:"reserved-disk-bytes,omitempty" comment:"Disk space in bytes reserved for realtime segments. Queued segments may use up to max-disk-usage minus this value. Only applies when tables are configured. Defaults to 1073741824."`
	QueuedReservedWorkersPercent int              `toml:"queued-reserved-workers-percent,omitempty" comment:"Percentage of transfer workers reserved for queued segments so realtime traffic cannot starve them. At least one worker is always reserved. Defaults to 10."`
	Tables                       []*RealtimeTable `toml:"tables,omitempty" comment:"Tables that use realtime ingestion."`
}

// RealtimeTable identifies a realtime table.
type RealtimeTable struct {
	Database string `toml:"database" comment:"Database of the realtime table."`
	Table    string `toml:"table" comment:"Name of the realtime table."`
}

// Validate validates the realtime config and applies defaults.  maxDiskUsage is the effective collector max disk
// usage.
func (r *Realtime) Validate(maxDiskUsage int64) error {
	if r.MaxSegmentAgeMs < 0 {
		return errors.New("realtime.max-segment-age-ms must be greater than 0")
	}
	if r.MaxSegmentAgeMs == 0 {
		r.MaxSegmentAgeMs = int(DefaultRealtimeMaxSegmentAge / time.Millisecond)
	}

	if r.MaxBatchLatencyMs < 0 {
		return errors.New("realtime.max-batch-latency-ms must be greater than 0")
	}
	if r.MaxBatchLatencyMs == 0 {
		r.MaxBatchLatencyMs = int(DefaultRealtimeMaxBatchLatency / time.Millisecond)
	}

	if r.MaxBatchBytes < 0 {
		return errors.New("realtime.max-batch-bytes must be greater than 0")
	}
	if r.MaxBatchBytes == 0 {
		r.MaxBatchBytes = DefaultRealtimeMaxBatchBytes
	}

	if r.ReservedDiskBytes < 0 {
		return errors.New("realtime.reserved-disk-bytes must be greater than 0")
	}
	if r.ReservedDiskBytes == 0 {
		r.ReservedDiskBytes = DefaultRealtimeReservedDiskBytes
	}

	if r.QueuedReservedWorkersPercent < 0 || r.QueuedReservedWorkersPercent >= 100 {
		return errors.New("realtime.queued-reserved-workers-percent must be between 1 and 99")
	}
	if r.QueuedReservedWorkersPercent == 0 {
		r.QueuedReservedWorkersPercent = DefaultRealtimeQueuedReservedWorkersPercent
	}

	for i, t := range r.Tables {
		if t == nil {
			return fmt.Errorf("realtime.tables[%d] must not be empty", i)
		}
		if t.Database == "" {
			return fmt.Errorf("realtime.tables[%d].database must be set", i)
		}
		if t.Table == "" {
			return fmt.Errorf("realtime.tables[%d].table must be set", i)
		}
	}

	if _, err := r.Policy(); err != nil {
		return fmt.Errorf("realtime.tables: %w", err)
	}

	// Without realtime tables no disk is reserved so queued capacity is unchanged.
	if len(r.Tables) == 0 {
		r.ReservedDiskBytes = 0
		return nil
	}
	if r.ReservedDiskBytes >= maxDiskUsage {
		return fmt.Errorf("realtime.reserved-disk-bytes (%d) must be less than max-disk-usage (%d)", r.ReservedDiskBytes, maxDiskUsage)
	}
	return nil
}

// Policy returns the ingestion priority policy for the configured tables.
func (r *Realtime) Policy() (*ingestpolicy.Policy, error) {
	if r == nil {
		return ingestpolicy.New(nil)
	}
	tables := make([]ingestpolicy.Table, 0, len(r.Tables))
	for _, t := range r.Tables {
		if t == nil {
			continue
		}
		tables = append(tables, ingestpolicy.Table{Database: t.Database, Table: t.Table})
	}
	return ingestpolicy.New(tables)
}

// MaxSegmentAge returns the realtime segment age as a duration.  Validate must be called first.
func (r *Realtime) MaxSegmentAge() time.Duration {
	return time.Duration(r.MaxSegmentAgeMs) * time.Millisecond
}

// MaxBatchLatency returns the realtime batch latency as a duration.  Validate must be called first.
func (r *Realtime) MaxBatchLatency() time.Duration {
	return time.Duration(r.MaxBatchLatencyMs) * time.Millisecond
}
