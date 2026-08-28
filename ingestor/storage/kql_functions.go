package storage

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/scheduler"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
)

const (
	// name of our custom finalizer
	FinalizerName = "function.adx-mon.azure.com/finalizer"
)

// ErrNotLeader indicates that Function listing was skipped because this instance is not the leader.
var ErrNotLeader = errors.New("not leader")

type Functions interface {
	UpdateStatus(ctx context.Context, fn *adxmonv1.Function) error
	Update(ctx context.Context, fn *adxmonv1.Function) error
	List(ctx context.Context, opts ListOptions) ([]*adxmonv1.Function, error)
	UpdateCondition(ctx context.Context, fn *adxmonv1.Function, condition metav1.Condition) error
}

type ListOptions struct {
	IncludeCriteriaMismatches bool
}

type functions struct {
	Client  client.Client
	Elector scheduler.Elector
}

func NewFunctions(client client.Client, elector scheduler.Elector) *functions {
	return &functions{
		Client:  client,
		Elector: elector,
	}
}

func (f *functions) Update(ctx context.Context, fn *adxmonv1.Function) error {
	if f.Client == nil {
		return errors.New("no client provided")
	}

	if err := f.Client.Update(ctx, fn); err != nil {
		logger.Errorf("Failed to update function %s: %v", fn.Name, err)
		return err
	}

	return nil
}

func (f *functions) UpdateStatus(ctx context.Context, fn *adxmonv1.Function) error {
	if f.Client == nil {
		return errors.New("no client provided")
	}
	if fn == nil {
		return errors.New("function cannot be nil")
	}

	if fn.Status.Status == adxmonv1.Success {
		fn.Status.ObservedGeneration = fn.GetGeneration()
		fn.Status.Error = ""

		if !fn.DeletionTimestamp.IsZero() && controllerutil.ContainsFinalizer(fn, FinalizerName) {
			if err := f.removeFinalizer(ctx, fn); err != nil {
				logger.Errorf("Failed to remove finalizer from function %s: %v", fn.Name, err)
				fn.Status.Status = adxmonv1.Failed
			} else {
				return nil
			}
		}
	}

	// Also update ObservedGeneration for PermanentFailure to prevent re-processing the same generation.
	// The function will be re-processed when the user updates the CRD (new generation).
	if fn.Status.Status == adxmonv1.PermanentFailure {
		fn.Status.ObservedGeneration = fn.GetGeneration()
	}

	fn.Status.LastTimeReconciled = metav1.Now()
	if logger.IsDebug() {
		for _, condition := range fn.Status.Conditions {
			logger.Debugf("Function %s/%s condition %s status=%s reason=%s message=%s", fn.Namespace, fn.Name, condition.Type, condition.Status, condition.Reason, condition.Message)
		}
	}

	desiredStatus := *fn.Status.DeepCopy()
	evaluatedGeneration := fn.GetGeneration()
	key := client.ObjectKeyFromObject(fn)
	var persisted *adxmonv1.Function

	var lastConflict error
	err := wait.ExponentialBackoffWithContext(ctx, functionUpdateBackoff(), func(ctx context.Context) (bool, error) {
		latest := &adxmonv1.Function{}
		if err := f.Client.Get(ctx, key, latest); err != nil {
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}

		if latest.GetGeneration() != evaluatedGeneration || functionStatusesEqual(latest.Status, desiredStatus) {
			persisted = latest
			return true, nil
		}

		latest.Status = *desiredStatus.DeepCopy()
		if err := f.Client.Status().Update(ctx, latest); err != nil {
			if apierrors.IsConflict(err) {
				lastConflict = err
				return false, nil
			}
			return false, err
		}
		persisted = latest
		return true, nil
	})
	if err != nil {
		if lastConflict != nil && errors.Is(err, wait.ErrWaitTimeout) {
			return lastConflict
		}
		return err
	}
	if persisted != nil {
		fn.ResourceVersion = persisted.ResourceVersion
		fn.Status = *persisted.Status.DeepCopy()
	}
	return nil
}

