package adx

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/davidnarayan/go-flake"
	"github.com/stretchr/testify/require"
)

func TestUploader(t *testing.T) {
	// Create some mocks
	idgent, err := flake.New()
	require.NoError(t, err)

	dir := t.TempDir()
	segmentReader := func(path string) (io.ReadCloser, error) {
		return os.Open(path)
	}

	// Set the stage
	var (
		scenario = map[string]map[string]int{
			"database1": {
				"table1": 2,
				"table2": 1,
			},
			"database2": {
				"table1": 1,
			},
		}
		paths       []string
		uploaders   []Uploader
		concurrency = 1
	)
	for database, tables := range scenario {
		uploaders = append(uploaders, NewFakeUploader(database))
		for table, count := range tables {
			for i := 0; i < count; i++ {
				now := idgent.NextId()
				path := filepath.Join(dir, wal.Filename(database, table, now.String()))
				f, err := os.Create(path)
				require.NoError(t, err)
				f.WriteString(fmt.Sprintf("%s,%s,%d\n", database, table, i))
				err = f.Close()
				require.NoError(t, err)
				paths = append(paths, path)
			}
		}
	}

	// Run the test
	ctx := context.Background()
	coordinator := NewCoordinator(uploaders, segmentReader, concurrency)
	err = coordinator.Open(ctx)
	require.NoError(t, err)

	// Process our paths
	coordinator.handlePaths(ctx, paths)
	// We expect all files to be uploaded
	require.Equal(t, 0, len(coordinator.activeFiles))
	// Ensure all database::table combinations were uploaded
	have := make(map[string]map[string]int)
	for _, uploader := range uploaders {
		u := uploader.(*fakeUploader)
		for db, tb := range u.observer {
			have[db] = tb
		}
	}
	require.Equal(t, len(scenario), len(have))
	for database, tables := range have {
		require.Equal(t, len(scenario[database]), len(tables))
	}
}

func BenchmarkUploader(b *testing.B) {
	idgent, err := flake.New()
	require.NoError(b, err)
	uploader := NewFakeUploader("database")
	dir := b.TempDir()
	segmentReader := func(path string) (io.ReadCloser, error) {
		return os.Open(path)
	}
	now := idgent.NextId()
	path := filepath.Join(dir, wal.Filename("database", "table", now.String()))
	f, err := os.Create(path)
	require.NoError(b, err)
	f.WriteString("database,table")
	err = f.Close()
	require.NoError(b, err)

	ctx := context.Background()
	coordinator := NewCoordinator([]Uploader{uploader}, segmentReader, 1)
	err = coordinator.Open(ctx)
	require.NoError(b, err)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		coordinator.handlePaths(ctx, []string{path})
	}
}
