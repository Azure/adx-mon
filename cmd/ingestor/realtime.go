// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Azure/adx-mon/ingestor"
	"github.com/Azure/adx-mon/ingestor/adx"
	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/pkg/ingestpolicy"
	"github.com/Azure/adx-mon/schema"
	"github.com/Azure/adx-mon/storage"
	"github.com/urfave/cli/v2"
)

const (
	// maxStreamingRequestBytes is the ADX streaming ingestion request size limit.
	maxStreamingRequestBytes int64 = 4 * 1024 * 1024

	defaultRealtimeMaxSegmentAge              = 250 * time.Millisecond
	defaultRealtimeMaxBatchLatency            = 250 * time.Millisecond
	defaultRealtimeMaxBatchBytes        int64 = 512 * 1024
	defaultRealtimeMaxLag                     = 30 * time.Second
	defaultRealtimeReservedDiskBytes    int64 = 1024 * 1024 * 1024
	defaultQueuedReservedWorkersPercent       = 10
	defaultRealtimeMinSlots                   = 1
)

// realtimeCLIFlags returns the flags that configure realtime ingestion.
func realtimeCLIFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringSliceFlag{Name: "realtime-table", Usage: "Table that uses realtime streaming ingestion in the format <db>.<table>. Can be repeated."},
		&cli.StringSliceFlag{Name: "realtime-streaming-budget", Usage: "Maximum concurrent streaming ingestion requests this ingestor deployment may send to a Kusto endpoint in the format <endpoint>=<n>. The budget is divided among ingestor peers. Required for each endpoint with realtime tables."},
		&cli.IntFlag{Name: "realtime-min-slots", Usage: "Minimum streaming upload slots per ingestor per endpoint", Value: defaultRealtimeMinSlots},
		&cli.IntFlag{Name: "realtime-max-slots", Usage: "Maximum streaming upload slots per ingestor per endpoint. 0 for no limit", Value: 0},
		&cli.DurationFlag{Name: "realtime-max-segment-age", Usage: "Maximum age of a realtime segment before it is rotated", Value: defaultRealtimeMaxSegmentAge},
		&cli.DurationFlag{Name: "realtime-max-batch-latency", Usage: "Maximum time a closed realtime segment waits to be batched with others before upload", Value: defaultRealtimeMaxBatchLatency},
		&cli.Int64Flag{Name: "realtime-max-batch-bytes", Usage: "Maximum size of a realtime batch in compressed WAL bytes. Batches whose uncompressed data exceeds the 4MiB streaming limit use queued ingestion. Cannot exceed 4MiB", Value: defaultRealtimeMaxBatchBytes},
		&cli.DurationFlag{Name: "realtime-max-lag", Usage: "Maximum age of a realtime segment before it falls back to queued ingestion", Value: defaultRealtimeMaxLag},
		&cli.Int64Flag{Name: "realtime-reserved-disk-bytes", Usage: "Disk space in bytes reserved for realtime segments. Queued segments may use up to max-disk-usage minus this value. Only applies when realtime tables are configured", Value: defaultRealtimeReservedDiskBytes},
		&cli.IntFlag{Name: "queued-reserved-workers-percent", Usage: "Percentage of upload workers reserved for queued segments. At least one worker is always reserved", Value: defaultQueuedReservedWorkersPercent},
	}
}

// realtimeFlags holds the raw realtime flag values.
type realtimeFlags struct {
	Tables                       []string
	StreamingBudgets             []string
	MinSlots                     int
	MaxSlots                     int
	MaxSegmentAge                time.Duration
	MaxBatchLatency              time.Duration
	MaxBatchBytes                int64
	MaxLag                       time.Duration
	ReservedDiskBytes            int64
	QueuedReservedWorkersPercent int
}

func realtimeFlagsFromContext(ctx *cli.Context) realtimeFlags {
	return realtimeFlags{
		Tables:                       ctx.StringSlice("realtime-table"),
		StreamingBudgets:             ctx.StringSlice("realtime-streaming-budget"),
		MinSlots:                     ctx.Int("realtime-min-slots"),
		MaxSlots:                     ctx.Int("realtime-max-slots"),
		MaxSegmentAge:                ctx.Duration("realtime-max-segment-age"),
		MaxBatchLatency:              ctx.Duration("realtime-max-batch-latency"),
		MaxBatchBytes:                ctx.Int64("realtime-max-batch-bytes"),
		MaxLag:                       ctx.Duration("realtime-max-lag"),
		ReservedDiskBytes:            ctx.Int64("realtime-reserved-disk-bytes"),
		QueuedReservedWorkersPercent: ctx.Int("queued-reserved-workers-percent"),
	}
}

