package limiter_test

import (
	"context"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/limiter"
	"github.com/stretchr/testify/require"
)

func TestNewFixed(t *testing.T) {
	limit := 5
	l := limiter.NewFixed(limit)
	require.Equal(t, limit, l.Capacity())
	require.True(t, l.Idle())
}

func TestFixedIdle(t *testing.T) {
	l := limiter.NewFixed(2)
	require.True(t, l.Idle())

	l.Take(context.Background())
	require.False(t, l.Idle())

	l.Release()
	require.True(t, l.Idle())
}

func TestFixedAvailable(t *testing.T) {
	l := limiter.NewFixed(3)
	require.Equal(t, 3, l.Available())

	l.Take(context.Background())
	require.Equal(t, 2, l.Available())

	l.Release()
	require.Equal(t, 3, l.Available())
}

func TestFixedTryTake(t *testing.T) {
	l := limiter.NewFixed(1)
	require.True(t, l.TryTake())
	require.False(t, l.TryTake())

	l.Release()
	require.True(t, l.TryTake())
}

func TestFixedTake(t *testing.T) {
	l := limiter.NewFixed(1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	require.NoError(t, l.Take(ctx))
	require.Error(t, l.Take(ctx))

	l.Release()
	require.NoError(t, l.Take(context.Background()))
}

func TestFixedRelease(t *testing.T) {
	l := limiter.NewFixed(1)
	require.True(t, l.TryTake())
	l.Release()
	require.True(t, l.TryTake())
}