func (f *functions) removeFinalizer(ctx context.Context, fn *adxmonv1.Function) error {
	key := client.ObjectKeyFromObject(fn)
	var persisted *adxmonv1.Function
	var lastConflict error
	err := wait.ExponentialBackoffWithContext(ctx, functionUpdateBackoff(), func(ctx context.Context) (bool, error) {
		latest := &adxmonv1.Function{}
		if err := f.Client.Get(ctx, key, latest); err != nil {
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		if !controllerutil.ContainsFinalizer(latest, FinalizerName) {
			persisted = latest
			return true, nil
		}

		controllerutil.RemoveFinalizer(latest, FinalizerName)
		if err := f.Client.Update(ctx, latest); err != nil {
			if apierrors.IsConflict(err) {
				lastConflict = err
				return false, nil
			}
			return false, err
		}
		persisted = latest
		return true, nil
	})
	if err != nil {
		if lastConflict != nil && errors.Is(err, wait.ErrWaitTimeout) {
			return lastConflict
		}
		return err
	}
	if persisted != nil {
		fn.ResourceVersion = persisted.ResourceVersion
		fn.Finalizers = append([]string(nil), persisted.Finalizers...)
	}
	return nil
}

func functionUpdateBackoff() wait.Backoff {
	return wait.Backoff{
		Duration: 10 * time.Millisecond,
		Factor:   1,
		Jitter:   0.1,
		Steps:    4,
	}
}

func functionStatusesEqual(a, b adxmonv1.FunctionStatus) bool {
	// Ignore reconciliation timestamps while comparing meaningful status fields,
	// including ObservedGeneration, to avoid writes caused only by clock changes.
	aCopy := a.DeepCopy()
	bCopy := b.DeepCopy()
	aCopy.LastTimeReconciled = metav1.Time{}
	bCopy.LastTimeReconciled = metav1.Time{}
	for i := range aCopy.Conditions {
		aCopy.Conditions[i].LastTransitionTime = metav1.Time{}
	}
	for i := range bCopy.Conditions {
		bCopy.Conditions[i].LastTransitionTime = metav1.Time{}
	}
	return reflect.DeepEqual(aCopy, bCopy)
}

func (f *functions) UpdateCondition(ctx context.Context, fn *adxmonv1.Function, condition metav1.Condition) error {
	if f.Client == nil {
		return errors.New("no client provided")
	}
	if fn == nil {
		return errors.New("function cannot be nil")
	}
	existing := meta.FindStatusCondition(fn.Status.Conditions, condition.Type)
	if condition.ObservedGeneration == 0 {
		condition.ObservedGeneration = fn.GetGeneration()
	}
	if condition.LastTransitionTime.IsZero() {
		condition.LastTransitionTime = metav1.Now()
	}
	meta.SetStatusCondition(&fn.Status.Conditions, condition)
	logConditionStatusUpdate(fn, existing, condition)
	return f.UpdateStatus(ctx, fn)
}

func (f *functions) List(ctx context.Context, opts ListOptions) ([]*adxmonv1.Function, error) {
	if f.Client == nil {
		return nil, fmt.Errorf("no client provided")
	}

	if f.Elector != nil && !f.Elector.IsLeader() {
		return nil, ErrNotLeader
	}

	list := &adxmonv1.FunctionList{}
	if err := f.Client.List(ctx, list); err != nil {
		if errors.Is(err, &meta.NoKindMatchError{}) || errors.Is(err, &meta.NoResourceMatchError{}) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to list functions: %w", err)
	}
	var fns []*adxmonv1.Function
	for _, fn := range list.Items {
		if fn.Spec.Suspend != nil && *fn.Spec.Suspend {
			// Skip suspended functions
			continue
		}

		if !fn.GetDeletionTimestamp().IsZero() {
			fn.Status.Reason = "Function deleted"

		} else if fn.GetGeneration() == fn.Status.ObservedGeneration {
			if !opts.IncludeCriteriaMismatches || !criteriaNotMatched(&fn) {
				continue
			}
		} else {
			switch fn.GetGeneration() {
			case 1:
				fn.Status.Reason = "Function created"
			default:
				fn.Status.Reason = "Function updated"
			}
		}

		fns = append(fns, &fn)
	}

	return fns, nil
}

func criteriaNotMatched(fn *adxmonv1.Function) bool {
	condition := meta.FindStatusCondition(fn.Status.Conditions, adxmonv1.FunctionReconciled)
	return condition != nil && condition.Reason == adxmonv1.ReasonCriteriaNotMatched
}

// logConditionStatusUpdate emits a log entry when a status condition transitions in a meaningful way.
// It keeps the UpdateCondition flow readable and avoids duplicating the change-detection logic elsewhere.
func logConditionStatusUpdate(fn *adxmonv1.Function, previous *metav1.Condition, updated metav1.Condition) {
	if previous != nil &&
		previous.Status == updated.Status &&
		previous.Reason == updated.Reason &&
		previous.Message == updated.Message {
		return
	}

	logger.Infof(
		"Function %s/%s condition %s updated status=%s reason=%s message=%s",
		fn.Namespace,
		fn.Name,
		updated.Type,
		updated.Status,
		updated.Reason,
		updated.Message,
	)
}
