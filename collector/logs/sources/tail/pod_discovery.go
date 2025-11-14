package tail

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Azure/adx-mon/pkg/k8s"
	"github.com/Azure/adx-mon/pkg/logger"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
)

type currenttarget struct {
	CurrentTarget FileTailTarget
	UpdateChan    chan<- FileTailTarget
	ExpireTime    time.Time
}

type TailerSourceInterface interface {
	AddTarget(target FileTailTarget, updateChan <-chan FileTailTarget) error
	RemoveTarget(target string)
}

type PodDiscoveryOpts struct {
	NodeName         string
	PodInformer      k8s.PodInformerInterface
	StaticPodTargets []*StaticPodTargets
}

type StaticPodTargets struct {
	Namespace   string
	Name        string
	Labels      map[string]string
	Parsers     []string
	Destination string
}

type PodDiscovery struct {
	NodeName         string
	staticPodTargets []*StaticPodTargets

	nowFunc func() time.Time

	tailsource TailerSourceInterface
	// pod uid -> filename -> target
	podidToTargets map[string]map[string]*currenttarget
	mut            sync.Mutex // protect podidToTargets

	cancel               context.CancelFunc
	podInformer          k8s.PodInformerInterface
	informerRegistration cache.ResourceEventHandlerRegistration
	wg                   sync.WaitGroup
}

func NewPodDiscovery(opts PodDiscoveryOpts, tailsource TailerSourceInterface) *PodDiscovery {
	return &PodDiscovery{
		nowFunc:          time.Now,
		NodeName:         opts.NodeName,
		podInformer:      opts.PodInformer,
		tailsource:       tailsource,
		podidToTargets:   make(map[string]map[string]*currenttarget),
		staticPodTargets: opts.StaticPodTargets,
	}
}

func (i *PodDiscovery) Open(ctx context.Context) error {
	logger.Infof("Starting logging scraper for node %s", i.NodeName)
	ctx, cancelFn := context.WithCancel(ctx)
	i.cancel = cancelFn

	var err error
	if i.informerRegistration, err = i.podInformer.Add(ctx, i); err != nil {
		return err
	}

	i.wg.Add(1)
	go func() {
		defer i.wg.Done()
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				i.cleanTombstonedTargets()
			}
		}
	}()

	return nil
}

func (i *PodDiscovery) Close() error {
	i.cancel()
	i.podInformer.Remove(i.informerRegistration)
	i.informerRegistration = nil
	i.wg.Wait()
	return nil
}

// OnAdd is called when an object is added. Expects a pod.
func (i *PodDiscovery) OnAdd(obj interface{}, isInitialList bool) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		return
	}

	targets := getFileTargets(pod, i.NodeName, i.staticPodTargets)

	i.mut.Lock()
	defer i.mut.Unlock()
	existingTargets, hasExistingTargets := i.podidToTargets[string(pod.UID)]
	if !hasExistingTargets && len(targets) == 0 {
		// Nothing to update or keep track of.
		return
	}

	if !hasExistingTargets {
		existingTargets = map[string]*currenttarget{}
		i.podidToTargets[string(pod.UID)] = existingTargets
	}

	// Update or add targets
	for _, target := range targets {
		existingCurrentTarget, ok := existingTargets[target.FilePath]
		if ok {
			// We're already tailing this target. Check if we need to update the database or table.
			if isTargetChanged(existingCurrentTarget.CurrentTarget, target) || !existingCurrentTarget.ExpireTime.IsZero() {
				i.updateTarget(target, existingCurrentTarget)
			}
		} else {
			// We're not tailing this target yet. Add it.
			err := i.startTailing(target, existingTargets)
			if err != nil {
				logger.Errorf("failed to start tailing %q: %s", target.FilePath, err)
			}
		}
	}

	// Remove old targets
	for existingPath, existingTarget := range existingTargets {
		found := false
		for _, target := range targets {
			if target.FilePath == existingPath {
				found = true
				break
			}
		}
		if !found {
			i.tombstoneTarget(existingTarget)
		}
	}

}

// OnUpdate is called when an object is updated. Expects a pod.
func (i *PodDiscovery) OnUpdate(oldObj, newObj interface{}) {
	p, ok := newObj.(*v1.Pod)
	if !ok || p == nil {
		return
	}

	if p.DeletionTimestamp != nil {
		i.OnDelete(p)
	} else {
		i.OnAdd(p, false)
	}
}

// OnDelete is called when an object is deleted. Expects a pod.
func (i *PodDiscovery) OnDelete(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		return
	}

	i.mut.Lock()
	defer i.mut.Unlock()
	existingTargets, hasExistingTargets := i.podidToTargets[string(pod.UID)]
	if !hasExistingTargets {
		return
	}

	for _, existingTarget := range existingTargets {
		i.tombstoneTarget(existingTarget)
	}
}

func (i *PodDiscovery) startTailing(target FileTailTarget, existingTargets map[string]*currenttarget) error {
	logger.Infof("Adding target %s", target.FilePath)
	// We're not tailing this target yet. Add it.
	updateChan := make(chan FileTailTarget, 1)
	err := i.tailsource.AddTarget(target, updateChan)
	if err != nil {
		return fmt.Errorf("failed to start tailing: %w", err)
	}
	currenttarget := &currenttarget{
		CurrentTarget: target,
		UpdateChan:    updateChan,
	}
	existingTargets[target.FilePath] = currenttarget
	return nil
}

func (i *PodDiscovery) updateTarget(target FileTailTarget, existingCurrentTarget *currenttarget) {
	logger.Infof("Updating target %s", target.FilePath)
	if !existingCurrentTarget.ExpireTime.IsZero() {
		// No longer should be tombstoned
		existingCurrentTarget.ExpireTime = time.Time{}
	}
	existingCurrentTarget.UpdateChan <- target
	existingCurrentTarget.CurrentTarget = target
}

func (i *PodDiscovery) tombstoneTarget(existingCurrentTarget *currenttarget) {
	// Set this target to be cleaned in the future. This is to allow some time for the target's logs
	// to be written to disk by the container runtime and for us to consume it before we stop tailing it.

	// We don't want to just tell tailsource to stop tailing when the file reaches EOF because the file
	// may be written to again in the "future" and perhaps even rotated.
	if existingCurrentTarget.ExpireTime.IsZero() {
		logger.Infof("Tombstoning target %s", existingCurrentTarget.CurrentTarget.FilePath)
		existingCurrentTarget.ExpireTime = i.nowFunc().Add(1 * time.Minute)
	}
}

func (i *PodDiscovery) cleanTombstonedTargets() {
	i.mut.Lock()
	defer i.mut.Unlock()

	for podID, existingTargets := range i.podidToTargets {
		for path, existingTarget := range existingTargets {
			if !existingTarget.ExpireTime.IsZero() && i.nowFunc().After(existingTarget.ExpireTime) {
				logger.Infof("Removing tombstoned target %s", path)
				i.tailsource.RemoveTarget(existingTarget.CurrentTarget.FilePath)
				delete(existingTargets, path)
			}
		}
		if len(existingTargets) == 0 {
			delete(i.podidToTargets, podID)
		}
	}
}
