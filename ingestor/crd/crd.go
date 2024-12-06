package crd

import (
	"context"
	"errors"
	"fmt"
	"time"

	v1 "github.com/Azure/adx-mon/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

type Options struct {
	RestConfig    *rest.Config
	PollFrequency time.Duration
	Handler       cache.ResourceEventHandler
}

type CRD struct {
	opts Options
	stop chan struct{}
}

func New(opts Options) *CRD {
	return &CRD{
		opts: opts,
	}
}

// TODO Do we need some sort of leader election here? We probably only want
// one operator mutating Kusto's state.

func (c *CRD) Open(ctx context.Context) error {
	if c.opts.RestConfig == nil {
		return errors.New("no client provided")
	}
	if c.opts.Handler == nil {
		return errors.New("no handler provided")
	}

	dynClient, err := dynamic.NewForConfig(c.opts.RestConfig)
	if err != nil {
		return fmt.Errorf("failed to create dynamic client: %w", err)
	}

	gvr := schema.GroupVersionResource{
		Group:    v1.GroupVersion.Group,
		Version:  v1.GroupVersion.Version,
		Resource: v1.GroupVersion.WithKind("functions").Kind,
	}

	listWatch := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			unstructuredList, err := dynClient.Resource(gvr).Namespace(metav1.NamespaceAll).List(ctx, options)
			if err != nil {
				return nil, err
			}
			list := &v1.FunctionList{}
			err = runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredList.UnstructuredContent(), list)
			return list, err
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			watcher, err := dynClient.Resource(gvr).Namespace(metav1.NamespaceAll).Watch(ctx, options)
			if err != nil {
				return nil, err
			}

			return watch.Filter(watcher, func(event watch.Event) (watch.Event, bool) {
				if event.Type == watch.Error {
					return event, true
				}

				unstructuredObj, ok := event.Object.(*unstructured.Unstructured)
				if !ok {
					return event, false
				}

				function := &v1.Function{}
				err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.UnstructuredContent(), function)
				if err != nil {
					return watch.Event{Type: watch.Error, Object: &metav1.Status{
						Status:  metav1.StatusFailure,
						Message: err.Error(),
					}}, true
				}

				event.Object = function
				return event, true
			}), nil
		},
	}

	crdInformer := cache.NewSharedIndexInformer(listWatch, &v1.Function{}, c.opts.PollFrequency, cache.Indexers{})
	crdInformer.AddEventHandlerWithResyncPeriod(c.opts.Handler, c.opts.PollFrequency)

	c.stop = make(chan struct{})
	go crdInformer.Run(c.stop)

	if !cache.WaitForCacheSync(c.stop, crdInformer.HasSynced) {
		return fmt.Errorf("failed to sync caches")
	}

	return nil
}

func (c *CRD) Close() error {
	close(c.stop)
	return nil
}
