package storage_test

import (
	"encoding/json"
	"testing"

	"github.com/Azure/adx-mon/ingestor/storage"
	"github.com/stretchr/testify/require"
)

func TestNewSchema_NoLabels(t *testing.T) {
	mapping := storage.DefaultMapping
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

func TestNewSchema_AddDimension(t *testing.T) {
	storage.AddDefaultMapping("Region", "eastus")
	mapping := storage.DefaultMapping

	_, err := json.Marshal(mapping)
	require.NoError(t, err)

	require.Equal(t, len(mapping), 5)
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

	require.Equal(t, "Region", mapping[4].Column)
	require.Equal(t, "string", mapping[4].DataType)
	require.Equal(t, "4", mapping[4].Properties.Ordinal)
	require.Equal(t, "eastus", mapping[4].Properties.ConstValue)

}
