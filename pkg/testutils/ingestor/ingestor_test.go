package ingestor_test

import (
	"context"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/Azure/adx-mon/pkg/testutils/ingestor"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

func TestIngestor(t *testing.T) {
	testutils.IntegrationTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	c, err := ingestor.Run(ctx)
	testcontainers.CleanupContainer(t, c)
	require.NoError(t, err)
}
