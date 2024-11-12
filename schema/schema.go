package schema

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	adxcsv "github.com/Azure/adx-mon/pkg/csv"
	"github.com/cespare/xxhash/v2"
)

var (
	DefaultMetricsMapping SchemaMapping = NewMetricsSchema()
	DefaultLogsMapping    SchemaMapping = NewLogsSchema()
)

const (
	max_adx_identifier_length = 1024
)

type SchemaMapping []CSVMapping

type CSVMapping struct {
	Column     string `json:"Column"`
	DataType   string `json:"DataType"`
	Properties struct {
		Ordinal    string `json:"Ordinal,omitempty"`
		ConstValue string `json:"ConstValue,omitempty"`
	} `json:"Properties"`
}

func NewMetricsSchema() SchemaMapping {
	var (
		mapping SchemaMapping
		idx     int
	)
	mapping = append(mapping, CSVMapping{
		Column:   "Timestamp",
		DataType: "datetime",
		Properties: struct {
			Ordinal    string `json:"Ordinal,omitempty"`
			ConstValue string `json:"ConstValue,omitempty"`
		}{
			Ordinal: strconv.Itoa(idx),
		},
	})
	idx += 1
	mapping = append(mapping, CSVMapping{
		Column:   "SeriesId",
		DataType: "long",
		Properties: struct {
			Ordinal    string `json:"Ordinal,omitempty"`
			ConstValue string `json:"ConstValue,omitempty"`
		}{
			Ordinal: strconv.Itoa(idx),
		},
	})

	idx += 1
	mapping = append(mapping, CSVMapping{
		Column:   "Labels",
		DataType: "dynamic",
		Properties: struct {
			Ordinal    string `json:"Ordinal,omitempty"`
			ConstValue string `json:"ConstValue,omitempty"`
		}{
			Ordinal: strconv.Itoa(idx),
		},
	})
	idx += 1
	mapping = append(mapping, CSVMapping{
		Column:   "Value",
		DataType: "real",
		Properties: struct {
			Ordinal    string `json:"Ordinal,omitempty"`
			ConstValue string `json:"ConstValue,omitempty"`
		}{
			Ordinal: strconv.Itoa(idx),
		},
	})
	return mapping
}

func NewLogsSchema() SchemaMapping {
	// https://opentelemetry.io/docs/specs/otel/logs/data-model/#log-and-event-record-definition
	var (
		mapping SchemaMapping
		idx     int
	)
	mapping = append(mapping, CSVMapping{
		Column:   "Timestamp",
		DataType: "datetime",
		Properties: struct {
			Ordinal    string `json:"Ordinal,omitempty"`
			ConstValue string `json:"ConstValue,omitempty"`
		}{
			Ordinal: strconv.Itoa(idx),
		},
	})
	idx += 1
	mapping = append(mapping, CSVMapping{
		Column:   "ObservedTimestamp",
		DataType: "datetime",
		Properties: struct {
			Ordinal    string `json:"Ordinal,omitempty"`
			ConstValue string `json:"ConstValue,omitempty"`
		}{
			Ordinal: strconv.Itoa(idx),
		},
	})
	idx += 1
	mapping = append(mapping, CSVMapping{
		Column:   "TraceId",
		DataType: "string",
		Properties: struct {
			Ordinal    string `json:"Ordinal,omitempty"`
			ConstValue string `json:"ConstValue,omitempty"`
		}{
			Ordinal: strconv.Itoa(idx),
		},
	})
	idx += 1
	mapping = append(mapping, CSVMapping{
		Column:   "SpanId",
		DataType: "string",
		Properties: struct {
			Ordinal    string `json:"Ordinal,omitempty"`
			ConstValue string `json:"ConstValue,omitempty"`
		}{
			Ordinal: strconv.Itoa(idx),
		},
	})
	idx += 1
	mapping = append(mapping, CSVMapping{
		Column:   "SeverityText",
		DataType: "string",
		Properties: struct {
			Ordinal    string `json:"Ordinal,omitempty"`
			ConstValue string `json:"ConstValue,omitempty"`
		}{
			Ordinal: strconv.Itoa(idx),
		},
	})
	idx += 1
	mapping = append(mapping, CSVMapping{
		Column:   "SeverityNumber",
		DataType: "int",
		Properties: struct {
			Ordinal    string `json:"Ordinal,omitempty"`
			ConstValue string `json:"ConstValue,omitempty"`
		}{
			Ordinal: strconv.Itoa(idx),
		},
	})
	idx += 1
	mapping = append(mapping, CSVMapping{
		Column:   "Body",
		DataType: "dynamic",
		Properties: struct {
			Ordinal    string `json:"Ordinal,omitempty"`
			ConstValue string `json:"ConstValue,omitempty"`
		}{
			Ordinal: strconv.Itoa(idx),
		},
	})
	idx += 1
	mapping = append(mapping, CSVMapping{
		Column:   "Resource",
		DataType: "dynamic",
		Properties: struct {
			Ordinal    string `json:"Ordinal,omitempty"`
			ConstValue string `json:"ConstValue,omitempty"`
		}{
			Ordinal: strconv.Itoa(idx),
		},
	})
	idx += 1
	mapping = append(mapping, CSVMapping{
		Column:   "Attributes",
		DataType: "dynamic",
		Properties: struct {
			Ordinal    string `json:"Ordinal,omitempty"`
			ConstValue string `json:"ConstValue,omitempty"`
		}{
			Ordinal: strconv.Itoa(idx),
		},
	})
	idx += 1
	return mapping
}

