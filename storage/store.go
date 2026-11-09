package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"text/tabwriter"
	"time"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/ingestpolicy"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/otlp"
	"github.com/Azure/adx-mon/pkg/pool"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/service"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/adx-mon/schema"
	transform2 "github.com/Azure/adx-mon/transform"
	gbp "github.com/libp2p/go-buffer-pool"
)

var (
	csvWriterPool = pool.NewGeneric(1000, func(sz int) any {
		return transform2.NewCSVWriter(bytes.NewBuffer(make([]byte, 0, sz)), nil)
	})

	nativeLogsCSVWriterPool = pool.NewGeneric(1000, func(sz int) any {
		return transform2.NewCSVNativeLogsCSVWriter(bytes.NewBuffer(make([]byte, 0, sz)), nil)
	})

	metricsCSVWriterPool = pool.NewGeneric(1000, func(sz int) any {
		return transform2.NewMetricsCSVWriter(bytes.NewBuffer(make([]byte, 0, sz)), nil)
	})
)

// ErrIntakeMemoryExhausted reports that too many concurrent requests are
// already buffered. It is retryable: the sender keeps its data.
var ErrIntakeMemoryExhausted = errors.New("intake memory exhausted")

type Store interface {
	service.Component

	// WriteTimeSeries writes a batch of time series to the Store.
	WriteTimeSeries(ctx context.Context, req *prompb.WriteRequest) error

	// WriteOTLPLogs writes a batch of logs to the Store.
	WriteOTLPLogs(ctx context.Context, database, table string, logs *otlp.Logs) error

	// WriteNativeLogs writes a batch of logs to the Store.
	WriteNativeLogs(ctx context.Context, logs *types.LogBatch) error

	// Import imports a file into the LocalStore and returns the number of bytes stored.
	Import(filename string, body io.ReadCloser) (int, error)
}

// LocalStore provides local storage of time series data.  It manages Write Ahead Logs (WALs) for each metric.
type LocalStore struct {
	opts StoreOpts

	mu         sync.RWMutex
	repository *wal.Repository

	inflightWriteBytes atomic.Int64
	unregisterBudget   func()
	unregisterIntake   func()
	intake             *MemoryLimiter
}

type StoreOpts struct {
	StorageDir             string
	SegmentMaxSize         int64
	SegmentMaxAge          time.Duration
	MaxDiskUsage           int64
	StartupOpenConcurrency int
	Backend                Backend

	LiftedLabels     []string
	LiftedAttributes []string
	LiftedResources  []string
	WALFlushInterval time.Duration
	EnableWALFsync   bool

	// Policy assigns an ingestion priority to each table.  When nil, all tables are queued.
	Policy *ingestpolicy.Policy

	// Realtime overrides segment rotation settings for realtime tables.
	Realtime wal.RotationPolicy

	// UnownedSegmentMaxAge, when positive, replaces SegmentMaxAge for queued tables that Owned reports are owned by
	// another node.
	UnownedSegmentMaxAge time.Duration

	// Owned reports whether this node owns the table of a WAL prefix.  When nil, every table is owned.
	Owned func(prefix string) bool

	// RealtimeReservedDiskBytes is disk space reserved for realtime tables.  Queued writes are rejected once disk
	// usage reaches MaxDiskUsage minus this value.
	RealtimeReservedDiskBytes int64

	// Admission owns per-destination capacity accounting.
	Admission *wal.Admission

	// MetricComponent labels exported table budget metrics.
	MetricComponent string

	// MaxIntakeMemory bounds the total bytes buffered across concurrent
	// imports. Zero disables the ceiling.
	MaxIntakeMemory int64
}

