package schema_test

import (
	"encoding/json"
	"testing"

	"github.com/Azure/adx-mon/schema"
	"github.com/stretchr/testify/require"
)

func TestNewSchema_NoLabels(t *testing.T) {
	mapping := schema.NewMetricsSchema()
	_, err := json.Marshal(mapping)
	require.NoError(t, err)

	require.Equal(t, len(mapping), 4)
	require.Equal(t, "Timestamp", mapping[0].Column)
	require.Equal(t, "datetime", mapping[0].DataType)
	require.Equal(t, "0", mapping[0].Properties.Ordinal)

	require.Equal(t, "SeriesId", mapping[1].Column)
	require.Equal(t, "long", mapping[1].DataType)
	require.Equal(t, "1", mapping[1].Properties.Ordinal)

	require.Equal(t, "Labels", mapping[2].Column)
	require.Equal(t, "dynamic", mapping[2].DataType)
	require.Equal(t, "2", mapping[2].Properties.Ordinal)

	require.Equal(t, "Value", mapping[3].Column)
	require.Equal(t, "real", mapping[3].DataType)
	require.Equal(t, "3", mapping[3].Properties.Ordinal)
}

func TestNewSchema_AddConstMapping(t *testing.T) {
	mapping := schema.NewMetricsSchema()
	mapping = mapping.AddConstMapping("Region", "eastus")

	_, err := json.Marshal(mapping)
	require.NoError(t, err)

	require.Equal(t, len(mapping), 5)
	require.Equal(t, "Timestamp", mapping[0].Column)
	require.Equal(t, "datetime", mapping[0].DataType)
	require.Equal(t, "0", mapping[0].Properties.Ordinal)

	require.Equal(t, "SeriesId", mapping[1].Column)
	require.Equal(t, "long", mapping[1].DataType)
	require.Equal(t, "1", mapping[1].Properties.Ordinal)

	require.Equal(t, "Region", mapping[2].Column)
	require.Equal(t, "string", mapping[2].DataType)
	require.Equal(t, "2", mapping[2].Properties.Ordinal)
	require.Equal(t, "eastus", mapping[2].Properties.ConstValue)

	require.Equal(t, "Labels", mapping[3].Column)
	require.Equal(t, "dynamic", mapping[3].DataType)
	require.Equal(t, "3", mapping[3].Properties.Ordinal)

	require.Equal(t, "Value", mapping[4].Column)
	require.Equal(t, "real", mapping[4].DataType)
	require.Equal(t, "4", mapping[4].Properties.Ordinal)

}

func TestNewSchema_AddLiftedMapping(t *testing.T) {
	mapping := schema.NewMetricsSchema()

	mapping = mapping.AddStringMapping("Region")
	mapping = mapping.AddStringMapping("Host")

	_, err := json.Marshal(mapping)
	require.NoError(t, err)

	require.Equal(t, len(mapping), 6)
	require.Equal(t, "Timestamp", mapping[0].Column)
	require.Equal(t, "datetime", mapping[0].DataType)
	require.Equal(t, "0", mapping[0].Properties.Ordinal)

	require.Equal(t, "SeriesId", mapping[1].Column)
	require.Equal(t, "long", mapping[1].DataType)
	require.Equal(t, "1", mapping[1].Properties.Ordinal)

	require.Equal(t, "Region", mapping[2].Column)
	require.Equal(t, "string", mapping[2].DataType)
	require.Equal(t, "2", mapping[2].Properties.Ordinal)

	require.Equal(t, "Host", mapping[3].Column)
	require.Equal(t, "string", mapping[3].DataType)
	require.Equal(t, "3", mapping[3].Properties.Ordinal)

	require.Equal(t, "Labels", mapping[4].Column)
	require.Equal(t, "dynamic", mapping[4].DataType)
	require.Equal(t, "4", mapping[4].Properties.Ordinal)

	require.Equal(t, "Value", mapping[5].Column)
	require.Equal(t, "real", mapping[5].DataType)
	require.Equal(t, "5", mapping[5].Properties.Ordinal)

}

func TestNormalize(t *testing.T) {
	require.Equal(t, "Redis", string(schema.Normalize([]byte("__redis__"))))
	require.Equal(t, "UsedCpuUserChildren", string(schema.Normalize([]byte("used_cpu_user_children"))))
	require.Equal(t, "Host1", string(schema.Normalize([]byte("host_1"))))
	require.Equal(t, "Region", string(schema.Normalize([]byte("region"))))
	require.Equal(t, "JobEtcdRequestLatency75pctlrate5m", string(schema.Normalize([]byte("Job:etcdRequestLatency:75pctlrate5m"))))
	require.Equal(t, "TestLimit", string(schema.Normalize([]byte("Test$limit"))))
	require.Equal(t, "TestRateLimit", string(schema.Normalize([]byte("Test::Rate$limit"))))
}

