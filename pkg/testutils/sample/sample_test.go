package sample_test

import (
	"context"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/testutils/sample"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

func TestSample(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode")
	}

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(10*time.Minute))
	defer cancel()

	c, err := sample.Run(ctx, sample.WithStarted())
	testcontainers.CleanupContainer(t, c)
	require.NoError(t, err)
}
