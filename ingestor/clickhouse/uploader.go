package clickhouse

import (
	"bufio"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Azure/adx-mon/ingestor/cluster"
	monlogger "github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/service"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/adx-mon/schema"
)

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
	schemas map[string]Schema
	conn    connectionManager

	mu       sync.RWMutex
	schemaMu sync.RWMutex
	cancel   context.CancelFunc
	ctx      context.Context
	open     bool

	workers int
	wg      sync.WaitGroup
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

	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}
	u.workers = workers

	for i := 0; i < workers; i++ {
		u.wg.Add(1)
		go u.worker(i)
	}

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

func (u *uploader) worker(_ int) {
	defer u.wg.Done()

	for {
		select {
		case <-u.ctx.Done():
			return
		case batch := <-u.queue:
			if batch == nil {
				continue
			}

			if err := u.processBatch(u.ctx, batch); err != nil {
				if u.log != nil {
					u.log.Error("failed to upload batch", slog.String("prefix", batch.Prefix), slog.String("error", err.Error()))
				}
			}
		}
	}
}

func (u *uploader) processBatch(ctx context.Context, batch *cluster.Batch) error {
	defer batch.Release()

	if batch.Database != u.cfg.Database {
		if u.log != nil {
			u.log.Error("clickhouse batch database mismatch", slog.String("expected", u.cfg.Database), slog.String("actual", batch.Database))
		}
		return nil
	}

	var (
		writer       batchWriter
		schemaDef    Schema
		schemaLoaded bool
		schemaID     string
		headerSig    string
		rowsInBatch  int
		totalRows    int
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
			return err
		}

		processed++

		bufReader := bufio.NewReader(sr)
		header, err := readHeader(bufReader)
		if err != nil {
			sr.Close()
			if u.log != nil {
				u.log.Error("failed to read segment header", slog.String("path", segment.Path), slog.String("error", err.Error()))
			}
			return err
		}

		if headerSig == "" {
			headerSig = header
			_, _, schemaName, _, err := wal.ParseFilename(segment.Path)
			if err != nil {
				sr.Close()
				if u.log != nil {
					u.log.Error("failed to parse segment filename", slog.String("path", segment.Path), slog.String("error", err.Error()))
				}
				return err
			}

			schemaID = schemaName
			mapping, err := schema.UnmarshalSchema(header)
			if err != nil {
				sr.Close()
				if u.log != nil {
					u.log.Error("failed to unmarshal schema", slog.String("path", segment.Path), slog.String("error", err.Error()))
				}
				return err
			}
			if len(mapping) == 0 {
				sr.Close()
				return fmt.Errorf("empty schema for table %s", batch.Table)
			}

			schemaDef = u.ensureSchema(batch.Table, schemaID, mapping)

			writer, err = u.conn.PrepareInsert(ctx, batch.Database, batch.Table, schemaDef.Columns)
			if err != nil {
				sr.Close()
				if u.log != nil {
					u.log.Error("failed to prepare clickhouse batch", slog.String("table", batch.Table), slog.String("error", err.Error()))
				}
				return err
			}

			schemaLoaded = true

			if flushInterval > 0 {
				flushTimer = time.NewTimer(flushInterval)
			}
		} else if headerSig != header {
			sr.Close()
			return fmt.Errorf("schema mismatch for table %s", batch.Table)
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
				return err
			}

			if !schemaLoaded {
				sr.Close()
				return fmt.Errorf("schema not loaded for table %s", batch.Table)
			}

			values, err := convertRecord(record, schemaDef.Columns)
			if err != nil {
				sr.Close()
				if u.log != nil {
					u.log.Error("failed to convert record", slog.String("table", batch.Table), slog.String("error", err.Error()))
				}
				return err
			}

			if err := writer.Append(values...); err != nil {
				sr.Close()
				if u.log != nil {
					u.log.Error("failed to append row", slog.String("table", batch.Table), slog.String("error", err.Error()))
				}
				return err
			}

			rowsInBatch++
			totalRows++

			if u.cfg.Batch.MaxRows > 0 && rowsInBatch >= u.cfg.Batch.MaxRows {
				if err := writer.Flush(); err != nil {
					sr.Close()
					if u.log != nil {
						u.log.Error("failed to flush batch", slog.String("table", batch.Table), slog.String("error", err.Error()))
					}
					return err
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
							return err
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
		return fmt.Errorf("no schema resolved for table %s", batch.Table)
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
		return fmt.Errorf("send clickhouse batch: %w", err)
	}

	if err := batch.Remove(); err != nil {
		return fmt.Errorf("remove batch: %w", err)
	}

	return nil
}

func (u *uploader) ensureSchema(table, schemaID string, mapping schema.SchemaMapping) Schema {
	key := schemaCacheKey(table, schemaID)

	u.schemaMu.RLock()
	if existing, ok := u.schemas[key]; ok {
		u.schemaMu.RUnlock()
		return existing
	}
	u.schemaMu.RUnlock()

	cols := convertToColumns(mapping)
	schemaCopy := Schema{Table: table, Columns: make([]Column, len(cols))}
	copy(schemaCopy.Columns, cols)

	u.schemaMu.Lock()
	defer u.schemaMu.Unlock()

	if existing, ok := u.schemas[key]; ok {
		return existing
	}

	u.schemas[key] = schemaCopy
	return schemaCopy
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
