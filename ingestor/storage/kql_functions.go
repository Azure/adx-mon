package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/scheduler"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
)

const (
	// name of our custom finalizer
	FinalizerName = "function.adx-mon.azure.com/finalizer"
)

type Functions interface {
	UpdateStatus(ctx context.Context, fn *adxmonv1.Function) error
	Update(ctx context.Context, fn *adxmonv1.Function) error
	List(ctx context.Context) ([]*adxmonv1.Function, error)
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

	if fn.Status.Status == adxmonv1.Success {
		fn.Status.ObservedGeneration = fn.GetGeneration()
		fn.Status.Error = ""

		if !fn.DeletionTimestamp.IsZero() && controllerutil.ContainsFinalizer(fn, FinalizerName) {
			// remove our finalizer from the list and update it.
			controllerutil.RemoveFinalizer(fn, FinalizerName)
			if err := f.Client.Update(ctx, fn); err != nil {
				logger.Errorf("Failed to remove finalizer from function %s: %v", fn.Name, err)
				fn.Status.Status = adxmonv1.Failed
			} else {
				return nil
			}
		}
	}

	fn.Status.LastTimeReconciled = metav1.Now()
	return f.Client.Status().Update(ctx, fn)
}

func (f *functions) List(ctx context.Context) ([]*adxmonv1.Function, error) {
	if f.Client == nil {
		return nil, fmt.Errorf("no client provided")
	}

	if f.Elector != nil && !f.Elector.IsLeader() {
		return nil, nil
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

		} else {

			switch fn.GetGeneration() {
			case fn.Status.ObservedGeneration:
				// Skip functions that are up to date
				continue

			case 1:
				fn.Status.Reason = "Function created"

			default:
				fn.Status.Reason = "Function updated"
			}

			if err := f.ensureFinalizer(ctx, &fn); err != nil {
				logger.Errorf("Failed to ensure finalizer for function %s: %v", fn.Name, err)
			}
		}

		fns = append(fns, &fn)
	}

	return fns, nil
}

func (f *functions) ensureFinalizer(ctx context.Context, fn *adxmonv1.Function) error {
	if f.Client == nil {
		return fmt.Errorf("no client provided")
	}

	// examine DeletionTimestamp to determine if object is under deletion
	if fn.ObjectMeta.DeletionTimestamp.IsZero() {
		// The object is not being deleted, so if it does not have our finalizer,
		// then lets add the finalizer and update the object. This is equivalent
		// to registering our finalizer.
		if !controllerutil.ContainsFinalizer(fn, FinalizerName) {
			controllerutil.AddFinalizer(fn, FinalizerName)
			return f.Client.Update(ctx, fn)
		}
	}

	return nil
}
