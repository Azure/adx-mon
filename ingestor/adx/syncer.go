package adx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/schema"
	azkustodata "github.com/Azure/azure-kusto-go/azkustodata"
	"github.com/Azure/azure-kusto-go/azkustodata/kql"
	kustov1 "github.com/Azure/azure-kusto-go/azkustodata/query/v1"
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
	Mgmt(ctx context.Context, db string, query azkustodata.Statement, options ...azkustodata.QueryOption) (kustov1.Dataset, error)
}

type Syncer struct {
	KustoCli mgmt

	database string

	mu       sync.RWMutex
	mappings map[string]schema.SchemaMapping
	st       SampleType

	// inflight serializes concurrent schema work for the same entity without
	// blocking other tables behind a database-wide lock.
	inflightMu sync.Mutex
	inflight   map[string]*mappingEntry

	tables map[string]struct{}

	// schemas holds the columns of each table at startup, by table and column name, so tables that already have a
	// mapping's columns are not created again on their first upload.  It is guarded by mu.
	schemas map[string]map[string]string

	// streamingMu guards streamingPolicies, which records the result of enabling the streaming policy per table.
	streamingMu       sync.Mutex
	streamingPolicies map[string]streamingPolicyState

	defaultMapping schema.SchemaMapping
	cancelFn       context.CancelFunc
	wg             sync.WaitGroup
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
		KustoCli: kustoCli,

		database:          database,
		defaultMapping:    defaultMapping,
		mappings:          make(map[string]schema.SchemaMapping),
		st:                st,
		tables:            make(map[string]struct{}),
		streamingPolicies: make(map[string]streamingPolicyState),
	}
}

func (s *Syncer) Open(ctx context.Context) error {
	if err := s.loadIngestionMappings(ctx); err != nil {
		return err
	}

	if err := s.loadTableSchemas(ctx); err != nil {
		logger.Warnf("Failed to load table schemas for %s, tables are created on their first upload: %s", s.database, err)
	}

	if err := s.ensureFunctions(ctx); err != nil {
		return err
	}

	ctx, s.cancelFn = context.WithCancel(ctx)

	s.wg.Add(1)
	go s.reconcileTables(ctx)

	return nil
}

func (s *Syncer) Close() error {
	// cancelFn is nil if Open failed or was never called.
	if s.cancelFn != nil {
		s.cancelFn()
	}
	s.wg.Wait()
	return nil
}

func (s *Syncer) loadIngestionMappings(ctx context.Context) error {
	query := fmt.Sprintf(".show database %s ingestion mappings", s.database)
	stmt := kql.New("").AddUnsafe(query)
	ds, err := s.KustoCli.Mgmt(ctx, s.database, stmt)
	if err != nil {
		return err
	}
	table, ok := primaryResultTable(ds)
	if !ok {
		return fmt.Errorf("failed to load ingestion mappings: missing primary result table")
	}

	for _, row := range table.Rows() {
		var v IngestionMapping
		if err := row.ToStruct(&v); err != nil {
			return fmt.Errorf("failed to load ingestion mappings: %w", err)
		}

		var sm schema.SchemaMapping
		if err := json.Unmarshal([]byte(v.Mapping), &sm); err != nil {
			return fmt.Errorf("failed to load ingestion mappings: %w", err)
		}

		logger.Infof("Loaded %s ingestion mapping %s", s.database, v.Name)
		s.mappings[v.Name] = sm
	}

	return nil
}

type tableSchema struct {
	TableName string `kusto:"TableName"`
	Schema    string `kusto:"Schema"`
}

// loadTableSchemas loads the columns of the database's tables.
func (s *Syncer) loadTableSchemas(ctx context.Context) error {
	stmt := kql.New("").AddUnsafe(fmt.Sprintf(".show database %s cslschema", s.database))
	ds, err := s.KustoCli.Mgmt(ctx, s.database, stmt)
	if err != nil {
		return err
	}
	table, ok := primaryResultTable(ds)
	if !ok {
		return fmt.Errorf("missing primary result table")
	}

	schemas := make(map[string]map[string]string)
	for _, row := range table.Rows() {
		var v tableSchema
		if err := row.ToStruct(&v); err != nil {
			return err
		}
		columns := make(map[string]string)
		// The schema is formatted as name:type pairs separated by commas, with names quoted as ['name'] when needed.
		for col := range strings.SplitSeq(v.Schema, ",") {
			col = strings.TrimSpace(col)
			i := strings.LastIndex(col, ":")
			if i <= 0 {
				continue
			}
			name := strings.TrimSuffix(strings.TrimPrefix(col[:i], "['"), "']")
			columns[name] = col[i+1:]
		}
		schemas[v.TableName] = columns
	}

	s.mu.Lock()
	s.schemas = schemas
	s.mu.Unlock()
	return nil
}