func NewLocalStore(opts StoreOpts) *LocalStore {
	if opts.Backend == "" {
		opts.Backend = BackendADX
	}

	return &LocalStore{
		opts:   opts,
		intake: NewMemoryLimiter(opts.MaxIntakeMemory),
		repository: wal.NewRepository(wal.RepositoryOpts{
			StorageDir:                opts.StorageDir,
			SegmentMaxSize:            opts.SegmentMaxSize,
			SegmentMaxAge:             opts.SegmentMaxAge,
			MaxDiskUsage:              opts.MaxDiskUsage,
			StartupOpenConcurrency:    opts.StartupOpenConcurrency,
			WALFlushInterval:          opts.WALFlushInterval,
			EnableWALFsync:            opts.EnableWALFsync,
			Policy:                    opts.Policy,
			Realtime:                  opts.Realtime,
			UnownedSegmentMaxAge:      opts.UnownedSegmentMaxAge,
			Owned:                     opts.Owned,
			RealtimeReservedDiskBytes: opts.RealtimeReservedDiskBytes,
			Admission:                 opts.Admission,
		}),
	}
}

func (s *LocalStore) Open(ctx context.Context) error {
	if err := s.repository.Open(ctx); err != nil {
		return err
	}
	if s.opts.Admission != nil && s.opts.MetricComponent != "" {
		s.unregisterBudget = metrics.RegisterTableBudget(s.opts.MetricComponent, s.opts.Admission)
	}
	if s.opts.MetricComponent != "" {
		s.unregisterIntake = metrics.RegisterIntakeMemory(s.opts.MetricComponent, s.intake)
	}
	return nil
}

func (s *LocalStore) Close() error {
	if s.unregisterBudget != nil {
		s.unregisterBudget()
	}
	if s.unregisterIntake != nil {
		s.unregisterIntake()
	}
	return s.repository.Close()
}

func (s *LocalStore) GetWAL(ctx context.Context, key []byte) (*wal.WAL, error) {
	return s.repository.Get(ctx, key)
}

// preAdmit rejects work for a destination that is already over budget, before
// the caller pays to encode it or to create a WAL for it. Returning early also
// keeps a rejected destination from accumulating WAL objects and goroutines.
func (s *LocalStore) preAdmit(prefix []byte) *wal.TableOverloadedError {
	if s.opts.Admission == nil {
		return nil
	}
	d, err := wal.DestinationFromPrefix(string(prefix))
	if err != nil {
		return nil
	}
	reason, throttled := s.opts.Admission.Throttled(d)
	if !throttled {
		return nil
	}
	return &wal.TableOverloadedError{Destination: d, Reason: reason, RetryAfter: 5 * time.Second}
}

// overBudget reports which of a request's tables might not fit in the store's global budget, or nil when they all
// fit.  It returns ErrMaxDiskUsageExceeded when the realtime tables do not fit, or when nothing fits, so the request
// is rejected and retried rather than any realtime data being shed.
//
// The check reserves nothing and does not count segments, so it makes partial writes rarer rather than impossible: a
// concurrent request can still use up the room it saw, or the segment limit can be reached, after some of the tables
// are written, and the sender's retry then writes those again.  When the realtime tables fit but the queued ones might
// not, the request is not rejected: the realtime tables are written first, and then each queued table is written if
// its actual size fits and shed if it does not, so the realtime reservation keeps realtime data flowing.  Queued
// tables are never shed on the estimate alone, which is an upper bound and can be many times their actual size.
//
// Realtime tables come first: they are checked against the whole budget using only their part of the request, so a
// large queued payload cannot block them.  The queued tables might not fit unless they fit in the queued share and
// also fit alongside the realtime tables.  size(i) is table i's estimated size from seriesEstimate.
//
// The estimate is an upper bound, so one larger than a whole limit would be rejected even by an empty store, and the
// sender's retries could never make progress.  Such an estimate is taken as the limit: it fits once the store is
// empty, and the write then checks the tables' actual size.
func (s *LocalStore) overBudget(tables int, size func(int) int64, key func(int) []byte) ([]bool, error) {
	a := s.opts.Admission
	if a == nil {
		return nil, nil
	}
	var total int64
	for i := range tables {
		total += size(i)
	}
	if a.FitsAll(total) {
		return nil, nil
	}

	const (
		noDestination = iota
		realtimeTable
		queuedTable
	)
	// Every realtime table shares one global limit and every queued table another, so any destination of a kind
	// stands for all of them.
	kinds := make([]int, tables)
	var realtime, queued int64
	var realtimeDest, queuedDest wal.Destination
	for i := range kinds {
		d, err := wal.DestinationFromPrefix(string(key(i)))
		switch {
		case err != nil:
		case a.UsesReservation(d):
			kinds[i], realtimeDest = realtimeTable, d
			realtime += size(i)
		default:
			kinds[i], queuedDest = queuedTable, d
			queued += size(i)
		}
	}
	if realtime > 0 && !a.FitsEstimate(realtimeDest, realtime) {
		return nil, wal.ErrMaxDiskUsageExceeded
	}
	queuedFits := a.FitsEstimate(queuedDest, queued) && (realtime == 0 || a.FitsEstimate(realtimeDest, realtime+queued))

	full := make([]bool, tables)
	anyFits := false
	for i, kind := range kinds {
		full[i] = kind == queuedTable && !queuedFits
		anyFits = anyFits || !full[i]
	}
	if !anyFits {
		return nil, wal.ErrMaxDiskUsageExceeded
	}
	return full, nil
}

