package rules

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	alertrulev1 "github.com/Azure/adx-mon/api/v1"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	clocktesting "k8s.io/utils/clock/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type countingClient struct {
	client.Client
	mu    sync.Mutex
	lists int
	gate  <-chan struct{}
}

func (c *countingClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	c.mu.Lock()
	c.lists++
	n := c.lists
	c.mu.Unlock()
	if n == 2 {
		select {
		case <-c.gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return c.Client.List(ctx, list, opts...)
}

func (c *countingClient) count() int { c.mu.Lock(); defer c.mu.Unlock(); return c.lists }

func TestStore_ReloadPeriodically_BoundedCadence(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		clk := clocktesting.NewFakeClock(time.Now())
		scheme := runtime.NewScheme()
		require.NoError(t, alertrulev1.AddToScheme(scheme))
		gate := make(chan struct{})
		release := sync.OnceFunc(func() { close(gate) })
		ctrlCli := &countingClient{Client: fake.NewClientBuilder().WithScheme(scheme).Build(), gate: gate}
		store := NewStore(StoreOpts{Region: "eastus", CtrlCli: ctrlCli, Clock: clk})
		require.NoError(t, store.Open(context.Background()))
		defer func() { release(); _ = store.Close() }()
		synctest.Wait()
		require.Equal(t, 1, clk.Waiters(), "reload must use the injected clock")
		require.Equal(t, 1, ctrlCli.count(), "initial reload")
		clk.Step(time.Minute)
		synctest.Wait()
		require.Equal(t, 2, ctrlCli.count(), "periodic reload is gated in List")
		clk.Step(3 * time.Minute)
		release()
		synctest.Wait()
		require.Equal(t, 3, ctrlCli.count(), "a pending ticker event runs immediately after the slow reload")
		clk.Step(time.Minute)
		synctest.Wait()
		require.Equal(t, 4, ctrlCli.count(), "normal cadence resumes without a fresh delay")
		require.NoError(t, store.Close())
		clk.Step(time.Minute)
		synctest.Wait()
		require.Equal(t, 4, ctrlCli.count(), "no reloads after Close joins the loop")
	})
}
