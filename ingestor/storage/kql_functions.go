package storage

import (
	"context"
	"fmt"
	"sync"

	v1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/logger"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Functions struct {
	mu        sync.RWMutex
	views     *v1.FunctionList
	functions *v1.FunctionList
	client    client.StatusClient
}

func NewFunctions(client client.StatusClient) *Functions {
	return &Functions{
		client: client,
	}
}

func (f *Functions) UpdateStatus(ctx context.Context, fn *v1.Function) error {
	if f.client == nil {
		return fmt.Errorf("no client provided")
	}

	return f.client.Status().Update(ctx, fn)
}

func (f *Functions) Delete(ctx context.Context, fn *v1.Function) error {
	if f.client == nil {
		return fmt.Errorf("no client provided")
	}

	return nil
}

func (f *Functions) View(database, table string) (*v1.Function, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if f.views == nil {
		return nil, false
	}

	// TODO (jesthom): Once the parser is in place we can identify the Views.

	return nil, false
}

func (f *Functions) Functions() []*v1.Function {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if f.functions == nil {
		return nil
	}

	var fns []*v1.Function
	for _, f := range f.functions.Items {
		fns = append(fns, f.DeepCopy())
	}
	return fns
}

func (f *Functions) List() []*v1.Function {
	f.mu.RLock()
	defer f.mu.RUnlock()

	var fns []*v1.Function
	if f.functions != nil {
		for _, fn := range f.functions.Items {
			fns = append(fns, fn.DeepCopy())
		}
	}
	if f.views != nil {
		for _, view := range f.views.Items {
			fns = append(fns, view.DeepCopy())
		}
	}

	return fns
}

func (f *Functions) Receive(ctx context.Context, list client.ObjectList) error {
	items, ok := list.(*v1.FunctionList)
	if !ok {
		return fmt.Errorf("expected *v1.FunctionList, got %T", list)
	}
	if items == nil || len(items.Items) == 0 {
		return nil
	}

	var (
		views     = &v1.FunctionList{}
		functions = &v1.FunctionList{}
		unique    = make(map[string]struct{})
	)
	for _, function := range items.Items {
		// TODO (jesthom): If a database isn't specified, should we just consider installing
		// the function in all tracked databases? This would be useful for adx-mon's
		// own functions, for example.
		if function.Spec.Database == "" {
			logger.Errorf("Function %s has no database", function.Name)
			continue
		}
		// TODO (jesthom): Once the parser is in place we can perform more validation.
		if _, ok := unique[function.Spec.Database+function.Name]; ok {
			logger.Errorf("Function %s is a duplicate", function.Name)
			continue
		}
		unique[function.Spec.Database+function.Name] = struct{}{}
		// TODO (jesthom): Once the parser is in place separate out the Views from the Functions.
		functions.Items = append(functions.Items, *function.DeepCopy())
	}

	f.mu.Lock()
	f.views = views
	f.functions = functions
	f.mu.Unlock()

	return nil
}
