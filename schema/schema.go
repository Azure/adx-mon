package schema

import (
	"strconv"
	"unicode"
)

var (
	DefaultMetricsMapping SchemaMapping = NewMetricsSchema()
	DefaultLogsMapping    SchemaMapping = NewLogsSchema()
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
	mapping := append([]CSVMapping{}, m[:len(m)-2]...)

	mapping = append(mapping, CSVMapping{
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

	mapping = append(mapping, m[len(m)-2:]...)
	for i := 2; i < len(mapping); i++ {
		mapping[i].Properties.Ordinal = strconv.Itoa(i)
	}

	return mapping
}

func (m SchemaMapping) AddStringMapping(col string) SchemaMapping {
	return m.AddConstMapping(string(Normalize([]byte(col))), "")
}

// Normalize converts a metrics name to a ProperCase table name
func Normalize(s []byte) []byte {
	return AppendNormalize(make([]byte, 0, len(s)), s)
}

// AppendNormalize converts a metrics name to a ProperCase table name and appends it to dst.
func AppendNormalize(dst, s []byte) []byte {
	for i := 0; i < len(s); i++ {
		// Skip any non-alphanumeric characters, but capitalize the first letter after it
		allowedChar := s[i] >= 'a' && s[i] <= 'z' || s[i] >= '0' && s[i] <= '9' || s[i] >= 'A' && s[i] <= 'Z'
		if !allowedChar {
			if i+1 < len(s) {
				if s[i+1] >= 'a' && s[i+1] <= 'z' {
					dst = append(dst, byte(unicode.ToUpper(rune(s[i+1]))))
					i += 1
					continue
				}
			}
			continue
		}

		// Capitalize the first letter
		if i == 0 {
			dst = append(dst, byte(unicode.ToUpper(rune(s[i]))))
			continue
		}
		dst = append(dst, s[i])
	}
	return dst
}
