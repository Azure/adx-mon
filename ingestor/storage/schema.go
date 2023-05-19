package storage

import (
	"strconv"

	"github.com/Azure/adx-mon/ingestor/transform"
)

var DefaultMapping SchemaMapping = NewSchema()

type SchemaMapping []CSVMapping

type CSVMapping struct {
	Column     string `json:"Column"`
	DataType   string `json:"DataType"`
	Properties struct {
		Ordinal    string `json:"Ordinal,omitempty"`
		ConstValue string `json:"ConstValue,omitempty"`
	} `json:"Properties"`
}

func NewSchema() SchemaMapping {
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
	return m.AddConstMapping(string(transform.Normalize([]byte(col))), "")
}
