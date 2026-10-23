package testutils

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

const containerExitEventError = "did not receive an exit event"

// CleanupContainer retries termination when Docker times out waiting for a
// container exit event, then removes the container after it has stopped.
func CleanupContainer(tb testing.TB, ctr testcontainers.Container) {
	tb.Helper()

	tb.Cleanup(func() {
		require.NoError(tb, terminateWithRetry(ctr))
	})
}

func terminateWithRetry(ctr testcontainers.Container) error {
	terminate := func() error { return testcontainers.TerminateContainer(ctr) }
	err := terminate()
	if err == nil || !strings.Contains(err.Error(), containerExitEventError) {
		return err
	}
	if err := waitForContainerStopped(ctr); err != nil {
		return fmt.Errorf("waiting for container to stop after termination timeout: %w", err)
	}
	return terminate()
}

func waitForContainerStopped(ctr testcontainers.Container) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		state, err := ctr.State(ctx)
		if err != nil {
			return err
		}
		if state == nil || !state.Running {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("container is still running after %s", time.Minute)
		case <-ticker.C:
		}
	}
}