// hasColumns reports whether table had every column in mapping, with the same type, when the schemas were loaded.
func (s *Syncer) hasColumns(table string, mapping schema.SchemaMapping) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	columns, ok := s.schemas[table]
	if !ok {
		return false
	}
	for _, c := range mapping {
		if columns[c.Column] != c.DataType {
			return false
		}
	}
	return true
}

// EnsureDefaultTable creates a table with the default schema mapping if it does not exist.
func (s *Syncer) EnsureDefaultTable(ctx context.Context, table string) error {
	return s.EnsureTable(ctx, table, s.defaultMapping)
}

// EnsureTable creates a table with the specified schema mapping if it does not exist.
func (s *Syncer) EnsureTable(ctx context.Context, table string, mapping schema.SchemaMapping) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.RLock()
	_, exists := s.tables[table]
	s.mu.RUnlock()
	if exists {
		return nil
	}

	key := "table:" + table
	entry := s.mappingLock(key)
	entry.Lock()
	defer s.releaseMappingLock(key, entry)

	s.mu.RLock()
	_, exists = s.tables[table]
	s.mu.RUnlock()
	if exists {
		return nil
	}

	if s.hasColumns(table, mapping) {
		// The loaded schema is used once, so a table dropped later is created again like any other uncached table.
		s.mu.Lock()
		s.tables[table] = struct{}{}
		delete(s.schemas, table)
		s.mu.Unlock()
		return nil
	}

	var columns []columnDef

	for _, v := range mapping {
		columns = append(columns, columnDef{
			name: v.Column,
			typ:  v.DataType,
		})
	}

	var sb strings.Builder
	sb.WriteString(".create-merge table ")
	fmt.Fprintf(&sb, "['%s'] ", table)

	sb.WriteString("(")
	for i, c := range mapping {
		fmt.Fprintf(&sb, "['%s']:%s", c.Column, c.DataType)
		if i < len(mapping)-1 {
			sb.WriteString(", ")
		}
	}
	sb.WriteString(")")

	if logger.IsDebug() {
		logger.Debugf("Creating table %s %s", table, sb.String())
	}

	showStmt := kql.New("").AddUnsafe(sb.String())

	_, err := s.KustoCli.Mgmt(ctx, s.database, showStmt)
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.tables[table] = struct{}{}
	s.mu.Unlock()

	return nil
}

// streamingPolicyRetryInterval is how long a failure to enable a table's streaming ingestion policy is cached before
// it is retried.
const streamingPolicyRetryInterval = 10 * time.Minute

type streamingPolicyState struct {
	enabled bool
	err     error
	retryAt time.Time
}

type mappingEntry struct {
	sync.Mutex
	users int // Includes holders and callers waiting to acquire the mutex.
}

// EnsureStreamingPolicy enables the streaming ingestion policy on table if it has not been enabled by this syncer.
// Failures are cached for streamingPolicyRetryInterval so a missing permission does not issue a management command
// for every batch.  A failure does not necessarily prevent streaming ingestion since the policy may be enabled at the
// database level.
func (s *Syncer) EnsureStreamingPolicy(ctx context.Context, table string) error {
	// Serialize only callers for this table so a stalled command does not block other tables.
	key := "streaming:" + table
	entry := s.mappingLock(key)
	entry.Lock()
	defer s.releaseMappingLock(key, entry)

	s.streamingMu.Lock()
	state := s.streamingPolicies[table]
	s.streamingMu.Unlock()
	if state.enabled {
		return nil
	}
	if state.err != nil && time.Now().Before(state.retryAt) {
		return state.err
	}

	stmt := kql.New(".alter table ").AddTable(table).AddLiteral(" policy streamingingestion enable")
	if _, err := s.KustoCli.Mgmt(ctx, s.database, stmt); err != nil {
		err = fmt.Errorf("enable streaming ingestion policy for %s.%s: %w", s.database, table, err)
		s.setStreamingPolicy(table, streamingPolicyState{err: err, retryAt: time.Now().Add(streamingPolicyRetryInterval)})
		return err
	}

	logger.Infof("Enabled streaming ingestion policy for %s.%s", s.database, table)
	s.setStreamingPolicy(table, streamingPolicyState{enabled: true})
	return nil
}

