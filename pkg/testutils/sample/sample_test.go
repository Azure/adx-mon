package sample_test

import (
	"context"
	"testing"

	"github.com/Azure/adx-mon/pkg/testutils/sample"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

func TestSample(t *testing.T) {
	c, err := sample.Run(context.Background(), sample.WithStarted())
	testcontainers.CleanupContainer(t, c)
	require.NoError(t, err)
}
