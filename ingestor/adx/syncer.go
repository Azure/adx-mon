package adx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/schema"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/unsafe"
	"github.com/cespare/xxhash"
)

type SampleType int

const (
	PromMetrics SampleType = iota
	OTLPLogs
)

type columnDef struct {
	name, typ string
}

type mgmt interface {
	Mgmt(ctx context.Context, db string, query kusto.Statement, options ...kusto.MgmtOption) (*kusto.RowIterator, error)
}

type Syncer struct {
	KustoCli mgmt

	database string

	mu       sync.RWMutex
	mappings map[string]schema.SchemaMapping
	st       SampleType

	tables map[string]struct{}

	defaultMapping schema.SchemaMapping
	cancelFn       context.CancelFunc
}

type IngestionMapping struct {
	Name          string    `kusto:"Name"`
	Kind          string    `kusto:"Kind"`
	Mapping       string    `kusto:"Mapping"`
	LastUpdatedOn time.Time `kusto:"LastUpdatedOn"`
	Database      string    `kusto:"Database"`
	Table         string    `kusto:"Table"`
}

type Table struct {
	TableName string `kusto:"TableName"`
}

func NewSyncer(kustoCli mgmt, database string, defaultMapping schema.SchemaMapping, st SampleType) *Syncer {
	return &Syncer{
		KustoCli:       kustoCli,
		database:       database,
		defaultMapping: defaultMapping,
		mappings:       make(map[string]schema.SchemaMapping),
		st:             st,
		tables:         make(map[string]struct{}),
	}
}

func (s *Syncer) Open(ctx context.Context) error {
	if err := s.loadIngestionMappings(ctx); err != nil {
		return err
	}

	if err := s.ensureFunctions(ctx); err != nil {
		return err
	}

	if err := s.ensureIngestionPolicy(ctx); err != nil {
		return err
	}

	ctx, s.cancelFn = context.WithCancel(ctx)

	go s.reconcileMappings(ctx)

	return nil
}

func (s *Syncer) Close() error {
	s.cancelFn()
	return nil
}

func (s *Syncer) loadIngestionMappings(ctx context.Context) error {
	query := fmt.Sprintf(".show database %s ingestion mappings", s.database)
	stmt := kusto.NewStmt("", kusto.UnsafeStmt(unsafe.Stmt{Add: true, SuppressWarning: true})).UnsafeAdd(query)
	rows, err := s.KustoCli.Mgmt(ctx, s.database, stmt)
	if err != nil {
		return err
	}

	for {
		row, err1, err2 := rows.NextRowOrError()
		if err2 == io.EOF {
			return nil
		} else if err1 != nil {
			return err1
		} else if err2 != nil {
			return err2
		}

		var v IngestionMapping
		if err := row.ToStruct(&v); err != nil {
			return err
		}

		var sm schema.SchemaMapping
		if err := json.Unmarshal([]byte(v.Mapping), &sm); err != nil {
			return err
		}

		logger.Infof("Loaded %s ingestion mapping %s", s.database, v.Name)

		s.mappings[v.Name] = sm
	}
}

func (s *Syncer) EnsureTable(table string) error {
	s.mu.RLock()
	if _, ok := s.tables[table]; ok {
		s.mu.RUnlock()
		return nil
	}
	s.mu.RUnlock()

	mapping := s.defaultMapping

	var columns []columnDef

	for _, v := range mapping {
		columns = append(columns, columnDef{
			name: v.Column,
			typ:  v.DataType,
		})
	}

	var sb strings.Builder
	sb.WriteString(".create-merge table ")
	sb.WriteString(fmt.Sprintf("['%s'] ", table))

	sb.WriteString("(")
	for i, c := range mapping {
		sb.WriteString(fmt.Sprintf("['%s']:%s", c.Column, c.DataType))
		if i < len(mapping)-1 {
			sb.WriteString(", ")
		}
	}
	sb.WriteString(")")

	if logger.IsDebug() {
		logger.Debugf("Creating table %s %s", table, sb.String())
	}

	showStmt := kusto.NewStmt("", kusto.UnsafeStmt(unsafe.Stmt{Add: true, SuppressWarning: true})).UnsafeAdd(sb.String())

	rows, err := s.KustoCli.Mgmt(context.Background(), s.database, showStmt)
	if err != nil {
		return err
	}

	for {
		_, err1, err2 := rows.NextRowOrError()
		if err2 == io.EOF {
			break
		} else if err1 != nil {
			return err1
		} else if err2 != nil {
			return err2
		}
	}

	s.mu.Lock()
	s.tables[table] = struct{}{}
	s.mu.Unlock()

	return nil
}

