package metadata

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

const (
	nodeInformerResync = time.Minute
	initialSyncTimeout = 10 * time.Second
)

// KubeNode watches metadata for the local Kubernetes node and exposes
// its labels and annotations in a concurrency-safe manner.  The watcher
// implements the service.Component interface via the Open and Close
// methods so it can be managed alongside other long-lived components in
// the collector.
type KubeNode struct {
	client   kubernetes.Interface
	nodeName string

	cancel          context.CancelFunc
	informerFactory informers.SharedInformerFactory
	opened          bool

	dataMu      sync.RWMutex
	labels      map[string]string
	annotations map[string]string
}

// NewKubeNode creates a new KubeNode watcher for the provided node name.
func NewKubeNode(client kubernetes.Interface, nodeName string) *KubeNode {
	return &KubeNode{
		client:   client,
		nodeName: nodeName,
	}
}

// Open initializes the node informer and blocks until the initial
// metadata for the local node has been observed or the timeout is
// reached.  Subsequent calls are no-ops.
// If the initial sync fails or times out, no error is returned to allow
// the collector to continue starting up; metadata will be populated
// asynchronously once events arrive.
func (k *KubeNode) Open(ctx context.Context) error {
	if k == nil {
		return errors.New("metadata: KubeNode is nil")
	}

	if err := k.ensureOpen(ctx); err != nil {
		return err
	}

	return nil
}

func (k *KubeNode) ensureOpen(ctx context.Context) error {
	if k.opened {
		return nil
	}

	if k.client == nil {
		return errors.New("metadata: kubernetes client is nil")
	}

	if k.nodeName == "" {
		return errors.New("metadata: node name is empty")
	}

	localCtx, cancel := context.WithCancel(ctx)
	factory := informers.NewSharedInformerFactoryWithOptions(
		k.client,
		nodeInformerResync,
		informers.WithTweakListOptions(func(lo *metav1.ListOptions) {
			lo.FieldSelector = fields.OneTermEqualSelector("metadata.name", k.nodeName).String()
		}),
	)

	nodeInformer := factory.Core().V1().Nodes()
	informer := nodeInformer.Informer()

	handler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			if node := k.decodeNode(obj); node != nil {
				k.captureMetadata(node)
			}
		},
		UpdateFunc: func(_, newObj interface{}) {
			if node := k.decodeNode(newObj); node != nil {
				k.captureMetadata(node)
			}
		},
		// Intentionally split out to allow calling with tests
		DeleteFunc: k.handleDelete,
	}

	_, err := informer.AddEventHandler(handler)
	if err != nil {
		cancel()
		return fmt.Errorf("metadata: failed to add node informer handler: %w", err)
	}

	k.cancel = cancel
	k.informerFactory = factory
	k.opened = true

	factory.Start(localCtx.Done())

	syncCtx, syncCancel := context.WithTimeout(ctx, initialSyncTimeout)
	defer syncCancel()

	synced := cache.WaitForCacheSync(syncCtx.Done(), informer.HasSynced)
	if !synced {
		if err := syncCtx.Err(); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			k.Close()
			return err
		}

		// Timed out waiting for initial sync; informer will continue
		// running and populate metadata asynchronously once events
		// arrive.
		return nil
	}

	if node, err := nodeInformer.Lister().Get(k.nodeName); err == nil {
		k.captureMetadata(node)
	}

	return nil
}

// Close shuts down the informer and clears the cancellation function.
func (k *KubeNode) Close() error {
	if k == nil {
		return nil
	}

	if !k.opened {
		return nil
	}

	cancel := k.cancel
	factory := k.informerFactory

	k.cancel = nil
	k.informerFactory = nil
	k.opened = false

	if cancel != nil {
		cancel()
	}

	if factory != nil {
		factory.Shutdown()
	}

	return nil
}

// Label returns the value of a label on the current node along with a
// boolean indicating whether the key was present.
func (k *KubeNode) Label(key string) (string, bool) {
	k.dataMu.RLock()
	defer k.dataMu.RUnlock()

	if k.labels == nil {
		return "", false
	}

	val, ok := k.labels[key]
	return val, ok
}

// Annotation returns the value of an annotation on the current node along with a
// boolean indicating whether the key was present.
func (k *KubeNode) Annotation(key string) (string, bool) {
	k.dataMu.RLock()
	defer k.dataMu.RUnlock()

	if k.annotations == nil {
		return "", false
	}

	val, ok := k.annotations[key]
	return val, ok
}

// WalkLabels calls the provided function for each label on the node.
func (k *KubeNode) WalkLabels(fn func(key, value string)) {
	if fn == nil {
		return
	}

	k.dataMu.RLock()
	defer k.dataMu.RUnlock()

	for key, value := range k.labels {
		fn(key, value)
	}
}

// WalkAnnotations calls the provided function for each annotation on the node.
func (k *KubeNode) WalkAnnotations(fn func(key, value string)) {
	if fn == nil {
		return
	}

	k.dataMu.RLock()
	defer k.dataMu.RUnlock()

	for key, value := range k.annotations {
		fn(key, value)
	}
}

func (k *KubeNode) decodeNode(obj interface{}) *corev1.Node {
	switch v := obj.(type) {
	case *corev1.Node:
		if v != nil && v.Name == k.nodeName {
			return v
		}
	case cache.DeletedFinalStateUnknown:
		if node, ok := v.Obj.(*corev1.Node); ok && node.Name == k.nodeName {
			return node
		}
	}
	return nil
}

func (k *KubeNode) handleDelete(obj interface{}) {
	// Intentionally no-op: if our node is deleted we keep the last
	// observed metadata because the collector is still running on it.
}

func (k *KubeNode) captureMetadata(node *corev1.Node) {
	if node == nil {
		return
	}

	k.dataMu.Lock()
	if k.labels == nil {
		k.labels = make(map[string]string, len(node.Labels))
	} else {
		clear(k.labels)
	}
	maps.Copy(k.labels, node.Labels)

	if k.annotations == nil {
		k.annotations = make(map[string]string, len(node.Annotations))
	} else {
		clear(k.annotations)
	}
	maps.Copy(k.annotations, node.Annotations)
	k.dataMu.Unlock()
}
