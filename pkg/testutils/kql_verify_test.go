//go:build !disableDocker

package testutils_test

import (
	"context"
	"testing"

	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/Azure/adx-mon/pkg/testutils/kustainer"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

func TestTableExists(t *testing.T) {
	k, err := kustainer.Run(context.Background(), "mcr.microsoft.com/azuredataexplorer/kustainer-linux:latest", kustainer.WithStarted())
	testcontainers.CleanupContainer(t, k)
	require.NoError(t, err)

	require.False(t, testutils.TableExists(context.Background(), t, "NetDefaultDB", "Foo", k.ConnectionUrl()))
	require.True(t, testutils.TableExists(context.Background(), t, "NetDefaultDB", "Table_0", k.ConnectionUrl()))
}

func TestTableHasRows(t *testing.T) {
	k, err := kustainer.Run(context.Background(), "mcr.microsoft.com/azuredataexplorer/kustainer-linux:latest", kustainer.WithStarted())
	testcontainers.CleanupContainer(t, k)
	require.NoError(t, err)

	require.False(t, testutils.TableHasRows(context.Background(), t, "NetDefaultDB", "Table_0", k.ConnectionUrl()))
}
