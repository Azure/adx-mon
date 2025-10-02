package clickhouse

import "github.com/Azure/adx-mon/schema"

// Column describes a single ClickHouse column definition. We keep
// the representation intentionally lightweight so later phases can
// translate it into CREATE TABLE statements or use it to validate
// incoming batches.
type Column struct {
	Name string
	Type string
}

// Schema holds the minimal metadata we need to reason about a ClickHouse table.
type Schema struct {
	Table   string
	Columns []Column
}

// DefaultSchemas returns the tables required for the ingestion pipeline. Later
// phases will expand this map with logs/native representations, but Phase 1 only
// needs a placeholder so downstream code has a well-defined anchor point.
func DefaultSchemas() map[string]Schema {
	return map[string]Schema{
		"metrics": {
			Table:   "metrics_samples",
			Columns: convertToColumns(schema.NewMetricsSchema()),
		},
		"logs": {
			Table:   "otel_logs",
			Columns: convertToColumns(schema.NewLogsSchema()),
		},
	}
}

func convertToColumns(mapping schema.SchemaMapping) []Column {
	columns := make([]Column, 0, len(mapping))
	for _, field := range mapping {
		columns = append(columns, Column{
			Name: field.Column,
			Type: mapClickHouseType(field.Column, field.DataType),
		})
	}
	return columns
}

func mapClickHouseType(columnName, adxType string) string {
	switch columnName {
	case "SeriesId":
		return "UInt64"
	case "SeverityNumber":
		return "Int32"
	}

	switch adxType {
	case "datetime":
		return "DateTime64"
	case "long":
		return "Int64"
	case "int":
		return "Int32"
	case "real":
		return "Float64"
	case "string":
		return "String"
	case "dynamic":
		return "JSON"
	default:
		return "String"
	}
}
