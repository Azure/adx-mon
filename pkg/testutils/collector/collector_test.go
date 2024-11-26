//go:build !disableDocker

package collector_test

import (
	"context"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/testutils/collector"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

func TestCollector(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	c, err := collector.Run(ctx)
	testcontainers.CleanupContainer(t, c)
	require.NoError(t, err)
}
