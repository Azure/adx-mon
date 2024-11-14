package ingestor_test

import (
	"context"
	"testing"

	"github.com/Azure/adx-mon/pkg/testutils/ingestor"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

func TestIngestor(t *testing.T) {
	c, err := ingestor.Run(context.Background())
	testcontainers.CleanupContainer(t, c)
	require.NoError(t, err)
}
