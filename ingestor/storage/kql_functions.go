package storage

import (
	"context"
	"fmt"
	"sync"
	"time"

	v1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/azure-kusto-go/kusto"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Functions struct {
	mu        sync.RWMutex
	views     *v1.FunctionList
	functions *v1.FunctionList
}

// TODO Waiting for Jason's PR to be merged before integrating this new code with
// the existing codebase. The idea is that TableViews will e shared between the adx
// package, which will invoke ViewForTable whenever it receives a log destined for
// a Table it has to create (but we also need to check if the Table exists but the View does not),
// and the service, which will create a TableView and pass it to the crd package to
// begin watching for Views.
//
// TODO We'll need something to creation Functions, too, not just Views.
//
// TODO Move the static function creation in the adx package into CRDs so we can
// dog-food our own code. Maybe, by way of an example, it makes sense to include
// a View for adx-mon component logs, just some simple default schema, that we
// in aks-adx-mon can override with a more specific schema.
//
// adx::syncer would probably be the best place to invoke ViewForTable, exposed as
// EnsureViewForTable, and it would be called whenever a log is received, just like
// EnsureTable is called whenever a log is received.
// Within adx::syncer there is a map that keeps track of Tables that have been created.
// We would like to have the same sort of thing; however, we need a way to invalid
// the cache when a View is modified. Maybe just store a reference to the TableView.
// Something like this:
func MockEnsureView(ctx context.Context, database, table string) error {
	// Created on sync receiver
	cache := make(map[string]*v1.Function)
	fns := &Functions{}
	client := &kusto.Client{}

	// Actual body of the method on sync
	view, ok := fns.View(database, table)
	if !ok {
		return nil
	}

	cached, ok := cache[database+table]
	if ok {
		// Is our cached version out of date?
		if cached.CreationTimestamp != view.CreationTimestamp {
			// Invalidate cache
			delete(cache, database+table)
		} else {
			// Cache is valid, nothing to do
			return nil
		}
	}

	stmt, err := view.Spec.MarshalToKQL()
	if err != nil {
		logger.Errorf("Failed to marshal view %s.%s to KQL: %v", database, table, err)
		return nil
	}
	if _, err := client.Mgmt(ctx, database, stmt); err != nil {
		logger.Errorf("Failed to create view %s.%s: %v", database, table, err)
		// Don't treat this as a terminal error
	} else {
		cache[database+table] = view
	}

	return nil
}

func MockUpdateFunctions(ctx context.Context) {
	// In sync
	fns := &Functions{}
	cache := make(map[string]*v1.Function)
	refreshInterval := time.Minute
	client := &kusto.Client{}

	// Method on sync
	for {
		select {
		case <-ctx.Done():
			return

		case <-time.After(refreshInterval):
			functions := fns.Functions()
			for _, fn := range functions {
				function, ok := cache[fn.Spec.Database+fn.Spec.Name]
				if ok {
					if fn.CreationTimestamp != function.CreationTimestamp {
						// Invalidate cache
						delete(cache, fn.Spec.Database+fn.Spec.Name)
					} else {
						// Cache is valid, nothing to do
						continue
					}
				}

				stmt, err := fn.Spec.MarshalToKQL()
				if err != nil {
					logger.Errorf("Failed to marshal view %s.%s to KQL: %v", fn.Spec.Database, fn.Spec.Name, err)
					continue
				}
				if _, err := client.Mgmt(ctx, fn.Spec.Database, stmt); err != nil {
					logger.Errorf("Failed to create view %s.%s: %v", fn.Spec.Database, fn.Spec.Name, err)
					// Don't treat this as a terminal error
					continue
				} else {
					cache[fn.Spec.Database+fn.Spec.Name] = fn
				}
			}
		}
	}
}

func (f *Functions) View(database, table string) (*v1.Function, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if f.views == nil {
		return nil, false
	}

	// Views act as facades for Tables, which the same name as the Table but
	// presenting a user defined schema instead of the stored OTLP. We therefore
	// identify a View by matching the database and then the Table == View.Name
	for _, view := range f.views.Items {
		if view.Spec.IsView && view.Spec.Database == database && view.Spec.Name == table {
			return view.DeepCopy(), true
		}
	}

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
		// TODO: If a database isn't specified, should we just consider installing
		// the function in all tracked databases? This would be useful for adx-mon's
		// own functions, for example.
		if function.Spec.Database == "" {
			logger.Errorf("Function %s has no database", function.Spec.Name)
			continue
		}
		if function.Spec.Table == "" {
			logger.Errorf("Function %s has no table", function.Spec.Name)
			continue
		}
		if function.Spec.IsView {
			if function.Spec.Name != function.Spec.Table {
				logger.Errorf("View %s has a name that does not match the Table", function.Spec.Name)
				continue
			}
		} else {
			if function.Spec.Name == "" {
				logger.Errorf("Function %s has no name", function.Spec.Name)
				continue
			}
		}
		if _, ok := unique[function.Spec.Database+function.Spec.Name]; ok {
			logger.Errorf("Function %s is a duplicate", function.Spec.Name)
			continue
		}
		unique[function.Spec.Database+function.Spec.Name] = struct{}{}

		if function.Spec.IsView {
			views.Items = append(views.Items, *function.DeepCopy())
		} else {
			functions.Items = append(functions.Items, *function.DeepCopy())
		}
	}
	// TODO (jesthom): Do we want to identify those Views that we know about but
	// are no longer in the system and should therefore be deleted?

	f.mu.Lock()
	f.views = views
	f.functions = functions
	f.mu.Unlock()

	return nil
}
