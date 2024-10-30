package journal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCursorPath(t *testing.T) {
	t.Run("sanitizes", func(t *testing.T) {
		cursorPath := cursorPath("cursorDirectory", []string{"filter1", "filter2"}, "database/20", "desttable")
		require.NotEmpty(t, cursorPath)

		require.Equal(t, "cursorDirectory", filepath.Dir(cursorPath), "no sub directories")
		baseName := filepath.Base(cursorPath)
		require.True(t, strings.HasPrefix(baseName, "journal_database_20_desttable_"))
	})

	t.Run("consistent", func(t *testing.T) {
		pathOne := cursorPath("cursorDirectory", []string{"filter1", "filter2"}, "destdatabase", "desttable")
		pathTwo := cursorPath("cursorDirectory", []string{"filter1", "filter2"}, "destdatabase", "desttable")
		require.Equal(t, pathOne, pathTwo)

		pathOne = cursorPath("cursorDirectory", nil, "destdatabase", "desttable")
		pathTwo = cursorPath("cursorDirectory", nil, "destdatabase", "desttable")
		require.Equal(t, pathOne, pathTwo)
	})

	t.Run("no filter", func(t *testing.T) {
		cPath := cursorPath("cursorDirectory", nil, "destdatabase", "desttable")
		require.NotEmpty(t, cPath)

		require.Equal(t, "cursorDirectory", filepath.Dir(cPath), "no sub directories")
		baseName := filepath.Base(cPath)
		require.True(t, strings.HasPrefix(baseName, "journal_destdatabase_desttable_"))
	})

	t.Run("varies by filter,db,table", func(t *testing.T) {
		controlPath := cursorPath("cursorDirectory", []string{"filter1", "filter2"}, "destdatabase", "desttable")
		testPath := cursorPath("cursorDirectory", []string{"filter1", "filter3", "filter2"}, "destdatabase", "desttable")
		require.NotEqual(t, controlPath, testPath)

		testPath = cursorPath("cursorDirectory", []string{"filter2"}, "destdatabase", "desttable")
		require.NotEqual(t, controlPath, testPath)

		// different order of filter
		testPath = cursorPath("cursorDirectory", []string{"filter2", "filter1"}, "destdatabase", "desttable")
		require.NotEqual(t, controlPath, testPath)

		// different table
		testPath = cursorPath("cursorDirectory", []string{"filter1", "filter2"}, "destdatabase", "secondtable")
		require.NotEqual(t, controlPath, testPath)

		// different db
		testPath = cursorPath("cursorDirectory", []string{"filter1", "filter2"}, "otherdatabase", "desttable")
		require.NotEqual(t, controlPath, testPath)
	})
}

func TestReadWriteCursor(t *testing.T) {
	t.Run("write empty cursor", func(t *testing.T) {
		path := t.TempDir()
		cursorPath := filepath.Join(path, "cursor")
		writeCursor(cursorPath, "")
		// no panic, no write

		_, err := os.Stat(cursorPath)
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("write empty cursor path", func(t *testing.T) {
		writeCursor("", "cursor")
		// no panic
	})

	t.Run("writes cursor", func(t *testing.T) {
		path := t.TempDir()
		cursorPath := filepath.Join(path, "cursor")
		writeCursor(cursorPath, "0xdeadbeef")

		cursor, err := readCursor(cursorPath)
		require.NoError(t, err)
		require.Equal(t, "0xdeadbeef", cursor)
	})

	t.Run("read empty cursor", func(t *testing.T) {
		path := t.TempDir()
		cursorPath := filepath.Join(path, "cursor")
		cursor, err := readCursor(cursorPath)
		require.Error(t, err)
		require.Empty(t, cursor)
	})

	t.Run("read empty cursor file", func(t *testing.T) {
		path := t.TempDir()
		cursorPath := filepath.Join(path, "cursor")
		f, err := os.Create(cursorPath)
		require.NoError(t, err)
		defer f.Close()

		cursor, err := readCursor(cursorPath)
		require.Error(t, err)
		require.Empty(t, cursor)
	})

	t.Run("read invalid cursor file", func(t *testing.T) {
		path := t.TempDir()
		cursorPath := filepath.Join(path, "cursor")
		f, err := os.Create(cursorPath)
		require.NoError(t, err)
		f.Write([]byte("invalid json"))
		f.Close()

		cursor, err := readCursor(cursorPath)
		require.Error(t, err)
		require.Empty(t, cursor)
	})
}

func TestClean(t *testing.T) {
	t.Run("empty cursor path", func(t *testing.T) {
		cleanCursor("")
		// no panic
	})

	t.Run("clean cursor", func(t *testing.T) {
		path := t.TempDir()
		cursorPath := filepath.Join(path, "cursor")
		writeCursor(cursorPath, "0xdeadbeef")

		cleanCursor(cursorPath)
		_, err := os.Stat(cursorPath)
		require.ErrorIs(t, err, os.ErrNotExist)
	})
}