// EnsureMapping creates a schema mapping for the specified table if it does not exist.  It returns the name of the mapping.
func (s *Syncer) EnsureMapping(table string) (string, error) {
	var columns []columnDef

	mapping := s.defaultMapping
	for _, v := range mapping {
		columns = append(columns, columnDef{
			name: v.Column,
			typ:  v.DataType,
		})
	}

	var b bytes.Buffer
	kind := schema.Normalize([]byte(table))
	b.Write(kind)
	for _, v := range mapping {
		b.Write(schema.Normalize([]byte(v.Column)))
	}

	name := fmt.Sprintf("%s_%d", string(kind), xxhash.Sum64(b.Bytes()))

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.mappings[name]; ok {
		return name, nil
	}

	var sb strings.Builder
	sb.WriteString(".create-or-alter table ")
	sb.WriteString(fmt.Sprintf("['%s'] ", table))
	sb.WriteString(fmt.Sprintf("ingestion csv mapping \"%s\" '", name))

	jsonb, err := json.Marshal(mapping)
	if err != nil {
		return "", err
	}

	sb.Write(jsonb)

	sb.WriteString("'")

	logger.Infof("Creating table %s %s", table, sb.String())

	showStmt := kusto.NewStmt("", kusto.UnsafeStmt(unsafe.Stmt{Add: true, SuppressWarning: true})).UnsafeAdd(sb.String())

	rows, err := s.KustoCli.Mgmt(context.Background(), s.database, showStmt)
	if err != nil {
		return "", err
	}

	for {
		_, err1, err2 := rows.NextRowOrError()
		if err2 == io.EOF {
			break
		} else if err1 != nil {
			return "", err1
		} else if err2 != nil {
			return "", err2
		}
	}
	s.mappings[name] = mapping
	return name, nil
}

func (s *Syncer) ensureFunctions(ctx context.Context) error {
	switch s.st {
	case PromMetrics:
		return s.ensurePromMetricsFunctions(ctx)
	case OTLPLogs:
		return s.ensureOTLPLogsFunctions(ctx)
	default:
		return fmt.Errorf("unknown sample type %d", s.st)
	}
}