func TestAppendCSVHeaderWithValidMapping(t *testing.T) {
	mapping := schema.NewMetricsSchema()
	expected := "Timestamp:datetime,SeriesId:long,Labels:dynamic,Value:real\n"
	result := schema.AppendCSVHeader(nil, mapping)
	require.Equal(t, expected, string(result))
}

func TestAppendCSVHeaderWithEmptyMapping(t *testing.T) {
	var mapping schema.SchemaMapping
	expected := "\n"
	result := schema.AppendCSVHeader(nil, mapping)
	require.Equal(t, expected, string(result))
}

func TestAppendCSVHeaderWithCustomMapping(t *testing.T) {
	mapping := schema.SchemaMapping{
		{Column: "CustomColumn1", DataType: "string"},
		{Column: "CustomColumn2", DataType: "int"},
	}
	expected := "CustomColumn1:string,CustomColumn2:int\n"
	result := schema.AppendCSVHeader(nil, mapping)
	require.Equal(t, expected, string(result))
}

func TestUnmarshalSchemaWithValidData(t *testing.T) {
	data := "Timestamp:datetime,SeriesId:long,Labels:dynamic,Value:real\n"
	expected := schema.SchemaMapping{
		{Column: "Timestamp", DataType: "datetime", Properties: struct {
			Ordinal    string `json:"Ordinal,omitempty"`
			ConstValue string `json:"ConstValue,omitempty"`
		}{Ordinal: "0"}},
		{Column: "SeriesId", DataType: "long", Properties: struct {
			Ordinal    string `json:"Ordinal,omitempty"`
			ConstValue string `json:"ConstValue,omitempty"`
		}{Ordinal: "1"}},
		{Column: "Labels", DataType: "dynamic", Properties: struct {
			Ordinal    string `json:"Ordinal,omitempty"`
			ConstValue string `json:"ConstValue,omitempty"`
		}{Ordinal: "2"}},
		{Column: "Value", DataType: "real", Properties: struct {
			Ordinal    string `json:"Ordinal,omitempty"`
			ConstValue string `json:"ConstValue,omitempty"`
		}{Ordinal: "3"}},
	}
	result, err := schema.UnmarshalSchema(data)
	require.NoError(t, err)
	require.Equal(t, expected, result)
}

func TestUnmarshalSchemaWithEmptyData(t *testing.T) {
	data := ""
	expected := schema.SchemaMapping{}
	result, err := schema.UnmarshalSchema(data)
	require.NoError(t, err)
	require.Equal(t, expected, result)
}

func TestUnmarshalSchemaWithInvalidData(t *testing.T) {
	data := "Timestamp:datetime,SeriesId:long,Labels\n"
	_, err := schema.UnmarshalSchema(data)
	require.Error(t, err)
}

func TestUnmarshalSchemaWithExtraNewline(t *testing.T) {
	data := "Timestamp:datetime,SeriesId:long,Labels:dynamic,Value:real\n\n"
	expected := schema.SchemaMapping{
		{Column: "Timestamp", DataType: "datetime", Properties: struct {
			Ordinal    string `json:"Ordinal,omitempty"`
			ConstValue string `json:"ConstValue,omitempty"`
		}{Ordinal: "0"}},
		{Column: "SeriesId", DataType: "long", Properties: struct {
			Ordinal    string `json:"Ordinal,omitempty"`
			ConstValue string `json:"ConstValue,omitempty"`
		}{Ordinal: "1"}},
		{Column: "Labels", DataType: "dynamic", Properties: struct {
			Ordinal    string `json:"Ordinal,omitempty"`
			ConstValue string `json:"ConstValue,omitempty"`
		}{Ordinal: "2"}},
		{Column: "Value", DataType: "real", Properties: struct {
			Ordinal    string `json:"Ordinal,omitempty"`
			ConstValue string `json:"ConstValue,omitempty"`
		}{Ordinal: "3"}},
	}
	result, err := schema.UnmarshalSchema(data)
	require.NoError(t, err)
	require.Equal(t, expected, result)
}

func TestUnmarshalSchemaWithMissingDataType(t *testing.T) {
	data := "Timestamp:datetime,SeriesId:long,Labels:dynamic,Value\n"
	_, err := schema.UnmarshalSchema(data)
	require.Error(t, err)
}
