package k8s

import (
	"sync"
	"sync/atomic"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
)

type podNotificationKind int

const (
	podNotificationAdd podNotificationKind = iota
	podNotificationUpdate
	podNotificationDelete
)

// podNotification is a pending event for one pod. For adds, the pod is in newPod. For updates, oldPod is the
// last state delivered to the handler and newPod is the latest state. For deletes, the pod is in oldPod.
type podNotification struct {
	kind    podNotificationKind
	oldPod  *corev1.Pod
	newPod  *corev1.Pod
	initial bool
}

// kubeletRegistration delivers notifications to a single handler on its own goroutine, so a slow or blocked
// handler cannot stall the kubelet poller or other handlers.
//
// Pending notifications are merged per pod UID, so a handler that falls behind only receives the latest state of
// each pod and the backlog is bounded by the number of pods rather than the number of polls.
type kubeletRegistration struct {
	informer *KubeletPodInformer
	handler  cache.ResourceEventHandler
	synced   atomic.Bool
	syncedCh chan struct{}

	mu                sync.Mutex
	queue             []types.UID
	pending           map[types.UID]*podNotification
	initialPending    int
	initialListQueued bool

	wake     chan struct{}
	stopCh   chan struct{}
	stopOnce sync.Once
	done     chan struct{}
}

var _ cache.ResourceEventHandlerRegistration = (*kubeletRegistration)(nil)

func newKubeletRegistration(informer *KubeletPodInformer, handler cache.ResourceEventHandler) *kubeletRegistration {
	return &kubeletRegistration{
		informer: informer,
		handler:  handler,
		syncedCh: make(chan struct{}),
		pending:  make(map[types.UID]*podNotification),
		wake:     make(chan struct{}, 1),
		stopCh:   make(chan struct{}),
		done:     make(chan struct{}),
	}
}

func (r *kubeletRegistration) HasSynced() bool {
	return r.synced.Load()
}

func (r *kubeletRegistration) HasSyncedChecker() cache.DoneChecker {
	return kubeletRegistrationDoneChecker{registration: r}
}

type kubeletRegistrationDoneChecker struct {
	registration *kubeletRegistration
}

func (c kubeletRegistrationDoneChecker) Name() string {
	return "kubelet pod handler"
}

func (c kubeletRegistrationDoneChecker) Done() <-chan struct{} {
	return c.registration.syncedCh
}

func (r *kubeletRegistration) enqueueAdd(pod *corev1.Pod, initial bool) {
	r.enqueue(pod.UID, podNotification{kind: podNotificationAdd, newPod: pod, initial: initial})
}

func (r *kubeletRegistration) enqueueUpdate(oldPod, newPod *corev1.Pod) {
	r.enqueue(newPod.UID, podNotification{kind: podNotificationUpdate, oldPod: oldPod, newPod: newPod})
}

func (r *kubeletRegistration) enqueueDelete(pod *corev1.Pod) {
	r.enqueue(pod.UID, podNotification{kind: podNotificationDelete, oldPod: pod})
}

// enqueue never blocks on the handler.
func (r *kubeletRegistration) enqueue(uid types.UID, n podNotification) {
	r.mu.Lock()
	if n.initial {
		r.initialPending++
	}
	if existing, ok := r.pending[uid]; ok {
		if !mergeNotification(existing, n) {
			delete(r.pending, uid)
			if existing.initial {
				r.finishInitialLocked()
			}
		}
	} else {
		r.pending[uid] = &n
		r.queue = append(r.queue, uid)
	}
	r.mu.Unlock()

	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// mergeNotification folds next into the pending notification so the handler observes a valid transition from the
// state it last saw to the latest state. Returns false when the two cancel out and nothing should be delivered.
func mergeNotification(pending *podNotification, next podNotification) bool {
	switch pending.kind {
	case podNotificationAdd:
		if next.kind == podNotificationDelete {
			// The handler never saw this pod.
			return false
		}
		pending.newPod = next.newPod
	case podNotificationUpdate:
		if next.kind == podNotificationDelete {
			pending.kind = podNotificationDelete
			pending.oldPod = next.oldPod
			pending.newPod = nil
			return true
		}
		pending.newPod = next.newPod
	case podNotificationDelete:
		if next.kind != podNotificationDelete {
			// The pod reappeared before the handler saw the delete; oldPod is still the last state it saw.
			pending.kind = podNotificationUpdate
			pending.newPod = next.newPod
		} else {
			pending.oldPod = next.oldPod
		}
	}
	return true
}

// markInitialListQueued is called once all initial adds have been enqueued.
func (r *kubeletRegistration) markInitialListQueued() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.initialListQueued = true
	r.maybeMarkSyncedLocked()
}

func (r *kubeletRegistration) finishInitialLocked() {
	r.initialPending--
	r.maybeMarkSyncedLocked()
}

func (r *kubeletRegistration) maybeMarkSyncedLocked() {
	if r.initialListQueued && r.initialPending == 0 && !r.synced.Load() {
		r.synced.Store(true)
		close(r.syncedCh)
	}
}

func (r *kubeletRegistration) next() (podNotification, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for len(r.queue) > 0 {
		uid := r.queue[0]
		r.queue[0] = ""
		r.queue = r.queue[1:]
		if n, ok := r.pending[uid]; ok {
			delete(r.pending, uid)
			return *n, true
		}
	}
	// Release the backing array once drained so a past burst doesn't pin memory.
	r.queue = nil
	return podNotification{}, false
}

func (r *kubeletRegistration) run() {
	defer close(r.done)

	for {
		n, ok := r.next()
		if !ok {
			select {
			case <-r.wake:
				continue
			case <-r.stopCh:
				return
			}
		}

		select {
		case <-r.stopCh:
			return
		default:
		}

		r.deliver(n)
	}
}

func (r *kubeletRegistration) deliver(n podNotification) {
	switch n.kind {
	case podNotificationAdd:
		r.handler.OnAdd(n.newPod, n.initial)
	case podNotificationUpdate:
		r.handler.OnUpdate(n.oldPod, n.newPod)
	case podNotificationDelete:
		r.handler.OnDelete(n.oldPod)
	}

	if n.initial {
		r.mu.Lock()
		r.finishInitialLocked()
		r.mu.Unlock()
	}
}

// stop prevents further deliveries without waiting for an in-progress handler call to return.
func (r *kubeletRegistration) stop() {
	r.stopOnce.Do(func() {
		close(r.stopCh)
	})
}
