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

	"github.com/Azure/adx-mon/ingestor/storage"
	"github.com/Azure/adx-mon/ingestor/transform"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/unsafe"
	"github.com/cespare/xxhash"
)

type columnDef struct {
	name, typ string
}

type mgmt interface {
	Mgmt(ctx context.Context, db string, query kusto.Stmt, options ...kusto.MgmtOption) (*kusto.RowIterator, error)
}

type Syncer struct {
	KustoCli mgmt

	database string

	mu       sync.RWMutex
	mappings map[string]storage.SchemaMapping

	tables map[string]struct{}

	defaultMapping storage.SchemaMapping
}

type IngestionMapping struct {
	Name          string    `kusto:"Name"`
	Kind          string    `kusto:"Kind"`
	Mapping       string    `kusto:"Mapping"`
	LastUpdatedOn time.Time `kusto:"LastUpdatedOn"`
	Database      string    `kusto:"Database"`
	Table         string    `kusto:"Table"`
}

func NewSyncer(kustoCli mgmt, database string, defaultMapping storage.SchemaMapping) *Syncer {
	return &Syncer{
		KustoCli:       kustoCli,
		database:       database,
		defaultMapping: defaultMapping,
		mappings:       make(map[string]storage.SchemaMapping),
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

	return nil

}

func (s *Syncer) loadIngestionMappings(ctx context.Context) error {
	stmt := kusto.NewStmt(".show ingestion mappings")
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

		var sm storage.SchemaMapping
		if err := json.Unmarshal([]byte(v.Mapping), &sm); err != nil {
			return err
		}

		logger.Infof("Loaded ingestion mapping %s", v.Name)

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

	logger.Infof("Creating table %s %s", table, sb.String())

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
	kind := transform.Normalize([]byte(table))
	b.Write(kind)
	for _, v := range mapping {
		b.Write(transform.Normalize([]byte(v.Column)))
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

	// CountCardinality needs at least 1 table to exists before it can be created due to the union * statement.
	if err := s.EnsureTable("AdxmonIngestorMetricsCardinalityCount"); err != nil {
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
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}

	logger.Infof("Creating ingestion batching policy: MaximumBatchingTimeSpan=%s, MaximumNumberOfItems=%d, MaximumRawDataSizeMB=%d",
		p.MaximumBatchingTimeSpan, p.MaximumNumberOfItems, p.MaximumRawDataSizeMB)

	stmt := kusto.NewStmt("", kusto.UnsafeStmt(unsafe.Stmt{Add: true, SuppressWarning: true})).UnsafeAdd(
		fmt.Sprintf(".alter database %s policy ingestionbatching\n```%s\n```", s.database, string(b)))
	_, err = s.KustoCli.Mgmt(ctx, s.database, stmt)
	if err != nil {
		return err
	}
	return nil
}
