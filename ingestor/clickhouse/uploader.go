package clickhouse

import (
	"bufio"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"

	"github.com/Azure/adx-mon/ingestor/cluster"
	monlogger "github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/service"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/adx-mon/schema"
)

// batchUploadTimeout bounds a single batch's upload.  It matches the ADX
// uploader's batchUploadTimeout so a slow or wedged ClickHouse connection
// (PrepareInsert, Append, or Send) releases the batch for retry instead of
// blocking its worker for the service lifetime.
var batchUploadTimeout = 10 * time.Minute

// schemaReconcileInterval is how often the uploader checks its schema cache
// against the tables that still exist in ClickHouse.  It mirrors the ADX
// syncer's reconcileInterval so a table dropped from ClickHouse does not keep
// its cached Schema in memory indefinitely.
var schemaReconcileInterval = 15 * time.Minute

// Uploader exposes the contract shared by all storage backends so that the
// ingestor service can remain agnostic of the implementation details.
type Uploader interface {
	service.Component
	UploadQueue() chan *cluster.Batch
	Database() string
	DSN() string
}

type uploader struct {
	cfg     Config
	log     *slog.Logger
	queue   chan *cluster.Batch
	jobs    <-chan *cluster.Batch
	schemas map[string]Schema
	conn    connectionManager
	syncer  *syncer

	mu       sync.RWMutex
	schemaMu sync.RWMutex
	// Per-table leases serialize schema creation with eviction without holding
	// the cache-wide lock across ClickHouse I/O.
	schemaLockMu sync.Mutex
	schemaLocks  map[string]*schemaTableLock
	cancel       context.CancelFunc
	ctx          context.Context
	open         bool

	workers int
	wg      sync.WaitGroup

	// schemaReconcileWG tracks the schema-cache reconcile goroutine.  It is
	// separate from wg (the workers) so Close can join it deterministically
	// without entangling the upload workers' lifecycle.
	schemaReconcileWG sync.WaitGroup
}

type schemaTableLock struct {
	sync.Mutex
	users int
}

func (u *uploader) lockSchemaTable(table string) func() {
	u.schemaLockMu.Lock()
	if u.schemaLocks == nil {
		u.schemaLocks = make(map[string]*schemaTableLock)
	}
	l := u.schemaLocks[table]
	if l == nil {
		l = &schemaTableLock{}
		u.schemaLocks[table] = l
	}
	l.users++ // Count waiters before releasing the map lock.
	u.schemaLockMu.Unlock()
	l.Lock()
	return func() {
		l.Unlock()
		u.schemaLockMu.Lock()
		l.users--
		if l.users == 0 {
			delete(u.schemaLocks, table)
		}
		u.schemaLockMu.Unlock()
	}
}

// NewUploader constructs a ClickHouse-backed uploader
func NewUploader(cfg Config, log *slog.Logger) (Uploader, error) {
	cfg = cfg.withDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if log == nil {
		log = monlogger.Logger()
	}
	log = log.With(slog.String("component", "clickhouse-uploader"))

	conn, err := newConnectionManager(cfg)
	if err != nil {
		return nil, err
	}

	return &uploader{
		cfg:     cfg,
		log:     log,
		queue:   make(chan *cluster.Batch, cfg.QueueCapacity),
		schemas: DefaultSchemas(),
		conn:    conn,
		syncer:  newSyncer(cfg.Database, conn, log),
	}, nil
}