// seriesEstimate is v's uncompressed CSV size, an upper bound on what its compressed WAL block reserves.
func seriesEstimate(req *prompb.WriteRequest, v *prompb.TimeSeries) int64 {
	var labels int64
	for l := range req.Labels(v) {
		labels += int64(len(l.Name) + len(l.Value) + 6)
	}
	return int64(len(v.Samples)) * (labels + 64)
}

func (s *LocalStore) WALCount() int {
	return s.repository.Count()
}

func (s *LocalStore) WriteTimeSeries(ctx context.Context, req *prompb.WriteRequest) error {
	var outcome WriteOutcome
	enc := metricsCSVWriterPool.Get(8 * 1024).(*transform2.MetricsCSVWriter)
	defer metricsCSVWriterPool.Put(enc)
	enc.InitColumns(s.opts.LiftedLabels)

	b := gbp.Get(256)
	defer gbp.Put(b)

	// Each table's series are written as one block rather than one per series, since every block is compressed,
	// checksummed, written under the segment lock and admitted separately.
	type tableSeries struct {
		key      []byte
		series   []*prompb.TimeSeries
		estimate int64
		// full is set for a queued table that might not fit alongside the rest of the request: it is the rejection
		// recorded when the table is shed, rather than the request rejected, because its write does not fit in the
		// global budget.
		full *wal.TableOverloadedError
	}
	var tables []*tableSeries
	byKey := make(map[string]int)
	for _, v := range req.Timeseries {
		key, err := SegmentKey(b[:0], req, v, enc.SchemaHash())
		if err != nil {
			return err
		}
		i, ok := byKey[string(key)]
		if !ok {
			i = len(tables)
			byKey[string(key)] = i
			tables = append(tables, &tableSeries{key: append([]byte(nil), key...)})
		}
		tables[i].series = append(tables[i].series, v)
		tables[i].estimate += seriesEstimate(req, v)
	}

	mayNotFit, err := s.overBudget(len(tables), func(i int) int64 { return tables[i].estimate }, func(i int) []byte { return tables[i].key })
	if err != nil {
		return err
	}
	if mayNotFit != nil {
		// Write the tables that fit first, so the queued tables that might not fit cannot take their room.
		var fit, rest []*tableSeries
		for i, t := range tables {
			if mayNotFit[i] {
				t.full = queuedBudgetFull(t.key)
				rest = append(rest, t)
			} else {
				fit = append(fit, t)
			}
		}
		tables = append(fit, rest...)
	}

	for _, t := range tables {
		var samples int64
		for _, v := range t.series {
			samples += int64(len(v.Samples))
		}

		if quota := s.preAdmit(t.key); quota != nil {
			s.recordRejection(quota, samples, 0, "metrics")
			outcome.Rejected += samples
			outcome.RejectedSeries = append(outcome.RejectedSeries, t.series...)
			continue
		}

		w, err := s.GetWAL(ctx, t.key)
		if err != nil {
			return err
		}

		enc.Reset()
		for _, v := range t.series {
			if err := enc.MarshalCSV(req, v); err != nil {
				return err
			}
		}

		s.inflightWriteBytes.Add(int64(len(enc.Bytes())))

		if s.diskUsageExceeded(w) {
			s.inflightWriteBytes.Add(-int64(len(enc.Bytes())))
			return wal.ErrMaxDiskUsageExceeded
		}

		if err := s.writeBlock(ctx, w, t.key, len(t.series), enc.Bytes()); err != nil {
			s.inflightWriteBytes.Add(-int64(len(enc.Bytes())))
			var quota *wal.TableOverloadedError
			if errors.Is(err, errBlockTooLarge) || errors.As(err, &quota) && blockTooLarge(quota, len(t.series)) {
				accepted, rejected, err := s.writeEachSeries(ctx, w, enc, req, t.series, t.full)
				outcome.Accepted += accepted
				metrics.SamplesStored.Add(float64(accepted))
				for _, v := range rejected {
					outcome.Rejected += int64(len(v.Samples))
				}
				outcome.RejectedSeries = append(outcome.RejectedSeries, rejected...)
				if err != nil {
					return err
				}
				continue
			}
			if quota == nil && t.full != nil && errors.Is(err, wal.ErrMaxDiskUsageExceeded) {
				quota = t.full
			}
			if quota != nil {
				s.recordRejection(quota, samples, int64(len(enc.Bytes())), "metrics")
				outcome.Rejected += samples
				outcome.RejectedSeries = append(outcome.RejectedSeries, t.series...)
				continue
			}
			return err
		}
		outcome.Accepted += samples
		metrics.SamplesStored.Add(float64(samples))
		// Decrement the inflight write bytes after writing to avoid double counting.
		s.inflightWriteBytes.Add(-int64(len(enc.Bytes())))
	}
	if outcome.Rejected > 0 {
		return &outcome
	}
	return nil
}