func (s *Syncer) setStreamingPolicy(table string, state streamingPolicyState) {
	s.streamingMu.Lock()
	defer s.streamingMu.Unlock()
	s.streamingPolicies[table] = state
}

// EnsureMapping creates a schema mapping for the specified table if it does not exist.  It returns the name of the mapping.
func (s *Syncer) EnsureDefaultMapping(ctx context.Context, table string) (string, error) {
	return s.EnsureMapping(ctx, table, s.defaultMapping)
}

// EnsureMapping creates a schema mapping for the specified table if it does not exist.  It returns the name of the mapping.
func (s *Syncer) EnsureMapping(ctx context.Context, table string, mapping schema.SchemaMapping) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var columns []columnDef

	for _, v := range mapping {
		columns = append(columns, columnDef{
			name: v.Column,
			typ:  v.DataType,
		})
	}

	var b bytes.Buffer
	kind := schema.NormalizeMetricName([]byte(table))
	b.Write(kind)
	for _, v := range mapping {
		b.Write(schema.NormalizeMetricName([]byte(v.Column)))
	}

	name := fmt.Sprintf("%s_%d", string(kind), xxhash.Sum64(b.Bytes()))

	s.mu.RLock()
	_, exists := s.mappings[name]
	s.mu.RUnlock()
	if exists {
		return name, nil
	}

	// Serialize only callers racing on this same mapping.
	entry := s.mappingLock(name)
	entry.Lock()
	defer s.releaseMappingLock(name, entry)

	s.mu.RLock()
	_, exists = s.mappings[name]
	s.mu.RUnlock()
	if exists {
		return name, nil
	}

	var sb strings.Builder
	sb.WriteString(".create-or-alter table ")
	fmt.Fprintf(&sb, "['%s'] ", table)
	fmt.Fprintf(&sb, "ingestion csv mapping \"%s\" '", name)

	jsonb, err := json.Marshal(mapping)
	if err != nil {
		return "", err
	}

	sb.Write(jsonb)

	sb.WriteString("'")

	logger.Infof("Creating ingestion mapping for table %s %s", table, sb.String())

	showStmt := kql.New("").AddUnsafe(sb.String())

	if _, err = s.KustoCli.Mgmt(ctx, s.database, showStmt); err != nil {
		return "", err
	}

	s.mu.Lock()
	s.mappings[name] = mapping
	s.mu.Unlock()
	return name, nil
}

// reclaimInflight drops per-entity locks that are no longer held.  Without it
// the map grows monotonically: every distinct table or mapping name that ever
// flows through permanently allocates a mutex.  Safe to call at any time, e.g.
// on each reconcileTables tick.
func (s *Syncer) reclaimInflight() {
	s.inflightMu.Lock()
	defer s.inflightMu.Unlock()

	for name, m := range s.inflight {
		if m.users == 0 {
			delete(s.inflight, name)
		}
	}
}

// mappingLock registers a user before returning, so reclamation cannot split
// waiters and holders across two mutexes for the same name.
func (s *Syncer) mappingLock(name string) *mappingEntry {
	s.inflightMu.Lock()
	defer s.inflightMu.Unlock()
	if s.inflight == nil {
		s.inflight = make(map[string]*mappingEntry)
	}
	entry, ok := s.inflight[name]
	if !ok {
		entry = &mappingEntry{}
		s.inflight[name] = entry
	}
	entry.users++
	return entry
}