func (u *uploader) Open(ctx context.Context) error {
	u.mu.Lock()
	defer u.mu.Unlock()

	if u.open {
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	u.ctx = ctx
	u.cancel = cancel
	u.open = true
	if err := u.conn.Ping(ctx); err != nil {
		u.open = false
		cancel()
		return err
	}

	if err := u.conn.Exec(ctx, "SELECT 1"); err != nil {
		u.open = false
		cancel()
		return fmt.Errorf("clickhouse health probe failed: %w", err)
	}

	if err := u.syncer.EnsureInitial(ctx, u.Schemas()); err != nil {
		u.open = false
		cancel()
		return err
	}

	workers := max(runtime.NumCPU(), 1)
	u.workers = workers
	u.jobs = cluster.FairQueue(ctx, u.queue, cluster.FairQueueOpts{Workers: workers, Pipeline: "clickhouse:" + u.cfg.Database})

	for i := range workers {
		u.wg.Add(1)
		go u.worker(i)
	}

	u.schemaReconcileWG.Add(1)
	go u.schemaReconcileLoop()

	u.log.Info("clickhouse uploader ready")

	return nil
}

func (u *uploader) Close() error {
	u.mu.Lock()
	if !u.open {
		u.mu.Unlock()
		return nil
	}

	cancel := u.cancel
	u.open = false
	u.mu.Unlock()

	cancel()
	u.wg.Wait()
	u.schemaReconcileWG.Wait()

	if err := u.conn.Close(); err != nil {
		u.log.Warn("failed to close clickhouse connection", slog.String("error", err.Error()))
	}
	u.log.Info("clickhouse uploader stopped")
	return nil
}

func (u *uploader) UploadQueue() chan *cluster.Batch {
	return u.queue
}

func (u *uploader) Database() string {
	return u.cfg.Database
}

func (u *uploader) DSN() string {
	return u.cfg.DSN
}

// Schemas exposes the tables known to the uploader.
func (u *uploader) Schemas() map[string]Schema {
	u.schemaMu.RLock()
	defer u.schemaMu.RUnlock()

	cp := make(map[string]Schema, len(u.schemas))
	for k, v := range u.schemas {
		cols := make([]Column, len(v.Columns))
		copy(cols, v.Columns)
		cp[k] = Schema{Table: v.Table, Columns: cols}
	}
	return cp
}

// schemaReconcileLoop periodically evicts schema-cache entries for tables that
// no longer exist in ClickHouse.  It runs for the lifetime of an open uploader
// and exits once the service context is canceled.
func (u *uploader) schemaReconcileLoop() {
	defer u.schemaReconcileWG.Done()

	t := time.NewTicker(schemaReconcileInterval)
	defer t.Stop()

	for {
		select {
		case <-u.ctx.Done():
			return
		case <-t.C:
			u.reconcileSchemas(u.ctx)
		}
	}
}

// reconcileSchemas lists the tables that still exist in the uploader's database
// and evicts any cached schemas whose table has been dropped.
func (u *uploader) reconcileSchemas(ctx context.Context) {
	existing, err := u.listTables(ctx)
	if err != nil {
		if u.log != nil {
			u.log.Warn("schema reconcile: list tables failed", slog.String("error", err.Error()))
		}
		return
	}
	u.evictSchemasForDroppedTables(existing)
}

// listTables returns the set of table names present in the uploader's database.
// It queries ClickHouse's system.tables catalog for the configured database.
type catalogTableName struct {
	Name string `ch:"name"`
}

func (u *uploader) listTables(ctx context.Context) (map[string]struct{}, error) {
	query := "SELECT name FROM system.tables WHERE database = " + quoteLiteral(u.cfg.Database)
	var names []catalogTableName
	if err := u.conn.Select(ctx, &names, query); err != nil {
		return nil, err
	}
	set := make(map[string]struct{}, len(names))
	for _, row := range names {
		set[row.Name] = struct{}{}
	}
	return set, nil
}

// evictSchemasForDroppedTables removes schemas and syncer entries for tables
// absent from the catalog. Per-table locks coordinate eviction with creation;
// the cache-wide locks are never held across ClickHouse I/O.
func (u *uploader) evictSchemasForDroppedTables(existingTables map[string]struct{}) {
	missing := make(map[string]struct{})
	u.schemaMu.RLock()
	for _, schema := range u.schemas {
		if _, ok := existingTables[schema.Table]; !ok {
			missing[schema.Table] = struct{}{}
		}
	}
	u.schemaMu.RUnlock()
	u.syncer.mu.Lock()
	for key := range u.syncer.ensured {
		table, _, _ := strings.Cut(key, "@")
		if _, ok := existingTables[table]; !ok {
			missing[table] = struct{}{}
		}
	}
	u.syncer.mu.Unlock()

	for table := range missing {
		unlock := u.lockSchemaTable(table)
		u.schemaMu.Lock()
		for key, schema := range u.schemas {
			if schema.Table == table {
				delete(u.schemas, key)
			}
		}
		u.syncer.mu.Lock()
		for key := range u.syncer.ensured {
			name, _, _ := strings.Cut(key, "@")
			if name == table {
				delete(u.syncer.ensured, key)
			}
		}
		u.syncer.mu.Unlock()
		u.schemaMu.Unlock()
		if u.log != nil {
			u.log.Debug("evicting cached schema for dropped table", slog.String("table", table))
		}
		unlock()
	}
}

func (u *uploader) worker(_ int) {
	defer u.wg.Done()

	for {
		select {
		case <-u.ctx.Done():
			return
		case batch, ok := <-u.jobs:
			if !ok {
				return
			}
			if batch == nil {
				continue
			}
			if !u.runBatch(batch) {
				return
			}
		}
	}
}

// runBatch returns false when shutdown won the race with a queue hand-off.
func (u *uploader) runBatch(batch *cluster.Batch) bool {
	if u.ctx.Err() != nil {
		batch.Release()
		return false
	}
	// Bound each batch's upload.  Derive from the service context without
	// inheriting its cancellation so one slow batch's timeout neither
	// cancels the worker's exit path nor a sibling batch, and shutdown
	// (Close) does not cancel an in-flight batch's bounded context.
	uploadCtx, cancel := context.WithTimeout(context.WithoutCancel(u.ctx), batchUploadTimeout)
	err := u.processBatch(uploadCtx, batch)
	cancel()
	if err != nil && u.log != nil {
		u.log.Error("failed to upload batch", slog.String("prefix", batch.Prefix), slog.String("error", err.Error()))
	}
	return true
}

func (u *uploader) processBatch(ctx context.Context, batch *cluster.Batch) error {
	defer batch.Release()

	if batch.Database != u.cfg.Database {
		if u.log != nil {
			u.log.Error("clickhouse batch database mismatch", slog.String("expected", u.cfg.Database), slog.String("actual", batch.Database))
		}
		return nil
	}

	stats := newUploadStats(batch.Database)
	var totalRows int
	defer func() {
		stats.setRows(totalRows)
		stats.observe()
	}()

	handleError := func(err error, retryable bool) error {
		if retryable {
			batch.RetryAfter(5 * time.Second)
		}
		if !retryable {
			if remErr := batch.Remove(); remErr != nil {
				if u.log != nil {
					u.log.Warn("failed to remove fatal batch", slog.String("database", batch.Database), slog.String("error", remErr.Error()))
				}
			}
		}
		return stats.error(err, retryable)
	}

	var (
		writer       batchWriter
		schemaDef    Schema
		schemaLoaded bool
		schemaID     string
		headerSig    string
		rowsInBatch  int
		processed    int
		flushTimer   *time.Timer
	)

	defer func() {
		if flushTimer != nil {
			flushTimer.Stop()
		}
		if writer != nil {
			_ = writer.Close()
		}
	}()

	flushInterval := u.cfg.Batch.FlushInterval

	for _, segment := range batch.Segments {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		sr, err := wal.NewSegmentReader(segment.Path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			if u.log != nil {
				u.log.Error("failed to open segment", slog.String("path", segment.Path), slog.String("error", err.Error()))
			}
			return handleError(err, false)
		}

		processed++
		stats.addBytes(segment.Size)

		bufReader := bufio.NewReader(sr)
		header, err := readHeader(bufReader)
		if err != nil {
			sr.Close()
			if u.log != nil {
				u.log.Error("failed to read segment header", slog.String("path", segment.Path), slog.String("error", err.Error()))
			}
			return handleError(err, false)
		}

		if headerSig == "" {
			headerSig = header
			if u.log != nil {
				u.log.Info("loaded WAL segment header",
					slog.String("table", batch.Table),
					slog.String("path", segment.Path),
					slog.String("header_signature", header))
			}
			_, _, schemaName, _, err := wal.ParseFilename(segment.Path)
			if err != nil {
				sr.Close()
				if u.log != nil {
					u.log.Error("failed to parse segment filename", slog.String("path", segment.Path), slog.String("error", err.Error()))
				}
				return handleError(err, false)
			}

			schemaID = schemaName
			mapping, err := schema.UnmarshalSchema(header)
			if err != nil {
				sr.Close()
				if u.log != nil {
					u.log.Error("failed to unmarshal schema", slog.String("path", segment.Path), slog.String("error", err.Error()))
				}
				return handleError(err, false)
			}
			if len(mapping) == 0 {
				sr.Close()
				return handleError(fmt.Errorf("empty schema for table %s", batch.Table), false)
			}

			schemaDef, err = u.ensureSchema(ctx, batch.Table, schemaID, mapping)
			if err != nil {
				sr.Close()
				if u.log != nil {
					u.log.Error("failed to ensure schema", slog.String("table", batch.Table), slog.String("error", err.Error()))
				}
				return handleError(err, true)
			}

			if u.log != nil {
				columnSpecs := make([]string, 0, len(schemaDef.Columns))
				for _, col := range schemaDef.Columns {
					columnSpecs = append(columnSpecs, fmt.Sprintf("%s:%s", col.Name, col.Type))
				}
				u.log.Info("prepared ClickHouse schema",
					slog.String("table", batch.Table),
					slog.String("schema_id", schemaID),
					slog.Any("columns", columnSpecs))
			}

			writer, err = u.conn.PrepareInsert(ctx, batch.Database, batch.Table, schemaDef.Columns)
			if err != nil {
				sr.Close()
				if u.log != nil {
					u.log.Error("failed to prepare clickhouse batch", slog.String("table", batch.Table), slog.String("error", err.Error()))
				}
				return handleError(err, true)
			}

			schemaLoaded = true

			if flushInterval > 0 {
				flushTimer = time.NewTimer(flushInterval)
			}
		} else if headerSig != header {
			sr.Close()
			return handleError(fmt.Errorf("schema mismatch for table %s", batch.Table), false)
		}

		csvReader := csv.NewReader(bufReader)
		csvReader.ReuseRecord = true
		csvReader.FieldsPerRecord = len(schemaDef.Columns)

		for {
			record, err := csvReader.Read()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				sr.Close()
				if u.log != nil {
					u.log.Error("failed to read csv record", slog.String("path", segment.Path), slog.String("error", err.Error()))
				}
				return handleError(err, false)
			}

			if headerSig != "" && isHeaderRecord(record, headerSig) {
				continue
			}

			if !schemaLoaded {
				sr.Close()
				return handleError(fmt.Errorf("schema not loaded for table %s", batch.Table), false)
			}

			values, err := convertRecord(record, schemaDef.Columns)
			if err != nil {
				sr.Close()
				if u.log != nil {
					recordCopy := append([]string(nil), record...)
					recordPreview := recordCopy
					if len(recordPreview) > 10 {
						recordPreview = recordPreview[:10]
					}
					u.log.Error("failed to convert record",
						slog.String("table", batch.Table),
						slog.String("schema_id", schemaID),
						slog.String("path", segment.Path),
						slog.Int("record_len", len(recordCopy)),
						slog.Any("record_preview", recordPreview),
						slog.String("header_signature", headerSig),
						slog.String("error", err.Error()))
				}
				return handleError(err, false)
			}

			if err := writer.Append(values...); err != nil {
				sr.Close()
				if u.log != nil {
					u.log.Error("failed to append row", slog.String("table", batch.Table), slog.String("error", err.Error()))
				}
				return handleError(err, isRetryable(err))
			}

			rowsInBatch++
			totalRows++

			if u.cfg.Batch.MaxRows > 0 && rowsInBatch >= u.cfg.Batch.MaxRows {
				if err := writer.Flush(); err != nil {
					sr.Close()
					if u.log != nil {
						u.log.Error("failed to flush batch", slog.String("table", batch.Table), slog.String("error", err.Error()))
					}
					return handleError(err, isRetryable(err))
				}
				rowsInBatch = 0
			}

			if flushTimer != nil {
				select {
				case <-flushTimer.C:
					if rowsInBatch > 0 {
						if err := writer.Flush(); err != nil {
							sr.Close()
							if u.log != nil {
								u.log.Error("failed to flush batch on interval", slog.String("table", batch.Table), slog.String("error", err.Error()))
							}
							return handleError(err, isRetryable(err))
						}
						rowsInBatch = 0
					}
					flushTimer.Reset(flushInterval)
				default:
				}
			}
		}

		sr.Close()
	}

	if processed == 0 {
		if err := batch.Remove(); err != nil {
			return fmt.Errorf("remove empty batch: %w", err)
		}
		return nil
	}

	if !schemaLoaded {
		return handleError(fmt.Errorf("no schema resolved for table %s", batch.Table), false)
	}

	if flushTimer != nil {
		flushTimer.Stop()
		flushTimer = nil
	}

	if totalRows == 0 {
		if err := batch.Remove(); err != nil {
			return fmt.Errorf("remove batch with no rows: %w", err)
		}
		return nil
	}

	if err := writer.Send(); err != nil {
		retryable := isRetryable(err)
		if !retryable && u.log != nil {
			u.log.Error("fatal clickhouse send error", slog.String("database", batch.Database), slog.String("error", err.Error()))
		}
		return handleError(fmt.Errorf("send clickhouse batch: %w", err), retryable)
	}

	if err := batch.Remove(); err != nil {
		return handleError(fmt.Errorf("remove batch: %w", err), true)
	}

	return nil
}