// blockTooLarge reports whether a table's block of n rows was rejected only because it is larger than the room left in
// the table's budget, so some of its rows written one at a time may still fit.
func blockTooLarge(quota *wal.TableOverloadedError, n int) bool {
	return quota.Reason == "bytes" && n > 1
}

// queuedBudgetFull is the rejection recorded for a queued table shed because the global budget's queued share is full.
func queuedBudgetFull(key []byte) *wal.TableOverloadedError {
	d, _ := wal.DestinationFromPrefix(string(key))
	return &wal.TableOverloadedError{Destination: d, Reason: "queued_budget"}
}

// errBlockTooLarge is returned by writeBlock for a block that was not written because it is larger than the room left
// in its table's budget.
var errBlockTooLarge = errors.New("block larger than the room left in its table's budget")

// writeBlock writes a table's block of n rows, unless it has more than one row and is larger than the room left in the
// table's budget, when it returns errBlockTooLarge so the caller writes the rows one at a time.  Checking first matters
// because a rejected write throttles the table until its usage drops well below the limit, and the throttle would then
// shed the rows that fit as well.  The block's uncompressed size is checked, which is larger than the compressed block
// the write reserves, so the check rarely lets through a block that is then rejected.
func (s *LocalStore) writeBlock(ctx context.Context, w *wal.WAL, key []byte, n int, block []byte) error {
	if n > 1 {
		if d, err := wal.DestinationFromPrefix(string(key)); err == nil && !s.opts.Admission.FitsTable(d, int64(len(block))) {
			return errBlockTooLarge
		}
	}
	return w.Write(ctx, block)
}