func (m SchemaMapping) AddConstMapping(col, value string) SchemaMapping {
	return append(m, CSVMapping{
		Column:   col,
		DataType: "string",
		Properties: struct {
			Ordinal    string `json:"Ordinal,omitempty"`
			ConstValue string `json:"ConstValue,omitempty"`
		}{
			Ordinal:    strconv.Itoa(len(m)),
			ConstValue: value,
		},
	})
}

func (m SchemaMapping) AddStringMapping(col string) SchemaMapping {
	return m.AddConstMapping(string(NormalizeMetricName([]byte(col))), "")
}

// NormalizeAdxIdentifier sanitizes a table or database name for use in ADX
// This does not do ProperCase transformations for metrics, only removes invalid characters
// See https://learn.microsoft.com/en-us/kusto/query/schema-entities/entity-names?view=microsoft-fabric#identifier-naming-rules
func NormalizeAdxIdentifier(s string) string {
	// Most common to not need normalization in this path. Only allocate if needed.
	bytesToRemove := 0
	for i := 0; i < len(s); i++ {
		// ADX appears to only accept ASCII letters/numbers
		if !(s[i] >= 'a' && s[i] <= 'z' || s[i] >= '0' && s[i] <= '9' || s[i] >= 'A' && s[i] <= 'Z') {
			bytesToRemove += 1
		}
	}

	if bytesToRemove == 0 && len(s) <= max_adx_identifier_length {
		return s
	}
	destSize := len(s) - bytesToRemove

	var b strings.Builder
	if destSize < max_adx_identifier_length {
		b.Grow(destSize)
	} else {
		b.Grow(max_adx_identifier_length)
	}
	for i := 0; i < len(s); i++ {
		if s[i] >= 'a' && s[i] <= 'z' || s[i] >= '0' && s[i] <= '9' || s[i] >= 'A' && s[i] <= 'Z' {
			b.WriteByte(s[i])
			if b.Len() >= max_adx_identifier_length {
				break
			}
		}
	}

	return b.String()
}

// NormalizeAdxIdentifier sanitizes a table or database name for use in ADX and appends it to dst.
// This does not do ProperCase transformations for metrics, only removes invalid characters
// See https://learn.microsoft.com/en-us/kusto/query/schema-entities/entity-names?view=microsoft-fabric#identifier-naming-rules
func AppendNormalizeAdxIdentifier(dst, s []byte) []byte {
	appendedChars := 0
	for i := 0; i < len(s); i++ {
		if s[i] >= 'a' && s[i] <= 'z' || s[i] >= '0' && s[i] <= '9' || s[i] >= 'A' && s[i] <= 'Z' {
			dst = append(dst, s[i])
			appendedChars += 1
			if appendedChars >= max_adx_identifier_length {
				break
			}
		}
	}

	return dst
}

// NormalizeMetricName converts a metrics name to a ProperCase table name
func NormalizeMetricName(s []byte) []byte {
	return AppendNormalizeMetricName(make([]byte, 0, len(s)), s)
}

// AppendNormalizeMetricName converts a metrics name to a ProperCase table name and appends it to dst.
func AppendNormalizeMetricName(dst, s []byte) []byte {
	// Most common to require normalization in the metric path to transform prom-style metrics to ProperCase table names
	appendedChars := 0
	for i := 0; i < len(s); i++ {
		if appendedChars >= max_adx_identifier_length {
			break
		}

		// Skip any non-alphanumeric characters, but capitalize the first letter after it
		// ADX appears to only accept ASCII letters/numbers
		allowedChar := s[i] >= 'a' && s[i] <= 'z' || s[i] >= '0' && s[i] <= '9' || s[i] >= 'A' && s[i] <= 'Z'
		if !allowedChar {
			if i+1 < len(s) {
				if s[i+1] >= 'a' && s[i+1] <= 'z' {
					dst = append(dst, byte(unicode.ToUpper(rune(s[i+1]))))
					appendedChars += 1
					i += 1
					continue
				}
			}
			continue
		}

		// Capitalize the first letter
		if i == 0 {
			dst = append(dst, byte(unicode.ToUpper(rune(s[i]))))
			appendedChars += 1
			continue
		}
		dst = append(dst, s[i])
		appendedChars += 1
	}
	return dst
}

func AppendCSVHeader(dst []byte, mapping SchemaMapping) []byte {
	for _, v := range mapping {
		if len(dst) > 0 {
			dst = append(dst, ',')
		}
		dst = append(dst, v.Column...)
		dst = append(dst, ':')
		dst = append(dst, v.DataType...)
	}
	return adxcsv.AppendNewLine(dst)
}

func UnmarshalSchema(data string) (SchemaMapping, error) {
	var mapping SchemaMapping
	idx := strings.IndexByte(data, '\n')
	if idx != -1 {
		data = data[:idx]
	}

	if len(data) == 0 {
		return SchemaMapping{}, nil
	}

	fields := strings.Split(data, ",")
	for i, v := range fields {
		nameType := strings.Split(v, ":")
		if len(nameType) != 2 {
			return nil, fmt.Errorf("invalid schema field: %s", data)
		}
		mapping = append(mapping, CSVMapping{
			Column:   nameType[0],
			DataType: nameType[1],
			Properties: struct {
				Ordinal    string `json:"Ordinal,omitempty"`
				ConstValue string `json:"ConstValue,omitempty"`
			}{
				Ordinal: strconv.Itoa(i),
			},
		})
	}

	return mapping, nil
}

func SchemaHash(mapping SchemaMapping) uint64 {
	x := xxhash.New()
	for _, v := range mapping {
		x.Write([]byte(v.Column))
		x.Write([]byte(v.DataType))
	}
	return x.Sum64()
}
