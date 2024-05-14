package k8s

import (
	"context"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

type PodInformer struct {
	K8sClient kubernetes.Interface
	NodeName  string

	mu                sync.Mutex
	informerFactory   informers.SharedInformerFactory
	informer          cache.SharedIndexInformer
	eventHandlerCount int
}

func NewPodInformer(k8sClient kubernetes.Interface, nodeName string) *PodInformer {
	return &PodInformer{
		K8sClient: k8sClient,
		NodeName:  nodeName,

		eventHandlerCount: 0,
	}
}

// Add adds a handler to the informer.  The handler will be called when a pod is added, updated, or deleted.
// Lazily creates and starts the informer if it does not already exist.
func (p *PodInformer) Add(ctx context.Context, handler cache.ResourceEventHandler) (cache.ResourceEventHandlerRegistration, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.informerFactory == nil {
		tweakOptions := informers.WithTweakListOptions(func(lo *metav1.ListOptions) {
			lo.FieldSelector = "spec.nodeName=" + p.NodeName
		})
		p.informerFactory = informers.NewSharedInformerFactoryWithOptions(p.K8sClient, time.Minute, tweakOptions)
		p.informer = p.informerFactory.Core().V1().Pods().Informer()

		p.informerFactory.Start(ctx.Done())
		p.informerFactory.WaitForCacheSync(ctx.Done())
	}

	ret, err := p.informer.AddEventHandler(handler)
	if err == nil {
		p.eventHandlerCount++
	}
	return ret, err
}

// Remove removes a handler from the informer.
// Lazily shuts down the informer if there are no more handlers.
func (p *PodInformer) Remove(reg cache.ResourceEventHandlerRegistration) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	err := p.informer.RemoveEventHandler(reg)
	if err == nil {
		p.eventHandlerCount--

		if p.eventHandlerCount == 0 {
			p.informerFactory.Shutdown()
			p.informerFactory = nil
			p.informer = nil
		}
	}

	return err
}
