package k8s

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// PodInformer wraps a shared pod informer for a single node and adds self-monitoring
// so the collector can detect when cache updates stop flowing. Once the watchdog
// declares the informer stale the caller will crash (allowing kubelet to restart
// the pod) rather than continuing to run in a silently unhealthy state.
type PodInformer struct {
	K8sClient kubernetes.Interface
	NodeName  string

	mu                sync.Mutex
	informerFactory   informers.SharedInformerFactory
	informer          cache.SharedIndexInformer
	eventHandlerCount int
	lastActivity      atomic.Int64
	watchdogCancel    context.CancelFunc
}

var (
	// PodInformerStaleThreshold controls how long we allow the informer to sit idle
	// after the most recent pod Add/Update/Delete callback before we assume it is
	// wedged and crash. Exported so tests (or future config) can tune the value.
	PodInformerStaleThreshold = 5 * time.Minute
	// podInformerWatchdogInterval is intentionally shorter than the stale threshold
	// so we check multiple times before crossing the threshold. Tests override this
	// to speed up execution.
	podInformerWatchdogInterval = time.Minute
	// podInformerPanic allows tests to intercept the watchdog panic path without
	// bringing down the entire test process. Production code leaves this as panic.
	podInformerPanic = func(msg string) { panic(msg) }
)

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

		p.markActivity()

		activityHandler := cache.ResourceEventHandlerFuncs{
			AddFunc: func(object any) {
				p.markActivity()
			},
			UpdateFunc: func(oldObj, newObj any) {
				p.markActivity()
			},
			DeleteFunc: func(object any) {
				p.markActivity()
			},
		}

		if _, err := p.informer.AddEventHandler(activityHandler); err != nil {
			p.informerFactory = nil
			p.informer = nil
			return nil, fmt.Errorf("k8s: failed to add activity handler: %w", err)
		}

		p.startWatchdogLocked(ctx)

		p.informerFactory.Start(ctx.Done()) // start informer goroutines
		p.informerFactory.WaitForCacheSync(ctx.Done())
		p.markActivity()
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
			p.stopWatchdogLocked()
		}
	}

	return err
}

func (p *PodInformer) markActivity() {
	// Called from informer's event handlers and when the informer is first
	// constructed. Writing the timestamp here lets the watchdog measure how
	// long it has been since the informer observed any pod events.
	p.lastActivity.Store(time.Now().UnixNano())
}

func (p *PodInformer) startWatchdogLocked(ctx context.Context) {
	// Avoid spawning duplicate watchdogs if multiple handlers race to create the informer.
	if p.watchdogCancel != nil {
		return
	}

	if PodInformerStaleThreshold <= 0 {
		// Disabled via tuning: never panic in this mode.
		return
	}

	watchCtx, cancel := context.WithCancel(ctx)
	p.watchdogCancel = cancel

	go func() {
		// Ticker that periodically evaluates the informer activity time. We defer
		// cleanup so a stopped informer does not leak goroutines.
		ticker := time.NewTicker(podInformerWatchdogInterval)
		defer ticker.Stop()

		for {
			select {
			case <-watchCtx.Done():
				return
			case <-ticker.C:
				last := p.lastActivity.Load()
				if last == 0 {
					continue
				}

				lastEvent := time.Unix(0, last)
				idle := time.Since(lastEvent)
				if idle > PodInformerStaleThreshold {
					if p.storeEmpty() {
						continue
					}
					podInformerPanic(fmt.Sprintf("k8s.PodInformer for node %s stale for %s (threshold %s)", p.NodeName, idle, PodInformerStaleThreshold))
				}
			}
		}
	}()
}

func (p *PodInformer) stopWatchdogLocked() {
	if p.watchdogCancel == nil {
		return
	}

	p.watchdogCancel()
	p.watchdogCancel = nil
}

func (p *PodInformer) storeEmpty() bool {
	p.mu.Lock()
	informer := p.informer
	p.mu.Unlock()

	if informer == nil {
		return true
	}

	store := informer.GetStore()
	if store == nil {
		return true
	}

	if len(store.ListKeys()) == 0 {
		return true
	}

	return false
}
