package clickhouse

import (
	"fmt"
	"strconv"
	"time"

	"github.com/Azure/adx-mon/schema"
)

type valueConverter func(string) (any, error)

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
	Table      string
	Columns    []Column
	Converters []valueConverter
}

// DefaultSchemas returns the tables required for the ingestion pipeline. Later
// phases will expand this map with logs/native representations, but Phase 1 only
// needs a placeholder so downstream code has a well-defined anchor point.
func DefaultSchemas() map[string]Schema {
	metricsColumns := convertToColumns(schema.NewMetricsSchema())
	logsColumns := convertToColumns(schema.NewLogsSchema())

	return map[string]Schema{
		"metrics": {
			Table:      "metrics_samples",
			Columns:    metricsColumns,
			Converters: buildConverters(metricsColumns),
		},
		"logs": {
			Table:      "otel_logs",
			Columns:    logsColumns,
			Converters: buildConverters(logsColumns),
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

func buildConverters(columns []Column) []valueConverter {
	converters := make([]valueConverter, len(columns))
	for i, column := range columns {
		column := column
		switch column.Type {
		case "DateTime64":
			converters[i] = func(field string) (any, error) {
				if field == "" {
					return time.Unix(0, 0).UTC(), nil
				}
				ts, err := time.Parse(time.RFC3339Nano, field)
				if err != nil {
					return nil, fmt.Errorf("column %s: %w", column.Name, err)
				}
				return ts, nil
			}
		case "UInt64":
			converters[i] = func(field string) (any, error) {
				if field == "" {
					return uint64(0), nil
				}
				uv, err := strconv.ParseUint(field, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("column %s: %w", column.Name, err)
				}
				return uv, nil
			}
		case "Int64":
			converters[i] = func(field string) (any, error) {
				if field == "" {
					return int64(0), nil
				}
				iv, err := strconv.ParseInt(field, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("column %s: %w", column.Name, err)
				}
				return iv, nil
			}
		case "Int32":
			converters[i] = func(field string) (any, error) {
				if field == "" {
					return int32(0), nil
				}
				iv, err := strconv.ParseInt(field, 10, 32)
				if err != nil {
					return nil, fmt.Errorf("column %s: %w", column.Name, err)
				}
				return int32(iv), nil
			}
		case "Float64":
			converters[i] = func(field string) (any, error) {
				if field == "" {
					return float64(0), nil
				}
				fv, err := strconv.ParseFloat(field, 64)
				if err != nil {
					return nil, fmt.Errorf("column %s: %w", column.Name, err)
				}
				return fv, nil
			}
		default:
			converters[i] = func(field string) (any, error) {
				return field, nil
			}
		}
	}
	return converters
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
		return "String"
	default:
		return "String"
	}
}
