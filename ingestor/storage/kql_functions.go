package storage

import (
	"context"
	"errors"
	"fmt"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/scheduler"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Functions interface {
	UpdateStatus(ctx context.Context, fn *adxmonv1.Function) error
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

func (f *functions) UpdateStatus(ctx context.Context, fn *adxmonv1.Function) error {
	if f.Client == nil {
		return fmt.Errorf("no client provided")
	}

	if fn.Status.Status == adxmonv1.Success {
		fn.Status.ObservedGeneration = fn.GetGeneration()
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
		if errors.Is(err, &meta.NoKindMatchError{}) {
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

		switch fn.GetGeneration() {
		case fn.Status.ObservedGeneration:
			// Skip functions that are up to date
			continue

		case 1:
			fn.Status.Reason = "Function created"

		default:
			fn.Status.Reason = "Function updated"
		}

		// TODO once we can parse the KQL and appropriately determine
		// if the error is really due to a permanent failure, we can
		// filter out the functions that are in a permanent failure state.

		fns = append(fns, &fn)
	}

	return fns, nil
}
