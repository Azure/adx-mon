package adx

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	kustoerrors "github.com/Azure/azure-kusto-go/kusto/data/errors"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/Azure/adx-mon/pkg/logger"
)

// isThrottled returns true if the error indicates the request was throttled by Kusto.
func isThrottled(err error) bool {
	var he *kustoerrors.HttpError
	if errors.As(err, &he) {
		return he.IsThrottled()
	}
	return false
}

// isHTTP5xx returns true if the error is an HttpError with 5xx status code.
func isHTTP5xx(err error) bool {
	var he *kustoerrors.HttpError
	if errors.As(err, &he) {
		return he.StatusCode >= http.StatusInternalServerError && he.StatusCode <= 599
	}
	return false
}

// isTransientKusto returns true if the error is considered retryable by the SDK Retry predicate.
func isTransientKusto(err error) bool {
	return kustoerrors.Retry(err)
}

// retryMgmt wraps a Kusto management command with exponential backoff using the
// Kubernetes wait utilities. Retries are attempted for:
//   - Throttling (HTTP 429)
//   - HTTP 5xx responses
//   - Transient errors recognized by the Kusto SDK's errors.Retry predicate
//
// Backoff parameters: start 500ms, factor 2.0, jitter 0.25, cap 10s, steps 8 (~1m upper bound).
func retryMgmt(ctx context.Context, desc string, fn func() error) error {
	backoff := wait.Backoff{
		Duration: 500 * time.Millisecond,
		Factor:   2.0,
		Jitter:   0.25,
		Steps:    8,
		Cap:      10 * time.Second,
	}

	var lastErr error
	err := wait.ExponentialBackoffWithContext(ctx, backoff, func(ctx context.Context) (done bool, err error) {
		if ctx.Err() != nil {
			return true, ctx.Err()
		}
		e := fn()
		if e == nil {
			return true, nil
		}
		// Decide retry conditions:
		shouldRetry := isThrottled(e) || isHTTP5xx(e) || isTransientKusto(e)
		if !shouldRetry {
			return true, e
		}
		lastErr = e
		logger.Warnf("Retrying %s due to transient error: %v", desc, e)
		return false, nil
	})
	if err == nil {
		return nil
	}
	if lastErr != nil {
		return fmt.Errorf("%s: exhausted retries: %w", desc, lastErr)
	}
	return fmt.Errorf("%s: %w", desc, err)
}
