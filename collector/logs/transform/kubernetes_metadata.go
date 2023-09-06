package transform

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/Azure/adx-mon/collector/logs"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
)

const (
	allNamespaces = ""
)

type KubernetesTransform struct {
	client kubernetes.Interface

	watchCtx context.Context
	cancel   context.CancelFunc

	wg sync.WaitGroup

	podCache   map[string]*v1.Pod
	cacheMutex sync.RWMutex

	toDelete    map[string]time.Time
	deleteMutex sync.Mutex
}

func NewKubernetesTransform(client kubernetes.Interface) *KubernetesTransform {
	return &KubernetesTransform{
		client:   client,
		podCache: make(map[string]*v1.Pod),
		toDelete: make(map[string]time.Time),
	}
}

func (t *KubernetesTransform) Open(ctx context.Context) error {
	if t.watchCtx != nil {
		return nil
	}

	t.watchCtx, t.cancel = context.WithCancel(ctx)
	t.wg.Add(1)
	go t.watcherWorker()
	return nil
}

func (t *KubernetesTransform) Transform(ctx context.Context, batch *logs.LogBatch) (*logs.LogBatch, error) {
	for _, log := range batch.Logs {
		podName, ok := log.Attributes["k8s.pod.name"].(string)
		if !ok {
			continue
		}
		namespace, ok := log.Attributes["k8s.namespace.name"].(string)
		if !ok {
			continue
		}

		podMeta, err := t.getPodMetadata(namespace, podName)
		if err != nil {
			// TODO log
			continue
		}
		log.Attributes["k8s.pod.labels"] = podMeta.Labels
	}
	return batch, nil
}

func (t *KubernetesTransform) Close() error {
	t.cancel()
	t.wg.Wait()
	return nil
}

func (t *KubernetesTransform) getPodMetadata(namespace string, podName string) (*v1.Pod, error) {
	t.cacheMutex.RLock()
	podMeta, ok := t.podCache[cacheKey(namespace, podName)]
	t.cacheMutex.RUnlock()
	if ok {
		return podMeta, nil
	}

	podMeta, err := t.client.CoreV1().Pods(namespace).Get(t.watchCtx, podName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	t.cacheMutex.Lock()
	t.podCache[cacheKey(namespace, podName)] = podMeta
	t.cacheMutex.Unlock()
	return podMeta, nil
}

func (t *KubernetesTransform) watcherWorker() {
	defer t.wg.Done()

	//TODO handle error
	hostname, _ := os.Hostname()

	options := metav1.ListOptions{
		FieldSelector: "spec.nodeName=" + hostname,
	}

	for {
		select {
		case <-t.watchCtx.Done():
			return
		default:
		}

		watcher, err := t.client.CoreV1().Pods(allNamespaces).Watch(t.watchCtx, options)
		if err != nil {
			//t.logger.Printf("Error opening watcher: %s", err)
			if utilnet.IsConnectionRefused(err) {
				continue
			}
			return
		}

		err = t.watchHandler(watcher)
		if err != nil {
			//t.logger.Printf("Error while watching: %s", err)
			if apierrors.IsResourceExpired(err) || apierrors.IsGone(err) {
				continue
			}
			if errors.Is(err, context.Canceled) {
				//t.logger.Printf("Wathcher normal exit")
				return
			}
			//t.logger.Fatalf("Watching error: %s", err)
		}
	}
}

func (t *KubernetesTransform) watchHandler(watcher watch.Interface) error {
	defer watcher.Stop()

	bookmarkTicker := time.NewTicker(10 * time.Second)
	defer bookmarkTicker.Stop()

	for {
		select {
		// shutdown
		case <-t.watchCtx.Done():
			return context.Canceled

		// periodic bookmark events to allow cache cleanup
		case <-bookmarkTicker.C:
			t.deleteMutex.Lock()
			for key, expireTime := range t.toDelete {
				if time.Now().After(expireTime) {
					delete(t.toDelete, key)
					t.cacheMutex.Lock()
					delete(t.podCache, key)
					t.cacheMutex.Unlock()
				}
			}
			t.deleteMutex.Unlock()

		// handle watch events
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return nil
			}

			switch event.Type {
			case watch.Error:
				//t.logger.Printf("Error event received: %v", event.Object)
				return apierrors.FromObject(event.Object)
			case watch.Bookmark:
				continue
			case watch.Added, watch.Modified:
				podMeta, ok := event.Object.(*v1.Pod)
				if !ok {
					//t.logger.Printf("Error casting event object to v1.Pod, actual type is %T", event.Object)
					continue
				}
				t.cacheMutex.Lock()
				// TODO set max cache size
				t.podCache[cacheKeyFromPod(podMeta)] = podMeta
				t.cacheMutex.Unlock()
			case watch.Deleted:
				podMeta, ok := event.Object.(*v1.Pod)
				if !ok {
					//t.logger.Printf("Error casting event object to v1.Pod, actual type is %T", event.Object)
					continue
				}
				t.deleteMutex.Lock()
				// TODO configurable
				t.toDelete[cacheKeyFromPod(podMeta)] = time.Now().Add(1 * time.Minute)
				t.deleteMutex.Unlock()
			}
		}
	}

}

func cacheKeyFromPod(pod *v1.Pod) string {
	return cacheKey(pod.Namespace, pod.Name)
}

func cacheKey(namespace string, podName string) string {
	return namespace + "/" + podName
}
