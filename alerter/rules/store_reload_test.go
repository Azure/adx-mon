package rules

import (
	"context"
	"sync"
	"testing"
	"time"

	alertrulev1 "github.com/Azure/adx-mon/api/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// countingClient wraps a client.Client and counts List calls, optionally
// blocking every call for a controlled duration to model a slow apiserver.
type countingClient struct {
	client.Client

	mu       sync.Mutex
	lists    int
	blockDur time.Duration
}

func (c *countingClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	c.mu.Lock()
	c.lists++
	block := c.blockDur
	c.mu.Unlock()

	if block > 0 {
		select {
		case <-time.After(block):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return c.Client.List(ctx, list, opts...)
}

func (c *countingClient) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lists
}

// TestStore_ReloadPeriodically_BoundedCadence demonstrates that the periodic
// rule reload keeps a bounded cadence even when reloadRules is slow, by using a
// single reusable ticker instead of calling time.After on every iteration.
//
// Before the fix, each loop iteration called time.After(reloadInterval). When a
// reload takes longer than the interval, the loop waits a full additional
// interval after the slow reload completes, so the reload cadence drifts: N
// slow reloads take roughly N * (slow + interval). With a ticker, the ticker
// keeps ticking in the background while a reload is in flight, so after a slow
// reload the next reload happens promptly (catch-up) and N slow reloads take
// roughly N * slow.
//
// The test makes every reload slow (blockDur > reloadInterval) and measures the
// time for several reloads. The bound is tight enough that the drifting
// time.After cadence exceeds it (the test fails before the fix) while the
// ticker cadence stays within it (the test passes after the fix).
func TestStore_ReloadPeriodically_BoundedCadence(t *testing.T) {
	orig := reloadInterval
	reloadInterval = 30 * time.Millisecond
	defer func() { reloadInterval = orig }()

	const (
		// slow == reloadInterval. With a ticker, a slow reload that lasts
		// exactly one interval means the next tick is due when the reload
		// finishes, so reloads chain back-to-back (~slow each). With
		// time.After, each reload is followed by a full additional interval
		// wait (~slow + interval each).
		slow  = 30 * time.Millisecond
		wantN = 6                      // total List calls (initial + 5 periodic)
		bound = 225 * time.Millisecond // ~ (wantN-1)*slow with a ticker; time.After drifts past this
	)

	scheme := runtime.NewScheme()
	require.NoError(t, alertrulev1.AddToScheme(scheme))

	rule := &alertrulev1.AlertRule{
		Namespace: "foo", Name: "valid",
		Spec: alertrulev1.AlertRuleSpec{
			Database:    "SomeDB",
			Interval:    metav1.Duration{Duration: time.Minute},
			Query:       "BadThings",
			Destination: "peoplewhocare",
		},
	}
	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(rule).Build()
	ctrlCli := &countingClient{Client: base, blockDur: slow}

	store := NewStore(StoreOpts{Region: "eastus", CtrlCli: ctrlCli})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	start := time.Now()
	require.NoError(t, store.Open(ctx))
	defer func() { _ = store.Close() }()

	// Wait for wantN total List calls (1 initial + wantN-1 periodic reloads).
	// With the fix (ticker) this completes in ~ (wantN-1)*slow + overhead.
	// Before the fix (time.After) each periodic reload adds an extra
	// reloadInterval of drift, exceeding the bound.
	require.Eventually(t, func() bool { return ctrlCli.count() >= wantN },
		bound, time.Millisecond,
		"rule reloads drifted: %d reloads not reached within %s (time.After cadence drift)",
		wantN, bound)

	elapsed := time.Since(start)
	require.LessOrEqual(t, elapsed, bound,
		"reloads took %s, exceeding the bounded-cadence bound %s; the periodic reload is drifting (time.After)",
		elapsed, bound)
	_ = ctx
}