// writeEachSeries writes a table's series one block per series, for a table whose block of every series did not fit,
// so the series that fit are written and only the rest are shed, as before a table's series were written together.  It
// returns the samples written and the series shed.  When full is not nil, series that do not fit in the global budget
// are shed as well and recorded as full, rather than stopping with ErrMaxDiskUsageExceeded.
func (s *LocalStore) writeEachSeries(ctx context.Context, w *wal.WAL, enc *transform2.MetricsCSVWriter, req *prompb.WriteRequest, series []*prompb.TimeSeries, full *wal.TableOverloadedError) (int64, []*prompb.TimeSeries, error) {
	var accepted int64
	var rejected []*prompb.TimeSeries
	for _, v := range series {
		enc.Reset()
		if err := enc.MarshalCSV(req, v); err != nil {
			return accepted, rejected, err
		}
		if err := w.Write(ctx, enc.Bytes()); err != nil {
			var quota *wal.TableOverloadedError
			if !errors.As(err, &quota) {
				if full == nil || !errors.Is(err, wal.ErrMaxDiskUsageExceeded) {
					return accepted, rejected, err
				}
				quota = full
			}
			s.recordRejection(quota, int64(len(v.Samples)), int64(len(enc.Bytes())), "metrics")
			rejected = append(rejected, v)
			continue
		}
		accepted += int64(len(v.Samples))
	}
	return accepted, rejected, nil
}

// writeEachLog is writeEachSeries for logs.  It returns the logs written and shed, and on an error the logs from the
// one that failed on, which were not written.
func (s *LocalStore) writeEachLog(ctx context.Context, w *wal.WAL, enc *transform2.NativeLogsCSVWriter, logs *types.LogBatch, idx []int) (int64, int64, []int, error) {
	var accepted, rejected int64
	for k, j := range idx {
		enc.Reset()
		if err := enc.MarshalNativeLog(logs.Logs[j]); err != nil {
			return accepted, rejected, append([]int(nil), idx[k:]...), err
		}
		if err := w.Write(ctx, enc.Bytes()); err != nil {
			var quota *wal.TableOverloadedError
			if !errors.As(err, &quota) {
				return accepted, rejected, append([]int(nil), idx[k:]...), err
			}
			s.recordRejection(quota, 1, int64(len(enc.Bytes())), "logs")
			rejected++
			continue
		}
		accepted++
	}
	return accepted, rejected, nil, nil
}

func (s *LocalStore) WriteOTLPLogs(ctx context.Context, database, table string, logs *otlp.Logs) error {
	sanitizedDB := schema.NormalizeAdxIdentifier(database)
	sanitizedTable := schema.NormalizeAdxIdentifier(table)

	if sanitizedDB == "" || sanitizedTable == "" {
		logger.Warnf("Invalid database or table name: %s.%s", database, table)
		return nil // Do not retry - move on
	}

	enc := csvWriterPool.Get(8 * 1024).(*transform2.CSVWriter)
	defer csvWriterPool.Put(enc)

	key := gbp.Get(256)
	defer gbp.Put(key)

	if logger.IsDebug() {
		logger.Debugf("Store received %d logs for %s.%s", len(logs.Logs), sanitizedDB, sanitizedTable)
	}

	key = fmt.Appendf(key[:0], "%s_%s", sanitizedDB, sanitizedTable)

	if quota := s.preAdmit(key); quota != nil {
		s.recordRejection(quota, int64(len(logs.Logs)), 0, "logs")
		return &WriteOutcome{Rejected: int64(len(logs.Logs))}
	}

	w, err := s.GetWAL(ctx, key)
	if err != nil {
		return err
	}

	enc.Reset()
	if err := enc.MarshalLog(logs); err != nil {
		return err
	}

	s.inflightWriteBytes.Add(int64(len(enc.Bytes())))
	defer s.inflightWriteBytes.Add(-int64(len(enc.Bytes())))

	if s.diskUsageExceeded(w) {
		return wal.ErrMaxDiskUsageExceeded
	}

	wo := wal.WithSampleMetadata(wal.LogSampleType, uint32(len(logs.Logs)))
	if err := w.Write(ctx, enc.Bytes(), wo); err != nil {
		if quota, ok := errors.AsType[*wal.TableOverloadedError](err); ok {
			s.recordRejection(quota, int64(len(logs.Logs)), int64(len(enc.Bytes())), "logs")
			return &WriteOutcome{Rejected: int64(len(logs.Logs))}
		}
		return err
	}

	return nil
}

