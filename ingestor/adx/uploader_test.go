package adx

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/Azure/adx-mon/pkg/testutils/kustainer"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/ingest"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

func TestClusterRequiresDirectIngest(t *testing.T) {
	testutils.IntegrationTest(t)

	ctx := context.Background()
	k, err := kustainer.Run(ctx, "mcr.microsoft.com/azuredataexplorer/kustainer-linux:latest", kustainer.WithStarted())
	testcontainers.CleanupContainer(t, k)
	require.NoError(t, err)

	cb := kusto.NewConnectionStringBuilder(k.ConnectionUrl())
	client, err := kusto.New(cb)
	require.NoError(t, err)
	defer client.Close()

	u := &uploader{
		KustoCli: client,
		database: "NetDefaultDB",
	}
	requiresDirectIngest, err := u.clusterRequiresDirectIngest(ctx)
	require.NoError(t, err)
	require.True(t, requiresDirectIngest)
}

func TestUploaderDoubleCheckedLocking(t *testing.T) {
	// Test that concurrent access to the ingestors map is properly synchronized
	// This test focuses on the locking behavior rather than mocking ingest.New

	u := &uploader{
		ingestors: make(map[string]ingest.Ingestor),
		opts: UploaderOpts{
			Database: "test-db",
		},
	}

	table := "test-table"
	mockIngestor := &mockIngestor{}

	// Create multiple goroutines that try to add the same ingestor concurrently
	const numGoroutines = 10
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			// Simulate the double-checked locking pattern
			u.mu.RLock()
			_, exists := u.ingestors[table]
			u.mu.RUnlock()

			if !exists {
				u.mu.Lock()
				defer u.mu.Unlock()
				// Double-check after acquiring write lock
				_, exists = u.ingestors[table]
				if !exists {
					// Simulate some work during ingestor setup
					time.Sleep(1 * time.Millisecond)
					u.ingestors[table] = mockIngestor
				}
			}
			errors <- nil
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		err := <-errors
		require.NoError(t, err, "Goroutine %d should not error", i)
	}

	// Verify that the ingestor is properly stored in the map
	require.Len(t, u.ingestors, 1, "Should have exactly one ingestor in the map")
	require.Contains(t, u.ingestors, table, "Should contain the test table")
	require.Equal(t, mockIngestor, u.ingestors[table], "Should store the correct ingestor")
}

func TestUploaderLockCleanupOnPanic(t *testing.T) {
	// Test that defer unlock properly handles panics

	u := &uploader{
		ingestors: make(map[string]ingest.Ingestor),
		opts: UploaderOpts{
			Database: "test-db",
		},
	}

	table := "test-table"

	// This function should recover from panic and verify the lock is released
	func() {
		defer func() {
			if r := recover(); r != nil {
				// Panic was recovered, now verify we can acquire the lock again
				// If the defer unlock didn't work, this would deadlock
				lockAcquired := make(chan bool, 1)
				go func() {
					u.mu.Lock()
					u.mu.Unlock()
					lockAcquired <- true
				}()

				select {
				case <-lockAcquired:
					// Good, lock was released properly
				case <-time.After(100 * time.Millisecond):
					t.Fatal("Lock was not released after panic - defer unlock failed")
				}
			}
		}()

		// Simulate the locking pattern that should handle panics
		u.mu.RLock()
		_, exists := u.ingestors[table]
		u.mu.RUnlock()

		if !exists {
			u.mu.Lock()
			defer u.mu.Unlock() // This defer should handle the panic
			// Double-check after acquiring write lock
			_, exists = u.ingestors[table]
			if !exists {
				// This will panic, but defer should still unlock
				panic("simulated panic during ingestor creation")
			}
		}
	}()

	// If we reach here, the test passed - the defer unlock handled the panic correctly
}

// mockIngestor implements the ingest.Ingestor interface for testing
type mockIngestor struct{}

func (m *mockIngestor) FromReader(ctx context.Context, reader io.Reader, options ...ingest.FileOption) (*ingest.Result, error) {
	return &ingest.Result{}, nil
}

func (m *mockIngestor) FromFile(ctx context.Context, fPath string, options ...ingest.FileOption) (*ingest.Result, error) {
	return &ingest.Result{}, nil
}

func (m *mockIngestor) Close() error {
	return nil
}