func (s *Syncer) ensurePromMetricsFunctions(ctx context.Context) error {
	// functions is the list of functions that we need to create in the database.  They are executed in order.
	functions := []struct {
		name string
		body string
	}{
		{
			name: "prom_increase",
			body: `.create-or-alter function prom_increase (T:(Timestamp:datetime, SeriesId: long, Labels:dynamic, Value:real), interval:timespan=1m) {
		T
		| where isnan(Value)==false
		| extend h=SeriesId
		| partition hint.strategy=shuffle by h (
			as Series
			| order by h, Timestamp asc
			| extend prevVal=prev(Value)
			| extend diff=Value-prevVal
			| extend Value=case(h == prev(h), case(diff < 0, next(Value)-Value, diff), real(0))
			| project-away prevVal, diff, h
		)}`},

		{
			name: "prom_rate",
			body: `.create-or-alter function prom_rate (T:(Timestamp:datetime, SeriesId: long, Labels:dynamic, Value:real), interval:timespan=1m) {
		T
		| invoke prom_increase(interval=interval)
		| extend Value=Value/((Timestamp-prev(Timestamp))/1s)
		| where isnotnull(Value)
		| where isnan(Value) == false}`},

		{

			name: "prom_delta",
			body: `.create-or-alter function prom_delta (T:(Timestamp:datetime, SeriesId: long, Labels:dynamic, Value:real), interval:timespan=1m) {
		T
		| where isnan(Value)==false
		| extend h=SeriesId
		| partition hint.strategy=shuffle by h (
			as Series
			| order by h, Timestamp asc
			| extend prevVal=prev(Value)
			| extend diff=Value-prevVal
			| extend Value=case(h == prev(h), case(diff < 0, next(Value)-Value, diff), real(0))
			| project-away prevVal, diff, h
		)}`},
		{

			name: "CountCardinality",
			body: `.create-or-alter function CountCardinality () {
				union withsource=table *
				| where Timestamp >= ago(1h) and Timestamp < ago(5m)
				| summarize Value=toreal(dcount(SeriesId)) by table
				| extend SeriesId=hash_xxhash64(table)
				| extend Timestamp=bin(now(), 1m)
				| extend Labels=bag_pack_columns(table)
				| project Timestamp, SeriesId, Labels, Value
		}`},
	}

	// This table is used to store the cardinality of all series in the database.  It's updated by the CountCardinality function
	// but we can't create the function unless a table exists.
	stmt := kusto.NewStmt("", kusto.UnsafeStmt(unsafe.Stmt{Add: true, SuppressWarning: true})).UnsafeAdd(
		".create table AdxmonIngestorTableCardinalityCount (Timestamp: datetime, SeriesId: long, Labels: dynamic, Value: real)")
	_, err := s.KustoCli.Mgmt(ctx, s.database, stmt)
	if err != nil {
		return err
	}

	for _, fn := range functions {
		logger.Infof("Creating function %s", fn.name)
		stmt := kusto.NewStmt("", kusto.UnsafeStmt(unsafe.Stmt{Add: true, SuppressWarning: true})).UnsafeAdd(fn.body)
		_, err := s.KustoCli.Mgmt(ctx, s.database, stmt)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Syncer) ensureOTLPLogsFunctions(ctx context.Context) error {
	return nil
}

func (s *Syncer) ensureIngestionPolicy(ctx context.Context) error {
	type ingestionPolicy struct {
		MaximumBatchingTimeSpan string `json:"MaximumBatchingTimeSpan"`
		MaximumNumberOfItems    int    `json:"MaximumNumberOfItems"`
		MaximumRawDataSizeMB    int    `json:"MaximumRawDataSizeMB"`
	}

	p := &ingestionPolicy{
		MaximumBatchingTimeSpan: "00:00:30",
		MaximumNumberOfItems:    500,
		MaximumRawDataSizeMB:    1000,
	}

	// Optimize logs for throughput instead of latency
	if s.st == OTLPLogs {
		p = &ingestionPolicy{
			MaximumBatchingTimeSpan: "00:05:00",
			MaximumNumberOfItems:    500,
			MaximumRawDataSizeMB:    4096,
		}
	}

	b, err := json.Marshal(p)
	if err != nil {
		return err
	}

	logger.Infof("Creating ingestion batching policy: Database=%s MaximumBatchingTimeSpan=%s, MaximumNumberOfItems=%d, MaximumRawDataSizeMB=%d",
		s.database, p.MaximumBatchingTimeSpan, p.MaximumNumberOfItems, p.MaximumRawDataSizeMB)

	stmt := kusto.NewStmt("", kusto.UnsafeStmt(unsafe.Stmt{Add: true, SuppressWarning: true})).UnsafeAdd(
		fmt.Sprintf(".alter-merge database %s policy ingestionbatching\n```%s\n```", s.database, string(b)))
	_, err = s.KustoCli.Mgmt(ctx, s.database, stmt)
	if err != nil {
		return err
	}
	return nil
}

func (s *Syncer) reconcileMappings(ctx context.Context) {
	t := time.NewTicker(24 * time.Hour)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:

			if err := func() error {
				tables, err := s.loadTables(ctx)
				if err != nil {
					return fmt.Errorf("error loading table details: %s", err)
				}

				if len(tables) == 0 {
					logger.Warnf("No tables found in database %s. Skipping ingestion mapping cleanup.", s.database)
					return nil
				}

				tableExists := make(map[string]struct{})
				for _, v := range tables {
					tableExists[v.TableName] = struct{}{}
				}

				s.mu.Lock()
				defer s.mu.Unlock()

				for k := range s.mappings {
					tableName := strings.Split(k, "_")[0]

					if _, ok := tableExists[tableName]; !ok {
						logger.Debugf("Removing cached ingestion mapping %s from %s", k, s.database)
						delete(s.mappings, k)
					}
				}
				return nil
			}(); err != nil {
				logger.Errorf("Error removing unused ingestion mappings: %s", err)
			}
		}
	}
}

func (s *Syncer) loadTables(ctx context.Context) ([]Table, error) {
	stmt := kusto.NewStmt(".show tables | project TableName")
	rows, err := s.KustoCli.Mgmt(ctx, s.database, stmt)
	if err != nil {
		return nil, err
	}

	var tables []Table
	for {
		row, err1, err2 := rows.NextRowOrError()
		if err2 == io.EOF {
			return tables, nil
		} else if err1 != nil {
			return tables, err1
		} else if err2 != nil {
			return tables, err2
		}

		var v Table
		if err := row.ToStruct(&v); err != nil {
			return tables, err
		}
		tables = append(tables, v)
	}
	return tables, nil
}