func (s *LocalStore) WriteNativeLogs(ctx context.Context, logs *types.LogBatch) error {
	var outcome WriteOutcome
	enc := nativeLogsCSVWriterPool.Get(8 * 1024).(*transform2.NativeLogsCSVWriter)
	defer nativeLogsCSVWriterPool.Put(enc)
	enc.InitColumns(s.opts.LiftedResources)

	key := gbp.Get(256)
	defer gbp.Put(key)

	if logger.IsDebug() {
		logger.Debugf("Store received %d native logs", len(logs.Logs))
	}

	noDestinationCount := 0

	// Each log can potentially have a different destination.
	// Instead of splitting ahead of time and allocating n slices, just encode and write to the wal on
	// a per-log basis.
	// Each table's logs are written as one block rather than one per log, since every block is compressed,
	// checksummed, written under the segment lock and admitted separately.
	type tableLogs struct {
		key  []byte
		logs []int
	}
	var tables []*tableLogs
	byKey := make(map[string]int)
	for i, log := range logs.Logs {
		// If we don't have a destination, we can't do anything with the log.
		// Skip instead of trying again, which will just repeat the same error.
		database := types.StringOrEmpty(log.GetAttributeValue(types.AttributeDatabaseName))
		if database == "" {
			noDestinationCount++
			continue
		}
		sanitizedDB := schema.NormalizeAdxIdentifier(database)
		if sanitizedDB == "" {
			noDestinationCount++
			continue
		}

		table := types.StringOrEmpty(log.GetAttributeValue(types.AttributeTableName))
		if table == "" {
			noDestinationCount++
			continue
		}
		sanitizedTable := schema.NormalizeAdxIdentifier(table)
		if sanitizedTable == "" {
			noDestinationCount++
			continue
		}

		key = fmt.Appendf(key[:0], "%s_%s_", sanitizedDB, sanitizedTable)
		key = strconv.AppendUint(key, enc.SchemaHash(), 36)
		j, ok := byKey[string(key)]
		if !ok {
			j = len(tables)
			byKey[string(key)] = j
			tables = append(tables, &tableLogs{key: append([]byte(nil), key...)})
		}
		tables[j].logs = append(tables[j].logs, i)
	}

	// pending returns the logs of the tables from i on, which were not written.
	pending := func(i int) []int {
		var p []int
		for _, t := range tables[i:] {
			p = append(p, t.logs...)
		}
		return p
	}

	for i, t := range tables {
		n := int64(len(t.logs))
		if quota := s.preAdmit(t.key); quota != nil {
			s.recordRejection(quota, n, 0, "logs")
			outcome.Rejected += n
			continue
		}

		w, err := s.GetWAL(ctx, t.key)
		if err != nil {
			return err
		}

		enc.Reset()
		for _, j := range t.logs {
			if err := enc.MarshalNativeLog(logs.Logs[j]); err != nil {
				return err
			}
		}
		s.inflightWriteBytes.Add(int64(len(enc.Bytes())))

		if s.diskUsageExceeded(w) {
			s.inflightWriteBytes.Add(-int64(len(enc.Bytes())))
			return &PartialWriteError{Pending: pending(i), Err: wal.ErrMaxDiskUsageExceeded}
		}

		if err := s.writeBlock(ctx, w, t.key, len(t.logs), enc.Bytes()); err != nil {
			s.inflightWriteBytes.Add(-int64(len(enc.Bytes())))
			var quota *wal.TableOverloadedError
			if errors.Is(err, errBlockTooLarge) || errors.As(err, &quota) && blockTooLarge(quota, len(t.logs)) {
				accepted, rejected, unwritten, err := s.writeEachLog(ctx, w, enc, logs, t.logs)
				outcome.Accepted += accepted
				outcome.Rejected += rejected
				if err != nil {
					if errors.Is(err, wal.ErrMaxDiskUsageExceeded) || errors.Is(err, wal.ErrMaxSegmentsExceeded) {
						return &PartialWriteError{Pending: append(unwritten, pending(i+1)...), Err: err}
					}
					return err
				}
				continue
			}
			if quota != nil {
				s.recordRejection(quota, n, int64(len(enc.Bytes())), "logs")
				outcome.Rejected += n
				continue
			}
			if errors.Is(err, wal.ErrMaxDiskUsageExceeded) || errors.Is(err, wal.ErrMaxSegmentsExceeded) {
				return &PartialWriteError{Pending: pending(i), Err: err}
			}
			return err
		}
		outcome.Accepted += n

		// Decrement the inflight write bytes after writing to avoid double counting.
		s.inflightWriteBytes.Add(-int64(len(enc.Bytes())))
	}

	if noDestinationCount > 0 {
		logger.Warnf("Got %d logs without ADX destinations - dropped", noDestinationCount)
		metrics.InvalidLogsDropped.WithLabelValues().Add(float64(noDestinationCount))
	}

	if outcome.Rejected > 0 {
		return &outcome
	}
	return nil
}