func (u *uploader) ensureSchema(ctx context.Context, table, schemaID string, mapping schema.SchemaMapping) (Schema, error) {
	key := schemaCacheKey(table, schemaID)

	u.schemaMu.RLock()
	if existing, ok := u.schemas[key]; ok {
		u.schemaMu.RUnlock()
		return existing, nil
	}
	u.schemaMu.RUnlock()

	unlock := u.lockSchemaTable(table)
	defer unlock()
	u.schemaMu.RLock()
	if existing, ok := u.schemas[key]; ok {
		u.schemaMu.RUnlock()
		return existing, nil
	}
	u.schemaMu.RUnlock()

	cols := convertToColumns(mapping)
	schemaCopy := Schema{Table: table, Columns: make([]Column, len(cols))}
	copy(schemaCopy.Columns, cols)

	if err := u.syncer.EnsureTable(ctx, table, schemaID, schemaCopy.Columns); err != nil {
		return Schema{}, err
	}

	u.schemaMu.Lock()
	u.schemas[key] = schemaCopy
	u.schemaMu.Unlock()
	return schemaCopy, nil
}

func schemaCacheKey(table, schemaID string) string {
	if schemaID == "" {
		return table
	}
	return table + "@" + schemaID
}

func readHeader(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func isHeaderRecord(record []string, header string) bool {
	if len(record) == 0 {
		return false
	}

	return strings.Join(record, ",") == header
}

func convertRecord(record []string, columns []Column) ([]any, error) {
	if len(record) < len(columns) {
		return nil, fmt.Errorf("record has %d fields, expected %d", len(record), len(columns))
	}

	values := make([]any, len(columns))
	for i, column := range columns {
		field := record[i]
		switch column.Type {
		case "DateTime64":
			if field == "" {
				values[i] = time.Unix(0, 0).UTC()
				continue
			}
			ts, err := time.Parse(time.RFC3339Nano, field)
			if err != nil {
				return nil, fmt.Errorf("column %s: %w", column.Name, err)
			}
			values[i] = ts
		case "UInt64":
			if field == "" {
				values[i] = uint64(0)
				continue
			}
			uv, err := strconv.ParseUint(field, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("column %s: %w", column.Name, err)
			}
			values[i] = uv
		case "Int64":
			if field == "" {
				values[i] = int64(0)
				continue
			}
			iv, err := strconv.ParseInt(field, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("column %s: %w", column.Name, err)
			}
			values[i] = iv
		case "Int32":
			if field == "" {
				values[i] = int32(0)
				continue
			}
			iv, err := strconv.ParseInt(field, 10, 32)
			if err != nil {
				return nil, fmt.Errorf("column %s: %w", column.Name, err)
			}
			values[i] = int32(iv)
		case "Float64":
			if field == "" {
				values[i] = float64(0)
				continue
			}
			fv, err := strconv.ParseFloat(field, 64)
			if err != nil {
				return nil, fmt.Errorf("column %s: %w", column.Name, err)
			}
			values[i] = fv
		case "JSON", "String":
			values[i] = field
		default:
			values[i] = field
		}
	}

	return values, nil
}

var fatalClickHouseCodes = map[int32]struct{}{
	44:  {}, // TYPE_MISMATCH
	53:  {}, // UNKNOWN_TYPE
	57:  {}, // UNKNOWN_DATABASE
	117: {}, // UNKNOWN_TABLE
	202: {}, // ILLEGAL_TYPE_OF_ARGUMENT
	241: {}, // READONLY
	271: {}, // NOT_FOUND_COLUMN_IN_BLOCK
}

func isRetryable(err error) bool {
	if err == nil {
		return true
	}

	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return true
	case errors.Is(err, clickhouse.ErrAcquireConnTimeout), errors.Is(err, clickhouse.ErrServerUnexpectedData), errors.Is(err, clickhouse.ErrBatchNotSent):
		return true
	case errors.Is(err, clickhouse.ErrAcquireConnNoAddress), errors.Is(err, clickhouse.ErrBatchInvalid), errors.Is(err, clickhouse.ErrBatchAlreadySent), errors.Is(err, clickhouse.ErrUnsupportedServerRevision):
		return false
	}

	if netErr, ok := errors.AsType[net.Error](err); ok {
		if netErr.Timeout() {
			return true
		}
	}

	if _, ok := errors.AsType[*clickhouse.OpError](err); ok {
		return false
	}

	if chErr, ok := errors.AsType[*clickhouse.Exception](err); ok {
		if _, ok := fatalClickHouseCodes[chErr.Code]; ok {
			return false
		}
		return true
	}

	return true
}

func buildInsertQuery(database, table string, columns []Column) string {
	names := make([]string, len(columns))
	for i, column := range columns {
		names[i] = quoteIdentifier(column.Name)
	}

	return fmt.Sprintf("INSERT INTO %s.%s (%s) VALUES", quoteIdentifier(database), quoteIdentifier(table), strings.Join(names, ", "))
}

func quoteIdentifier(id string) string {
	escaped := strings.ReplaceAll(id, "`", "``")
	return "`" + escaped + "`"
}
