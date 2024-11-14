package collector_test

import (
	"context"
	"testing"

	"github.com/Azure/adx-mon/pkg/testutils/collector"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

func TestCollector(t *testing.T) {
	c, err := collector.Run(context.Background())
	testcontainers.CleanupContainer(t, c)
	require.NoError(t, err)
}