func (s *LocalStore) PrefixesByAge() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.repository.PrefixesByAge()
}

func (s *LocalStore) Import(filename string, body io.ReadCloser) (int, error) {
	db, table, schema, _, err := wal.ParseFilename(filename)
	if err != nil {
		return 0, err
	}

	limit := s.opts.MaxDiskUsage
	tableScoped := false
	// In observe-only mode the table budget is reported, not enforced, so it
	// must not cap the body: rejecting as oversized would make the sender
	// drop the segment.
	if s.opts.Admission.Enforced() {
		if tableLimit := s.opts.Admission.Limit(wal.Destination{Database: db, Table: table}).Bytes; tableLimit > 0 && (limit <= 0 || tableLimit <= limit) {
			limit = tableLimit
			tableScoped = true
		}
	}
	var input io.Reader = body
	if limit > 0 {
		input = io.LimitReader(body, limit+1)
	}

	// The body is buffered in full before it can be admitted, so memory is
	// claimed as the buffer grows rather than reserving the destination's whole
	// budget, which could exceed the ceiling for a small payload.
	buf, err := readIntake(input, s.intake)
	defer buf.Release()
	if err != nil {
		return 0, err
	}
	n := int64(len(buf.Bytes()))
	if limit > 0 && n > limit {
		if tableScoped {
			// Larger than the destination's entire budget: draining cannot make
			// room, so report it as permanent for this payload shape.
			return 0, &wal.SegmentTooLargeError{Destination: wal.Destination{Database: db, Table: table}, Size: n, Limit: limit}
		}
		return 0, wal.ErrMaxDiskUsageExceeded
	}

	key := gbp.Get(256)
	defer gbp.Put(key)

	if schema != "" {
		key = fmt.Appendf(key[:0], "%s_%s_%s", db, table, schema)
	} else {
		key = fmt.Appendf(key[:0], "%s_%s", db, table)
	}

	w, err := s.GetWAL(context.Background(), key)
	if err != nil {
		return 0, err
	}

	s.inflightWriteBytes.Add(n)
	defer s.inflightWriteBytes.Add(-n)

	if s.diskUsageExceeded(w) {
		return 0, wal.ErrMaxDiskUsageExceeded
	}

	return int(n), w.Append(context.Background(), buf.Bytes())
}

func (s *LocalStore) Remove(path string) error {
	db, table, schema, _, err := wal.ParseFilename(path)
	if err != nil {
		return err
	}

	var key string
	if schema != "" {
		key = fmt.Sprintf("%s_%s_%s", db, table, schema)
	} else {
		key = fmt.Sprintf("%s_%s", db, table)
	}

	wal, err := s.GetWAL(context.Background(), []byte(key))
	if err != nil {
		return err
	}
	return wal.Remove(path)
}

func (s *LocalStore) Index() *wal.Index {
	return s.repository.Index()
}

// Size returns the total size of the LocalStore, including the size of the repository (active WAL segments),
// inflight write bytes, and index size (closed WAL segments).
// diskUsageExceeded returns true if writes to w must be rejected because disk usage reached the limit for w's
// ingestion priority.
// diskUsageExceeded reports whether the aggregate disk limit is reached.
// Admission owns capacity when enabled, including the global ceiling, so the
// legacy check is skipped to avoid two systems rejecting on the same budget.
func (s *LocalStore) diskUsageExceeded(w *wal.WAL) bool {
	if s.opts.Admission != nil {
		return false
	}
	return s.Size() > s.repository.MaxDiskUsage(w.Priority())
}

