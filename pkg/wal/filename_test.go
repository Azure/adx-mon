package wal_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/davidnarayan/go-flake"
	"github.com/stretchr/testify/require"
)

func TestListDir(t *testing.T) {
	var expect []string
	dir := t.TempDir()
	idgen, err := flake.New()
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		epoch := idgen.NextId()
		database := fmt.Sprintf("database-%d", i)
		table := fmt.Sprintf("table-%d", i)
		expect = append(expect, wal.Filename(database, table, epoch.String()))
		f, err := os.Create(filepath.Join(dir, expect[i]))
		require.NoError(t, err)
		_, err = f.WriteString(strconv.Itoa(i))
		require.NoError(t, err)
		err = f.Close()
		require.NoError(t, err)
	}

	have, err := wal.ListDir(dir)
	require.NoError(t, err)
	require.Equal(t, len(expect), len(have))
}
