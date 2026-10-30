package testutils

import (
	"context"
	"errors"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

type cleanupTestContainer struct {
	testcontainers.Container
	terminateErrs  []error
	terminateCalls int
	states         []*container.State
	stateErr       error
	stateCalls     int
	terminatedAt   []int
}

func (c *cleanupTestContainer) Terminate(context.Context, ...testcontainers.TerminateOption) error {
	c.terminateCalls++
	c.terminatedAt = append(c.terminatedAt, c.stateCalls)
	if len(c.terminateErrs) >= c.terminateCalls {
		return c.terminateErrs[c.terminateCalls-1]
	}
	return nil
}

func (c *cleanupTestContainer) State(context.Context) (*container.State, error) {
	c.stateCalls++
	if c.stateErr != nil {
		return nil, c.stateErr
	}
	if len(c.states) >= c.stateCalls {
		return c.states[c.stateCalls-1], nil
	}
	return &container.State{Running: false}, nil
}

func TestTerminateWithRetry(t *testing.T) {
	t.Run("retries after Docker misses the exit event", func(t *testing.T) {
		ctr := &cleanupTestContainer{
			terminateErrs: []error{
				errors.New("container stop: tried to kill container, but did not receive an exit event"),
			},
			states: []*container.State{{Running: true}, {Running: false}},
		}

		require.NoError(t, terminateWithRetry(ctr))
		require.Equal(t, 2, ctr.terminateCalls)
		require.Equal(t, []int{0, 2}, ctr.terminatedAt)
		require.Equal(t, 2, ctr.stateCalls)
	})

	t.Run("reports container inspection errors", func(t *testing.T) {
		wantErr := errors.New("docker inspection failed")
		ctr := &cleanupTestContainer{
			terminateErrs: []error{errors.New("container stop: did not receive an exit event")},
			stateErr:      wantErr,
		}

		require.ErrorIs(t, terminateWithRetry(ctr), wantErr)
		require.Equal(t, 1, ctr.terminateCalls)
	})

	t.Run("does not retry other errors", func(t *testing.T) {
		wantErr := errors.New("container removal failed")
		ctr := &cleanupTestContainer{terminateErrs: []error{wantErr}}

		require.ErrorIs(t, terminateWithRetry(ctr), wantErr)
		require.Equal(t, 1, ctr.terminateCalls)
	})
}