func (s *LocalStore) Size() int64 {
	if s.opts.Admission != nil {
		return s.opts.Admission.Usage().Bytes
	}
	return s.repository.Size() + s.inflightWriteBytes.Load() + s.Index().TotalSize()
}

func (s *LocalStore) SegmentCount() int64 {
	if s.opts.Admission != nil {
		return s.opts.Admission.Usage().Segments
	}
	return int64(s.Index().TotalSegments())
}

// TracksSegment returns true if path is a WAL segment file the store accounts for, either a closed segment pending
// upload or an active segment still being written.  A segment file on disk that is not tracked is orphaned: it is
// never uploaded or removed until the store is reopened.
func (s *LocalStore) TracksSegment(path string) bool {
	if s.opts.Admission != nil {
		return s.opts.Admission.Tracks(path)
	}
	return s.Index().Contains(path) || s.repository.IsActiveSegment(path)
}

// WriteDebug writes debug information to the given writer.
func (s *LocalStore) WriteDebug(w io.Writer) error {
	if s.opts.Admission != nil {
		for d, status := range s.opts.Admission.Status() {
			fmt.Fprintf(w, "Table %s: bytes=%d/%d segments=%d/%d throttled=%t rejected=%d\n", d, status.Bytes, status.Limit.Bytes, status.Segments, status.Limit.Segments, status.Throttled, status.RejectedRecords)
		}
	}
	if err := s.repository.WriteDebug(w); err != nil {
		return err
	}

	w.Write([]byte("\nSegments On Disk:\n"))
	tw := tabwriter.NewWriter(w, 0, 8, 1, ' ', 0)

	tw.Write([]byte("Path\tSize\tCreatedAt\n"))
	var totalSize, totalSegments int64
	if err := filepath.Walk(s.opts.StorageDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			fmt.Fprintf(tw, "%s\t%d\t%s\n", path, info.Size(), info.ModTime().Format(time.RFC3339))
			totalSize += info.Size()
			totalSegments += 1
		}
		return nil
	}); err != nil {
		return err
	}
	fmt.Fprintf(w, "Actual: Disk Usage: %d, Segments: %d\n", totalSize, totalSegments)
	tw.Flush()

	return nil
}

func (s *LocalStore) recordRejection(quota *wal.TableOverloadedError, records, bytes int64, kind string) {
	if s.opts.Admission.RecordRejection(quota.Destination, records, bytes) {
		logger.Warnf("Shedding table quota data: table=%s reason=%s records=%d encoded_bytes=%d", quota.Destination, quota.Reason, records, bytes)
	}
	metrics.TableQuotaRejectedRecords.WithLabelValues(s.opts.MetricComponent, kind, quota.Reason).Add(float64(records))
	metrics.TableQuotaRejectedBytes.WithLabelValues(s.opts.MetricComponent, kind, quota.Reason).Add(float64(bytes))
}

// SegmentKey returns the segment key for series using its effective labels
// from req, including request-level common labels and the per-series filter.
func SegmentKey(dst []byte, req *prompb.WriteRequest, series *prompb.TimeSeries, hash uint64) ([]byte, error) {
	var name, database []byte
	for v := range req.Labels(series) {
		if bytes.Equal(v.Name, []byte("adxmon_database")) {
			database = v.Value
			if len(name) > 0 {
				break
			}
			continue
		}

		if bytes.Equal(v.Name, []byte("__name__")) {
			name = v.Value
			if len(database) > 0 {
				break
			}
		}
	}

	if len(database) == 0 {
		return nil, fmt.Errorf("database label not found")
	}

	if len(name) == 0 {
		return nil, fmt.Errorf("name label not found")
	}

	dst = schema.AppendNormalizeAdxIdentifier(dst, database)
	dst = append(dst, delim...)
	dst = schema.AppendNormalizeMetricName(dst, name)
	dst = append(dst, delim...)
	return strconv.AppendUint(dst, hash, 36), nil
}

var delim = []byte("_")
