package k8s

import (
	"context"
	"fmt"
	"reflect"

	"github.com/Azure/adx-mon/pkg/logger"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// UpdateWithRetry implements optimistic concurrency control for Kubernetes object updates.
// It retries update operations when conflicts occur by fetching the latest version and
// reapplying the desired changes using the provided callback function.
func UpdateWithRetry(ctx context.Context, c client.Client, obj client.Object, reapplyChanges func(latest client.Object) error) error {
	const maxRetries = 5

	// Create a working copy to avoid modifying the original until we're sure of success
	workingCopy := obj.DeepCopyObject().(client.Object)

	// Apply changes to the working copy for the first update attempt
	if err := reapplyChanges(workingCopy); err != nil {
		return fmt.Errorf("failed to apply initial changes: %w", err)
	}

	for i := 0; i < maxRetries; i++ {
		if err := c.Update(ctx, workingCopy); err != nil {
			if !apierrors.IsConflict(err) {
				// Not a conflict error, return immediately
				return err
			}

			// Conflict error, fetch the latest version and retry
			logger.Infof("Conflict updating %s, retrying (attempt %d/%d)", obj.GetName(), i+1, maxRetries)

			latest := obj.DeepCopyObject().(client.Object)
			if err := c.Get(ctx, client.ObjectKeyFromObject(obj), latest); err != nil {
				return fmt.Errorf("failed to fetch latest object for retry: %w", err)
			}

			// Reapply the desired changes to the latest version
			if err := reapplyChanges(latest); err != nil {
				return fmt.Errorf("failed to reapply changes: %w", err)
			}

			// Use the updated latest version for the next update attempt
			workingCopy = latest
			continue
		}

		// Update succeeded - copy the successful result back to the original object
		objValue := reflect.ValueOf(obj).Elem()
		workingValue := reflect.ValueOf(workingCopy).Elem()
		objValue.Set(workingValue)
		return nil
	}
	return fmt.Errorf("failed to update object after %d retries due to conflicts", maxRetries)
}

// UpdateWithRetryPreserveSpec is a convenience wrapper for UpdateWithRetry that preserves
// the entire Spec field from the current object and applies it to the latest version.
// This covers the common case where you want to preserve all spec changes.
func UpdateWithRetryPreserveSpec[T client.Object, S any](ctx context.Context, c client.Client, obj T, getSpec func(T) S, setSpec func(T, S)) error {
	// Capture the desired spec before any retries
	preservedSpec := getSpec(obj)

	return UpdateWithRetry(ctx, c, obj, func(latest client.Object) error {
		latestTyped, ok := latest.(T)
		if !ok {
			return fmt.Errorf("unexpected latest object type: %T", latest)
		}

		// Apply the preserved spec to the latest version
		setSpec(latestTyped, preservedSpec)
		return nil
	})
}
