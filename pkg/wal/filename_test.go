package wal_test

import (
	"testing"

	"github.com/Azure/adx-mon/pkg/wal"

	"github.com/stretchr/testify/require"
)

func TestFilenameWithValidInput(t *testing.T) {
	database := "testdb"
	table := "testtable"
	schema := "testschema"
	epoch := "1234567890"
	expected := "testdb_testtable_testschema_1234567890.wal"
	result := wal.Filename(database, table, schema, epoch)
	require.Equal(t, expected, result)
}

func TestParseFilenameWithValidInput(t *testing.T) {
	path := "testdb_testtable_testschema_1234567890.wal"
	database, table, schema, epoch, err := wal.ParseFilename(path)
	require.NoError(t, err)
	require.Equal(t, "testdb", database)
	require.Equal(t, "testtable", table)
	require.Equal(t, "testschema", schema)
	require.Equal(t, "1234567890", epoch)
}

func TestParseFilenameWithMissingSchema(t *testing.T) {
	path := "testdb_testtable_1234567890.wal"
	database, table, schema, epoch, err := wal.ParseFilename(path)
	require.NoError(t, err)
	require.Equal(t, "testdb", database)
	require.Equal(t, "testtable", table)
	require.Equal(t, "", schema)
	require.Equal(t, "1234567890", epoch)
}

func TestParseFilenameWithInvalidExtension(t *testing.T) {
	path := "testdb_testtable_testschema_1234567890.txt"
	_, _, _, _, err := wal.ParseFilename(path)
	require.ErrorIs(t, err, wal.ErrNotWALSegment)
}

func TestParseFilenameWithInvalidFormat(t *testing.T) {
	path := "invalid_filename.wal"
	_, _, _, _, err := wal.ParseFilename(path)
	require.ErrorIs(t, err, wal.ErrInvalidWALSegment)
}

func TestParseFilenameMissingParts(t *testing.T) {
	for _, path := range []string{
		"_table_schema_epoch.wal",
		"db__schema_epoch.wal",
		"db_table__epoch.wal",
		"db_table_schema_.wal",
		"db_table_.wal",
		"__.wal",
		"db__.wal",
		"db_table_.wal",
		"db_table_epoch.txt",
		"db_table_schema_epoch.zip",
	} {
		t.Run(path, func(t *testing.T) {
			_, _, _, _, err := wal.ParseFilename(path)
			require.NotNil(t, err)
		})
	}
}