func (s *Syncer) releaseMappingLock(name string, entry *mappingEntry) {
	// Unlock while still registered, then drop the reference under inflightMu.
	entry.Unlock()
	s.inflightMu.Lock()
	entry.users--
	s.inflightMu.Unlock()
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
			| extend Value=case(h == prev(h), case(diff < 0, Value, diff), real(0))
			| project-away prevVal, diff, h
		)}`},

		{
			name: "prom_rate",
			body: `.create-or-alter function prom_rate (T:(Timestamp:datetime, SeriesId: long, Labels:dynamic, Value:real), interval:timespan=1m) {
		T
		| where isnan(Value) == false
		| partition hint.strategy=shuffle by SeriesId (
			order by Timestamp asc
			| extend prevVal=prev(Value), prevTs=prev(Timestamp)
			| extend sampleGap=(Timestamp-prevTs)/1s
			| extend inc=case(isnull(prevVal), real(null), Value-prevVal < 0, Value, Value-prevVal)
			| extend Value=iff(sampleGap > 0, inc/sampleGap, real(null))
			| project Timestamp, SeriesId, Labels, Value
		)
		| where isfinite(Value)}`},

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
			| extend Value=case(h == prev(h), case(diff < 0, Value, diff), real(0))
			| project-away prevVal, diff, h
		)}`},
	}

	for _, fn := range functions {
		logger.Infof("Creating function %s", fn.name)
		stmt := kql.New("").AddUnsafe(fn.body)
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

// reconcileInterval is how often cached tables and mappings are checked against the database.
var reconcileInterval = 15 * time.Minute

func (s *Syncer) reconcileTables(ctx context.Context) {
	defer s.wg.Done()
	t := time.NewTicker(reconcileInterval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:

			// The per-entity lock map is reclaimed under its own lock; s.mu is
			// not held here, so no deadlock with the prunes below.
			s.reclaimInflight()

			logger.Infof("Reconcile tables and ingestion mappings for database %s", s.database)
			if err := func() error {
				tables, err := s.loadTables(ctx)
				if err != nil {
					return fmt.Errorf("error loading table details: %s", err)
				}

				if len(tables) == 0 {
					// An empty successful listing means every cached policy is stale.
					// A failed listing returns above and must not clear the cache.
					s.streamingMu.Lock()
					clear(s.streamingPolicies)
					s.streamingMu.Unlock()
					logger.Warnf("No tables found in database %s. Skipping ingestion mapping cleanup.", s.database)
					return nil
				}

				tableExists := make(map[string]struct{})
				// Mapping names are prefixed with the normalized table name, which differs from names such as my_logs.
				mappingTableExists := make(map[string]struct{})
				for _, v := range tables {
					tableExists[v.TableName] = struct{}{}
					mappingTableExists[string(schema.NormalizeMetricName([]byte(v.TableName)))] = struct{}{}
				}

				s.mu.Lock()
				defer s.mu.Unlock()

				for tblName := range s.tables {
					if _, ok := tableExists[tblName]; !ok {
						logger.Debugf("Removing cached table %s from %s", tblName, s.database)
						delete(s.tables, tblName)
					}
				}
				for tblName := range s.schemas {
					if _, ok := tableExists[tblName]; !ok {
						delete(s.schemas, tblName)
					}
				}

				for k := range s.mappings {
					tableName, _, _ := strings.Cut(k, "_")

					if _, ok := mappingTableExists[tableName]; !ok {
						logger.Debugf("Removing cached ingestion mapping %s from %s", k, s.database)
						delete(s.mappings, k)
					}
				}

				// streamingPolicies is keyed by bare table name, so it prunes
				// against tableExists directly.  It is guarded by streamingMu,
				// not s.mu, so it is pruned under its own lock after s.mu is
				// released by the deferred unlock.
				s.streamingMu.Lock()
				for tbl := range s.streamingPolicies {
					if _, ok := tableExists[tbl]; !ok {
						logger.Debugf("Removing cached streaming policy for table %s from %s", tbl, s.database)
						delete(s.streamingPolicies, tbl)
					}
				}
				s.streamingMu.Unlock()

				return nil
			}(); err != nil {
				logger.Errorf("Error removing tables and mappings from cache: %s", err)
			}
		}
	}
}

func (s *Syncer) loadTables(ctx context.Context) ([]Table, error) {
	stmt := kql.New(".show tables | project TableName")
	ds, err := s.KustoCli.Mgmt(ctx, s.database, stmt)
	if err != nil {
		return nil, err
	}
	table, ok := primaryResultTable(ds)
	if !ok {
		return nil, fmt.Errorf("failed to load tables: missing primary result table")
	}

	var out []Table
	for _, row := range table.Rows() {
		var v Table
		if err := row.ToStruct(&v); err != nil {
			return nil, fmt.Errorf("failed to load tables: %w", err)
		}
		out = append(out, v)
	}

	return out, nil
}