// realtimeConfig is the validated realtime configuration.
type realtimeConfig struct {
	Policy *ingestpolicy.Policy

	// StreamingBudgets is keyed by normalized Kusto endpoint.
	StreamingBudgets map[string]int

	MinSlots        int
	MaxSlots        int
	MaxSegmentAge   time.Duration
	MaxBatchLatency time.Duration
	MaxBatchBytes   int64
	MaxLag          time.Duration

	// ReservedDiskBytes is 0 when no realtime tables are configured so queued capacity is unchanged.
	ReservedDiskBytes            int64
	QueuedReservedWorkersPercent int
}

// newRealtimeConfig validates the realtime flags against the configured Kusto endpoints.
func newRealtimeConfig(ctx *cli.Context, metricsEndpoints, logsEndpoints []string, maxDiskUsage int64, backend storage.Backend) (*realtimeConfig, error) {
	databaseEndpoints := make(map[string]string, len(metricsEndpoints)+len(logsEndpoints))
	for _, v := range append(append([]string{}, metricsEndpoints...), logsEndpoints...) {
		endpoint, database, err := parseStorageEndpoint(v)
		if err != nil {
			return nil, err
		}
		databaseEndpoints[database] = endpoint
	}
	return parseRealtimeConfig(realtimeFlagsFromContext(ctx), databaseEndpoints, maxDiskUsage, backend)
}

// parseRealtimeConfig validates the realtime flags.  databaseEndpoints maps each configured Kusto database to its
// endpoint.
func parseRealtimeConfig(f realtimeFlags, databaseEndpoints map[string]string, maxDiskUsage int64, backend storage.Backend) (*realtimeConfig, error) {
	if f.MinSlots < 1 {
		return nil, errors.New("--realtime-min-slots must be at least 1")
	}
	if f.MaxSlots < 0 {
		return nil, errors.New("--realtime-max-slots must not be negative")
	}
	if f.MaxSlots > 0 && f.MaxSlots < f.MinSlots {
		return nil, fmt.Errorf("--realtime-max-slots (%d) must be 0 or at least --realtime-min-slots (%d)", f.MaxSlots, f.MinSlots)
	}
	if f.MaxSegmentAge <= 0 {
		return nil, errors.New("--realtime-max-segment-age must be greater than 0")
	}
	if f.MaxBatchLatency <= 0 {
		return nil, errors.New("--realtime-max-batch-latency must be greater than 0")
	}
	if f.MaxBatchBytes <= 0 || f.MaxBatchBytes > maxStreamingRequestBytes {
		return nil, fmt.Errorf("--realtime-max-batch-bytes must be between 1 and %d", maxStreamingRequestBytes)
	}
	if f.MaxLag <= 0 {
		return nil, errors.New("--realtime-max-lag must be greater than 0")
	}
	if f.ReservedDiskBytes < 0 {
		return nil, errors.New("--realtime-reserved-disk-bytes must not be negative")
	}
	if f.QueuedReservedWorkersPercent < 1 || f.QueuedReservedWorkersPercent > 99 {
		return nil, errors.New("--queued-reserved-workers-percent must be between 1 and 99")
	}

	tables := make([]ingestpolicy.Table, 0, len(f.Tables))
	for _, v := range f.Tables {
		t, err := parseRealtimeTable(v)
		if err != nil {
			return nil, err
		}
		tables = append(tables, t)
	}
	policy, err := ingestpolicy.New(tables)
	if err != nil {
		return nil, fmt.Errorf("--realtime-table: %w", err)
	}

	endpointsByDB := make(map[string]string, len(databaseEndpoints))
	knownEndpoints := make(map[string]struct{}, len(databaseEndpoints))
	for db, endpoint := range databaseEndpoints {
		ep := normalizeEndpoint(endpoint)
		endpointsByDB[schema.NormalizeAdxIdentifier(db)] = ep
		knownEndpoints[ep] = struct{}{}
	}

	budgets := make(map[string]int, len(f.StreamingBudgets))
	for _, v := range f.StreamingBudgets {
		endpoint, budget, err := parseStreamingBudget(v)
		if err != nil {
			return nil, err
		}
		if _, ok := knownEndpoints[endpoint]; !ok {
			return nil, fmt.Errorf("--realtime-streaming-budget endpoint %q is not a configured kusto endpoint", endpoint)
		}
		if _, ok := budgets[endpoint]; ok {
			return nil, fmt.Errorf("--realtime-streaming-budget endpoint %q is set more than once", endpoint)
		}
		budgets[endpoint] = budget
	}

	cfg := &realtimeConfig{
		Policy:                       policy,
		StreamingBudgets:             budgets,
		MinSlots:                     f.MinSlots,
		MaxSlots:                     f.MaxSlots,
		MaxSegmentAge:                f.MaxSegmentAge,
		MaxBatchLatency:              f.MaxBatchLatency,
		MaxBatchBytes:                f.MaxBatchBytes,
		MaxLag:                       f.MaxLag,
		QueuedReservedWorkersPercent: f.QueuedReservedWorkersPercent,
	}

	if !policy.HasRealtime() {
		return cfg, nil
	}

	if backend != storage.BackendADX {
		return nil, fmt.Errorf("--realtime-table is only supported with storage backend %q", storage.BackendADX)
	}

	for _, t := range policy.RealtimeTables() {
		endpoint, ok := endpointsByDB[t.Database]
		if !ok {
			return nil, fmt.Errorf("--realtime-table %q: database is not a configured metrics or logs database", t.String())
		}
		if _, ok := budgets[endpoint]; !ok {
			return nil, fmt.Errorf("--realtime-table %q: no --realtime-streaming-budget for endpoint %q", t.String(), endpoint)
		}
	}

	if f.ReservedDiskBytes >= maxDiskUsage {
		return nil, fmt.Errorf("--realtime-reserved-disk-bytes (%d) must be less than --max-disk-usage (%d)", f.ReservedDiskBytes, maxDiskUsage)
	}
	cfg.ReservedDiskBytes = f.ReservedDiskBytes

	return cfg, nil
}

