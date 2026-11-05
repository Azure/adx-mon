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
	csvWriterPool = pool.NewGeneric(1000, func(sz int) interface{} {
		return transform2.NewCSVWriter(bytes.NewBuffer(make([]byte, 0, sz)), nil)
	})

	nativeLogsCSVWriterPool = pool.NewGeneric(1000, func(sz int) interface{} {
		return transform2.NewCSVNativeLogsCSVWriter(bytes.NewBuffer(make([]byte, 0, sz)), nil)
	})

	metricsCSVWriterPool = pool.NewGeneric(1000, func(sz int) interface{} {
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

	// ImportWith is like Import, but first offers the buffered file to forward, if set, and does not store it when
	// forward reports that it was accepted elsewhere.
	ImportWith(filename string, body io.ReadCloser, forward func(data []byte) bool) (int, error)
}

// LocalStore provides local storage of time series data.  It manages Write Ahead Logs (WALs) for each metric.
type LocalStore struct {
	opts StoreOpts

	mu         sync.RWMutex
	repository *wal.Repository

	inflightWriteBytes int64
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
// fit.  It returns ErrMaxDiskUsageExceeded when none fit, so the request is rejected before any series is written and
// the sender's retry of the whole request does not duplicate them.  When only the queued share of the budget is used
// up, the realtime tables fit and the queued ones are shed, so the realtime reservation keeps realtime data flowing.
//
// The queued tables are checked against the queued share using only the queued part of the request, and the realtime
// tables against the whole budget using only the part that is kept, so a large queued payload that is shed cannot
// block a realtime one.  size(i) is table i's estimated size from seriesEstimate.
func (s *LocalStore) overBudget(tables int, size func(int) int64, key func(int) []byte) ([]bool, error) {
	a := s.opts.Admission
	if a == nil {
		return nil, nil
	}
	var total int64
	for i := 0; i < tables; i++ {
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
	queuedFits := a.Fits(queuedDest, queued)
	if queuedFits {
		realtime += queued
	}
	realtimeFits := a.Fits(realtimeDest, realtime)

	full := make([]bool, tables)
	fits := false
	for i, kind := range kinds {
		full[i] = (kind == realtimeTable && !realtimeFits) || (kind == queuedTable && !queuedFits)
		fits = fits || !full[i]
	}
	if !fits {
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

	full, err := s.overBudget(len(tables), func(i int) int64 { return tables[i].estimate }, func(i int) []byte { return tables[i].key })
	if err != nil {
		return err
	}

	for i, t := range tables {
		var samples int64
		for _, v := range t.series {
			samples += int64(len(v.Samples))
		}

		if full != nil && full[i] {
			d, _ := wal.DestinationFromPrefix(string(t.key))
			s.recordRejection(&wal.TableOverloadedError{Destination: d, Reason: "queued_budget"}, samples, 0, "metrics")
			outcome.Rejected += samples
			outcome.RejectedSeries = append(outcome.RejectedSeries, t.series...)
			continue
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

		atomic.AddInt64(&s.inflightWriteBytes, int64(len(enc.Bytes())))

		if s.diskUsageExceeded(w) {
			atomic.AddInt64(&s.inflightWriteBytes, -int64(len(enc.Bytes())))
			return wal.ErrMaxDiskUsageExceeded
		}

		if err := w.Write(ctx, enc.Bytes()); err != nil {
			atomic.AddInt64(&s.inflightWriteBytes, -int64(len(enc.Bytes())))
			var quota *wal.TableOverloadedError
			if errors.As(err, &quota) {
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
		atomic.AddInt64(&s.inflightWriteBytes, -int64(len(enc.Bytes())))
	}
	if outcome.Rejected > 0 {
		return &outcome
	}
	return nil
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

	atomic.AddInt64(&s.inflightWriteBytes, int64(len(enc.Bytes())))
	defer atomic.AddInt64(&s.inflightWriteBytes, -int64(len(enc.Bytes())))

	if s.diskUsageExceeded(w) {
		return wal.ErrMaxDiskUsageExceeded
	}

	wo := wal.WithSampleMetadata(wal.LogSampleType, uint32(len(logs.Logs)))
	if err := w.Write(ctx, enc.Bytes(), wo); err != nil {
		var quota *wal.TableOverloadedError
		if errors.As(err, &quota) {
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
		atomic.AddInt64(&s.inflightWriteBytes, int64(len(enc.Bytes())))

		if s.diskUsageExceeded(w) {
			atomic.AddInt64(&s.inflightWriteBytes, -int64(len(enc.Bytes())))
			return &PartialWriteError{Pending: pending(i), Err: wal.ErrMaxDiskUsageExceeded}
		}

		if err := w.Write(ctx, enc.Bytes()); err != nil {
			atomic.AddInt64(&s.inflightWriteBytes, -int64(len(enc.Bytes())))
			var quota *wal.TableOverloadedError
			if errors.As(err, &quota) {
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
		atomic.AddInt64(&s.inflightWriteBytes, -int64(len(enc.Bytes())))
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
	return s.ImportWith(filename, body, nil)
}

func (s *LocalStore) ImportWith(filename string, body io.ReadCloser, forward func(data []byte) bool) (int, error) {
	db, table, schema, _, err := wal.ParseFilename(filename)
	if err != nil {
		return 0, err
	}

	buf := pool.BytesBufferPool.Get(512 * 1024).(*gbp.Buffer)
	defer pool.BytesBufferPool.Put(buf)
	buf.Reset()

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
	// claimed as it is read rather than reserving the destination's whole
	// budget, which could exceed the ceiling for a small payload.
	intake := &intakeReader{r: input, m: s.intake}
	defer intake.Release()

	n, err := io.Copy(buf, intake)
	if err != nil {
		return 0, err
	}
	if limit > 0 && n > limit {
		if tableScoped {
			// Larger than the destination's entire budget: draining cannot make
			// room, so report it as permanent for this payload shape.
			return 0, &wal.SegmentTooLargeError{Destination: wal.Destination{Database: db, Table: table}, Size: n, Limit: limit}
		}
		return 0, wal.ErrMaxDiskUsageExceeded
	}

	if forward != nil && forward(buf.Bytes()) {
		return int(n), nil
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

	atomic.AddInt64(&s.inflightWriteBytes, n)
	defer atomic.AddInt64(&s.inflightWriteBytes, -n)

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
	return s.repository.Size() + atomic.LoadInt64(&s.inflightWriteBytes) + s.Index().TotalSize()
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
			tw.Write([]byte(fmt.Sprintf("%s\t%d\t%s\n", path, info.Size(), info.ModTime().Format(time.RFC3339))))
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
