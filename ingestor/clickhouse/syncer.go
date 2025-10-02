package clickhouse

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	xxhash "github.com/cespare/xxhash/v2"
)

const schemaMetadataTable = "adxmon_schema_versions"

type syncer struct {
	database string
	conn     connectionManager
	log      *slog.Logger

	mu            sync.Mutex
	metadataReady bool
	ensured       map[string]struct{}
}

func newSyncer(database string, conn connectionManager, log *slog.Logger) *syncer {
	return &syncer{
		database: database,
		conn:     conn,
		log:      log,
		ensured:  make(map[string]struct{}),
	}
}

func (s *syncer) EnsureInitial(ctx context.Context, defaults map[string]Schema) error {
	if err := s.ensureMetadataTable(ctx); err != nil {
		return err
	}

	for key, schema := range defaults {
		if schema.Table == "" || len(schema.Columns) == 0 {
			continue
		}
		schemaID := decodeSchemaIDFromCacheKey(key, schema.Table)
		if err := s.EnsureTable(ctx, schema.Table, schemaID, schema.Columns); err != nil {
			return err
		}
	}

	return nil
}

func (s *syncer) EnsureTable(ctx context.Context, table, schemaID string, columns []Column) error {
	if len(columns) == 0 {
		return fmt.Errorf("clickhouse syncer: no columns provided for table %s", table)
	}

	if err := s.ensureMetadataTable(ctx); err != nil {
		return err
	}

	key := schemaCacheKey(table, schemaID)

	s.mu.Lock()
	if _, ok := s.ensured[key]; ok {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	ddl := buildCreateTableStatement(s.database, table, columns)
	if err := s.conn.Exec(ctx, ddl); err != nil {
		return fmt.Errorf("create clickhouse table %s: %w", table, err)
	}

	if err := s.insertSchemaVersion(ctx, table, schemaID, columns); err != nil {
		return err
	}

	if s.log != nil {
		s.log.Info("clickhouse table ensured", slog.String("table", table), slog.String("schema_id", schemaID))
	}

	s.mu.Lock()
	s.ensured[key] = struct{}{}
	s.mu.Unlock()
	return nil
}

func (s *syncer) ensureMetadataTable(ctx context.Context) error {
	s.mu.Lock()
	if s.metadataReady {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	ddl := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.%s (
        Table String,
        SchemaID String,
        Version UInt64,
        Header String,
        UpdatedAt DateTime
    )
    ENGINE = ReplacingMergeTree(UpdatedAt)
    ORDER BY (Table, SchemaID)`, quoteIdentifier(s.database), quoteIdentifier(schemaMetadataTable))

	if err := s.conn.Exec(ctx, ddl); err != nil {
		return fmt.Errorf("create metadata table: %w", err)
	}

	if s.log != nil {
		s.log.Info("clickhouse schema metadata table ensured", slog.String("table", schemaMetadataTable))
	}

	s.mu.Lock()
	s.metadataReady = true
	s.mu.Unlock()
	return nil
}

func (s *syncer) insertSchemaVersion(ctx context.Context, table, schemaID string, columns []Column) error {
	header, version := columnsSignature(columns)
	if schemaID == "" {
		schemaID = "default"
	}

	query := fmt.Sprintf("INSERT INTO %s.%s (Table, SchemaID, Version, Header, UpdatedAt) VALUES (%s, %s, %d, %s, now())",
		quoteIdentifier(s.database),
		quoteIdentifier(schemaMetadataTable),
		quoteLiteral(table),
		quoteLiteral(schemaID),
		version,
		quoteLiteral(header),
	)

	if err := s.conn.Exec(ctx, query); err != nil {
		return fmt.Errorf("record schema version for %s: %w", table, err)
	}

	return nil
}

func buildCreateTableStatement(database, table string, columns []Column) string {
	var sb strings.Builder
	sb.WriteString("CREATE TABLE IF NOT EXISTS ")
	sb.WriteString(quoteIdentifier(database))
	sb.WriteString(".")
	sb.WriteString(quoteIdentifier(table))
	sb.WriteString(" (")
	for i, column := range columns {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(quoteIdentifier(column.Name))
		sb.WriteString(" ")
		sb.WriteString(column.Type)
	}
	sb.WriteString(") ")

	orderBy := buildOrderByClause(columns)
	if orderBy != "" {
		hasTimestamp := columnExists(columns, "Timestamp")
		if hasTimestamp {
			sb.WriteString("ENGINE = MergeTree PARTITION BY toYYYYMMDD(Timestamp) ORDER BY ")
		} else {
			sb.WriteString("ENGINE = MergeTree ORDER BY ")
		}
		sb.WriteString(orderBy)
	} else {
		sb.WriteString("ENGINE = MergeTree ORDER BY tuple()")
	}
	sb.WriteString(" SETTINGS index_granularity = 8192")
	return sb.String()
}

func buildOrderByClause(columns []Column) string {
	var hasSeries, hasTimestamp bool
	for _, column := range columns {
		switch column.Name {
		case "SeriesId":
			hasSeries = true
		case "Timestamp":
			hasTimestamp = true
		}
	}

	switch {
	case hasSeries && hasTimestamp:
		return fmt.Sprintf("(%s, %s)", quoteIdentifier("SeriesId"), quoteIdentifier("Timestamp"))
	case hasTimestamp:
		return fmt.Sprintf("(%s)", quoteIdentifier("Timestamp"))
	case len(columns) > 0:
		return fmt.Sprintf("(%s)", quoteIdentifier(columns[0].Name))
	default:
		return ""
	}
}

func columnExists(columns []Column, name string) bool {
	for _, column := range columns {
		if column.Name == name {
			return true
		}
	}
	return false
}

func columnsSignature(columns []Column) (string, uint64) {
	parts := make([]string, len(columns))
	hasher := xxhash.New()
	for i, column := range columns {
		parts[i] = fmt.Sprintf("%s:%s", column.Name, column.Type)
		hasher.Write([]byte(column.Name))
		hasher.Write([]byte{'|'})
		hasher.Write([]byte(column.Type))
		hasher.Write([]byte{'|'})
	}
	return strings.Join(parts, ","), hasher.Sum64()
}

func quoteLiteral(value string) string {
	escaped := strings.ReplaceAll(value, "'", "''")
	return "'" + escaped + "'"
}

func decodeSchemaIDFromCacheKey(key, table string) string {
	if table != "" && key == table {
		return ""
	}

	if idx := strings.Index(key, "@"); idx != -1 {
		return key[idx+1:]
	}

	return key
}
