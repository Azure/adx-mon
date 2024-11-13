package testutils_test

import (
	"context"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/stretchr/testify/require"
)

func TestLogSampleOTLP(t *testing.T) {
	var (
		waitFor  = 10 * time.Minute
		database = "Logs"
		table    = "Sample"
		uri      = TestCluster.KustoConnectionURL()
	)
	ctx, cancel := context.WithTimeout(context.Background(), waitFor)
	defer cancel()

	require.Eventually(t, func() bool {
		return testutils.TableExists(ctx, t, database, table, uri)
	}, waitFor, 10*time.Second)

	require.Eventually(t, func() bool {
		return testutils.TableHasRows(ctx, t, database, table, uri)
	}, waitFor, time.Second)
}