// parseRealtimeTable parses a table in the format <db>.<table>.
func parseRealtimeTable(s string) (ingestpolicy.Table, error) {
	db, table, ok := strings.Cut(strings.TrimSpace(s), ".")
	if !ok || db == "" || table == "" || strings.Contains(table, ".") {
		return ingestpolicy.Table{}, fmt.Errorf("invalid --realtime-table %q: expected <db>.<table>", s)
	}
	return ingestpolicy.Table{Database: db, Table: table}, nil
}

// parseStreamingBudget parses a budget in the format <endpoint>=<n>.  The last '=' separates the budget so endpoints
// may contain '='.
func parseStreamingBudget(s string) (string, int, error) {
	i := strings.LastIndex(s, "=")
	if i < 0 {
		return "", 0, fmt.Errorf("invalid --realtime-streaming-budget %q: expected <endpoint>=<n>", s)
	}
	endpoint := normalizeEndpoint(s[:i])
	if endpoint == "" {
		return "", 0, fmt.Errorf("invalid --realtime-streaming-budget %q: endpoint is required", s)
	}
	budget, err := strconv.Atoi(strings.TrimSpace(s[i+1:]))
	if err != nil || budget < 1 {
		return "", 0, fmt.Errorf("invalid --realtime-streaming-budget %q: budget must be a positive integer", s)
	}
	return endpoint, budget, nil
}

// normalizeEndpoint returns a canonical form of a Kusto endpoint for comparison.
func normalizeEndpoint(endpoint string) string {
	return strings.ToLower(strings.TrimRight(strings.TrimSpace(endpoint), "/"))
}

// newStreamingSlots returns a streaming slot pool for each endpoint with a streaming budget.  Uploaders for databases
// on the same endpoint share its pool.
func (c *realtimeConfig) newStreamingSlots() map[string]*adx.StreamingSlots {
	slots := make(map[string]*adx.StreamingSlots, len(c.StreamingBudgets))
	if !c.Policy.HasRealtime() {
		return slots
	}
	for endpoint, budget := range c.StreamingBudgets {
		slots[endpoint] = adx.NewStreamingSlots(budget, c.MinSlots, c.MaxSlots, cluster.PeerInfo{Count: 1})
	}
	return slots
}

// uploadOpts returns the realtime upload options for an uploader of database on endpoint, or nil if the database has
// no realtime tables.
func (c *realtimeConfig) uploadOpts(database, endpoint string, slots map[string]*adx.StreamingSlots) *adx.RealtimeUploadOpts {
	if !c.hasRealtimeDatabase(database) {
		return nil
	}
	s, ok := slots[normalizeEndpoint(endpoint)]
	if !ok {
		return nil
	}
	return &adx.RealtimeUploadOpts{Slots: s, MaxLag: c.MaxLag}
}

func (c *realtimeConfig) hasRealtimeDatabase(database string) bool {
	db := schema.NormalizeAdxIdentifier(database)
	for _, v := range c.Policy.RealtimeDatabases() {
		if v == db {
			return true
		}
	}
	return false
}

// serviceOpts returns the realtime options for the ingestor service, or nil if no realtime tables are configured.
func (c *realtimeConfig) serviceOpts() *ingestor.RealtimeOpts {
	if !c.Policy.HasRealtime() {
		return nil
	}
	return &ingestor.RealtimeOpts{
		Policy:            c.Policy,
		MaxSegmentAge:     c.MaxSegmentAge,
		MaxBatchLatency:   c.MaxBatchLatency,
		MaxBatchBytes:     c.MaxBatchBytes,
		ReservedDiskBytes: c.ReservedDiskBytes,
	}
}

// peerListeners returns funcs that update the streaming slot pools when the ingestor's peers change.
func peerListeners(slots map[string]*adx.StreamingSlots) []func(cluster.PeerInfo) {
	listeners := make([]func(cluster.PeerInfo), 0, len(slots))
	for _, s := range slots {
		listeners = append(listeners, s.SetPeers)
	}
	return listeners
}
