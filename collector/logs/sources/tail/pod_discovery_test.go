package tail

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Azure/adx-mon/pkg/k8s"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
)

var (
	assertPollInterval = 10 * time.Millisecond
	assertPollMax      = 1 * time.Second
)

func TestPodDiscoveryLifecycle(t *testing.T) {
	t.Run("Log-only pod destination lifecycle", func(t *testing.T) {
		fakeNow, tailSource, podDiscovery := create()
		defer tailSource.Close()
		require.NoError(t, podDiscovery.Open(context.Background()))
		defer podDiscovery.Close()

		pod := scrapedPod("log-only")
		delete(pod.Annotations, "adx-mon/scrape")
		delete(pod.Annotations, "adx-mon/log-destination")
		podDiscovery.OnAdd(pod, false)
		require.Len(t, tailSource.targetAdds, 0)

		pod.Annotations["adx-mon/log-destination"] = "db:table"
		podDiscovery.OnUpdate(nil, pod)
		require.Len(t, tailSource.targetAdds, 1)

		pod.Annotations["adx-mon/log-destination"] = "db:other"
		podDiscovery.OnUpdate(nil, pod)
		require.Eventually(t, func() bool { return len(tailSource.targetUpdates) == 1 }, assertPollMax, assertPollInterval)

		pod.Annotations["adx-mon/log-destination"] = "db:"
		podDiscovery.OnUpdate(nil, pod)
		cleanTombstonedTargets(podDiscovery)
		require.Len(t, tailSource.targetRemoves, 0)

		pod.Annotations["adx-mon/log-destination"] = "db:table"
		podDiscovery.OnUpdate(nil, pod)
		fakeNow.Advance(time.Minute + time.Millisecond)
		cleanTombstonedTargets(podDiscovery)
		require.Len(t, tailSource.targetRemoves, 0)

		delete(pod.Annotations, "adx-mon/log-destination")
		podDiscovery.OnUpdate(nil, pod)
		cleanTombstonedTargets(podDiscovery)
		require.Len(t, tailSource.targetRemoves, 0)

		fakeNow.Advance(time.Minute + time.Millisecond)
		cleanTombstonedTargets(podDiscovery)
		require.Len(t, tailSource.targetRemoves, 1)
	})

	t.Run("Add Pod", func(t *testing.T) {
		_, tailSource, podDiscovery := create()
		defer tailSource.Close()
		err := podDiscovery.Open(context.Background())
		require.NoError(t, err)

		podDiscovery.OnAdd(scrapedPod("pod1"), false)
		podDiscovery.OnAdd(notScrapedPod("pod2"), false)

		cleanTombstonedTargets(podDiscovery)
		err = podDiscovery.Close()
		require.NoError(t, err)
		require.Equal(t, 1, len(tailSource.targetAdds))
		require.Equal(t, 0, len(tailSource.targetRemoves))
	})

	t.Run("Add Pod Without ContainerId", func(t *testing.T) {
		_, tailSource, podDiscovery := create()
		defer tailSource.Close()
		err := podDiscovery.Open(context.Background())
		require.NoError(t, err)

		pod := scrapedPod("pod1")
		pod.Status.ContainerStatuses[0].ContainerID = ""

		podDiscovery.OnAdd(pod, false)

		cleanTombstonedTargets(podDiscovery)
		err = podDiscovery.Close()
		require.NoError(t, err)
		// No containerID yet, so no target added.
		require.Equal(t, 0, len(tailSource.targetAdds))
		require.Equal(t, 0, len(tailSource.targetRemoves))
	})

	t.Run("Add Tolerant of Failure", func(t *testing.T) {
		// Failure tailing first container, but ok to tail the second container
		failureFunc := func(target FileTailTarget) error {
			if strings.Contains(target.FilePath, "container1") {
				return fmt.Errorf("failed to add target")
			}
			return nil
		}
		_, tailSource, podDiscovery := create()
		tailSource.addErrFunc = failureFunc
		defer tailSource.Close()
		err := podDiscovery.Open(context.Background())
		require.NoError(t, err)

		pod := scrapedPod("pod1")
		pod.Spec.Containers = append(pod.Spec.Containers, v1.Container{
			Name:  "container2",
			Image: "image2",
		})
		pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, v1.ContainerStatus{
			Name:        "container2",
			Image:       "image2",
			ContainerID: "docker://container2id",
		})
		podDiscovery.OnAdd(pod, false)

		cleanTombstonedTargets(podDiscovery)
		err = podDiscovery.Close()
		require.NoError(t, err)
		// Two attempted adds
		require.Equal(t, 2, len(tailSource.targetAdds))
		require.Equal(t, 0, len(tailSource.targetRemoves))
		// Only one target being scraped and tracked
		require.Equal(t, 1, len(podDiscovery.podidToTargets))
	})

	t.Run("Add And Update", func(t *testing.T) {
		_, tailSource, podDiscovery := create()
		err := podDiscovery.Open(context.Background())
		defer tailSource.Close()
		require.NoError(t, err)

		pod := scrapedPod("pod1")
		podDiscovery.OnAdd(pod, false)
		require.Equal(t, 1, len(tailSource.targetAdds))
		podDiscovery.OnUpdate(nil, pod)
		require.Equal(t, 1, len(tailSource.targetAdds)) // no change
		require.Equal(t, 0, len(tailSource.targetUpdates))

		pod.Annotations["adx-mon/log-destination"] = "db:table2"
		podDiscovery.OnUpdate(nil, pod)
		require.Equal(t, 1, len(tailSource.targetAdds)) // no new targets
		// updated destination table.
		require.Eventually(t, func() bool { return len(tailSource.targetUpdates) == 1 }, assertPollMax, assertPollInterval)

		pod.Spec.Containers = append(pod.Spec.Containers, v1.Container{
			Name:  "container2",
			Image: "image2",
		})
		pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, v1.ContainerStatus{
			Name:        "container2",
			Image:       "image2",
			ContainerID: "docker://container2id",
		})
		podDiscovery.OnUpdate(nil, pod)
		require.Equal(t, 2, len(tailSource.targetAdds)) // new container to scrape logs from

		cleanTombstonedTargets(podDiscovery)
		err = podDiscovery.Close()
		require.NoError(t, err)
		require.Equal(t, 0, len(tailSource.targetRemoves))
	})

	t.Run("Add And Remove", func(t *testing.T) {
		fakeNow, tailSource, podDiscovery := create()
		err := podDiscovery.Open(context.Background())
		defer tailSource.Close()
		require.NoError(t, err)

		// Removing pod we didn't know about is a no-op
		podDiscovery.OnDelete(scrapedPod("unknownpod"))

		pod := scrapedPod("pod1")
		podDiscovery.OnAdd(pod, false)
		require.Equal(t, 1, len(tailSource.targetAdds))

		pod.DeletionTimestamp = &metav1.Time{Time: fakeNow.Now()}
		podDiscovery.OnUpdate(nil, pod)
		cleanTombstonedTargets(podDiscovery)
		require.Equal(t, 0, len(tailSource.targetRemoves)) // no removal yet

		fakeNow.Advance(1 * time.Minute)
		fakeNow.Advance(1 * time.Millisecond)
		cleanTombstonedTargets(podDiscovery)
		require.Equal(t, 1, len(tailSource.targetRemoves)) // removed tombstoned target
		require.Equal(t, 0, len(podDiscovery.podidToTargets))

		err = podDiscovery.Close()
		require.NoError(t, err)
	})

	t.Run("Container Add And Remove", func(t *testing.T) {
		fakeNow, tailSource, podDiscovery := create()
		err := podDiscovery.Open(context.Background())
		defer tailSource.Close()
		require.NoError(t, err)

		pod := scrapedPod("pod1")
		podDiscovery.OnUpdate(nil, pod)
		require.Equal(t, 1, len(tailSource.targetAdds))

		pod.Spec.Containers = append(pod.Spec.Containers, v1.Container{
			Name:  "container2",
			Image: "image2",
		})
		pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, v1.ContainerStatus{
			Name:        "container2",
			Image:       "image2",
			ContainerID: "docker://container2id",
		})
		podDiscovery.OnUpdate(nil, pod)
		// New container added to scrape
		require.Equal(t, 2, len(tailSource.targetAdds))
		require.Equal(t, 0, len(tailSource.targetRemoves))

		pod.Spec.Containers = pod.Spec.Containers[:1]
		pod.Status.ContainerStatuses = pod.Status.ContainerStatuses[:1]
		podDiscovery.OnUpdate(nil, pod)
		// Removed container that is tombstoned, but not removed yet
		cleanTombstonedTargets(podDiscovery)
		require.Equal(t, 0, len(tailSource.targetRemoves))

		fakeNow.Advance(1 * time.Minute)
		fakeNow.Advance(1 * time.Millisecond)
		cleanTombstonedTargets(podDiscovery)
		require.Equal(t, 1, len(tailSource.targetRemoves))    // removed tombstoned target
		require.Equal(t, 1, len(podDiscovery.podidToTargets)) // still tracking the pod and a container

		err = podDiscovery.Close()
		require.NoError(t, err)
	})

	t.Run("Container Added, Removed, Added", func(t *testing.T) {
		fakeNow, tailSource, podDiscovery := create()
		err := podDiscovery.Open(context.Background())
		defer tailSource.Close()
		require.NoError(t, err)

		pod := scrapedPod("pod1")
		pod.Spec.Containers = append(pod.Spec.Containers, v1.Container{
			Name:  "container2",
			Image: "image2",
		})
		pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, v1.ContainerStatus{
			Name:        "container2",
			Image:       "image2",
			ContainerID: "docker://container2id",
		})
		podDiscovery.OnUpdate(nil, pod)
		// New container added to scrape
		require.Equal(t, 2, len(tailSource.targetAdds))
		require.Equal(t, 0, len(tailSource.targetRemoves))

		savedContainerList := pod.Spec.Containers
		savedContainerStatuses := pod.Status.ContainerStatuses
		pod.Spec.Containers = pod.Spec.Containers[:1]
		pod.Status.ContainerStatuses = pod.Status.ContainerStatuses[:1]
		podDiscovery.OnUpdate(nil, pod)
		// Removed container that is tombstoned, but not removed yet
		cleanTombstonedTargets(podDiscovery)
		require.Equal(t, 0, len(tailSource.targetRemoves))

		pod.Spec.Containers = savedContainerList
		pod.Status.ContainerStatuses = savedContainerStatuses
		podDiscovery.OnUpdate(nil, pod)
		// Nothing new added, nothing removed either.
		require.Equal(t, 2, len(tailSource.targetAdds))
		require.Equal(t, 0, len(tailSource.targetRemoves))

		fakeNow.Advance(1 * time.Minute)
		fakeNow.Advance(1 * time.Millisecond)
		cleanTombstonedTargets(podDiscovery)
		require.Equal(t, 0, len(tailSource.targetRemoves))    // Nothing removed, since it was readded
		require.Equal(t, 1, len(podDiscovery.podidToTargets)) // still tracking the pod

		err = podDiscovery.Close()
		require.NoError(t, err)
	})
}

func TestPodDiscoveryUpdatesDoNotBlockOnStuckTailer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_, tailSource, podDiscovery := create()
		defer tailSource.Close()
		tailSource.ignoreUpdates = true

		pod := scrapedPod("pod1")
		podDiscovery.OnAdd(pod, false)
		require.Len(t, tailSource.targetAdds, 1)

		done := make(chan struct{})
		go func() {
			defer close(done)
			for _, table := range []string{"t1", "t2", "t3"} {
				pod.Annotations["adx-mon/log-destination"] = "db:" + table
				podDiscovery.OnUpdate(nil, pod)
			}
		}()
		defer func() {
			for {
				select {
				case <-done:
					return
				case <-tailSource.lastUpdateChan:
				}
			}
		}()
		synctest.Wait()
		select {
		case <-done:
		default:
			t.Fatal("OnUpdate blocked on a tailer that is not reading updates")
		}

		select {
		case latest := <-tailSource.lastUpdateChan:
			require.Equal(t, "t3", latest.Table)
		default:
			t.Fatal("expected the latest update to be pending")
		}
	})
}

func TestPodDiscoverySlowRemovalDoesNotBlockPodEvents(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fakeNow, tailSource, podDiscovery := create()
		defer tailSource.Close()
		tailSource.removeRelease = make(chan struct{})
		releaseRemoval := sync.OnceFunc(func() { close(tailSource.removeRelease) })
		defer releaseRemoval()

		oldPod := scrapedPod("old")
		podDiscovery.OnAdd(oldPod, false)
		podDiscovery.OnDelete(oldPod)
		fakeNow.Advance(time.Minute + time.Millisecond)
		<-tailSource.targetAdds

		cleanupDone := make(chan struct{})
		go func() {
			defer close(cleanupDone)
			podDiscovery.cleanTombstonedTargets()
		}()
		removed := <-tailSource.targetRemoves
		if !podDiscovery.mut.TryLock() {
			t.Fatal("PodDiscovery mutex is held while removing a target")
		}
		podDiscovery.mut.Unlock()

		// RemoveTarget is blocked; pod events must still be processed.
		eventDone := make(chan struct{})
		go func() {
			defer close(eventDone)
			podDiscovery.OnAdd(scrapedPod("new"), false)
			// Re-adding the pod being removed must wait for the removal to finish.
			podDiscovery.OnUpdate(nil, oldPod)
		}()
		synctest.Wait()
		select {
		case <-eventDone:
		default:
			t.Fatal("pod events blocked on a slow target removal")
		}
		added := <-tailSource.targetAdds
		require.NotEqual(t, removed, added.FilePath)
		require.Len(t, tailSource.targetAdds, 0)

		releaseRemoval()
		synctest.Wait()
		select {
		case <-cleanupDone:
		default:
			t.Fatal("cleanup did not finish after removal was released")
		}

		readded := <-tailSource.targetAdds
		require.Equal(t, removed, readded.FilePath)

		podDiscovery.mut.Lock()
		defer podDiscovery.mut.Unlock()
		require.Empty(t, podDiscovery.removing)
		target := podDiscovery.podidToTargets[string(oldPod.UID)][removed]
		require.NotNil(t, target)
		require.False(t, target.pendingStart)
		require.NotNil(t, target.UpdateChan)
	})
}

func TestPodDiscoveryCloseFencesAlreadyDispatchedCallback(t *testing.T) {
	_, tailSource, podDiscovery := create()
	informer := &pausingPodInformer{inner: podDiscovery.podInformer, removed: make(chan struct{})}
	podDiscovery.podInformer = informer
	require.NoError(t, podDiscovery.Open(context.Background()))

	callbackEntered := make(chan struct{})
	releaseCallback := make(chan struct{})
	release := sync.OnceFunc(func() { close(releaseCallback) })
	defer func() {
		_ = podDiscovery.Close()
		tailSource.Close()
		release()
	}()
	callbackDone := make(chan struct{})
	go func() {
		defer close(callbackDone)
		close(callbackEntered)
		<-releaseCallback
		informer.handler.OnAdd(scrapedPod("late"), false)
	}()
	<-callbackEntered

	require.NoError(t, podDiscovery.Close())
	<-informer.removed
	tailSource.Close()
	release()
	<-callbackDone

	require.Len(t, tailSource.targetAdds, 0)
	require.Empty(t, podDiscovery.podidToTargets)
}

func TestPodDiscoveryCloseJoinsRemovalWithoutStartingPendingReplacement(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fakeNow, tailSource, podDiscovery := create()
		removeRelease := make(chan struct{})
		tailSource.removeRelease = removeRelease
		releaseRemoval := sync.OnceFunc(func() { close(removeRelease) })
		defer func() {
			releaseRemoval()
			_ = podDiscovery.Close()
			tailSource.Close()
		}()
		require.NoError(t, podDiscovery.Open(context.Background()))

		pod := scrapedPod("same-path")
		podDiscovery.OnAdd(pod, false)
		<-tailSource.targetAdds
		podDiscovery.OnDelete(pod)
		fakeNow.Advance(time.Minute + time.Millisecond)

		cleanupDone := make(chan struct{})
		podDiscovery.wg.Go(func() {
			defer close(cleanupDone)
			podDiscovery.cleanTombstonedTargets()
		})
		removedPath := <-tailSource.targetRemoves
		podDiscovery.OnAdd(pod, false)
		target := podDiscovery.podidToTargets[string(pod.UID)][removedPath]
		require.NotNil(t, target)
		require.True(t, target.pendingStart)

		closeDone := make(chan struct{})
		go func() {
			defer close(closeDone)
			_ = podDiscovery.Close()
		}()
		synctest.Wait()
		select {
		case <-closeDone:
			t.Fatal("Close returned before the in-progress removal finished")
		default:
		}

		releaseRemoval()
		synctest.Wait()
		select {
		case <-closeDone:
		default:
			t.Fatal("Close did not return after the removal finished")
		}
		select {
		case <-cleanupDone:
		default:
			t.Fatal("cleanup did not finish after removal was released")
		}
		require.Len(t, tailSource.targetAdds, 0)
		require.Empty(t, podDiscovery.removing)
		tailSource.Close()
	})
}

func TestPodDiscoveryDoesNotScheduleCleanupAfterClose(t *testing.T) {
	fakeNow, tailSource, podDiscovery := create()
	defer tailSource.Close()
	pod := scrapedPod("closed")
	podDiscovery.OnAdd(pod, false)
	podDiscovery.OnDelete(pod)
	fakeNow.Advance(time.Minute + time.Millisecond)

	require.NoError(t, podDiscovery.Close())
	podDiscovery.cleanTombstonedTargets()
	require.Len(t, tailSource.targetRemoves, 0)
	require.Empty(t, podDiscovery.removing)
}

func TestPodDiscoveryCannotReopenAfterClose(t *testing.T) {
	_, tailSource, podDiscovery := create()
	defer tailSource.Close()
	require.NoError(t, podDiscovery.Open(context.Background()))
	require.NoError(t, podDiscovery.Close())
	require.ErrorContains(t, podDiscovery.Open(context.Background()), "cannot be reopened")
}

func cleanTombstonedTargets(podDiscovery *PodDiscovery) {
	// Simulate the separate goroutine that cleans tombstoned targets
	errGroup, _ := errgroup.WithContext(context.Background())
	errGroup.Go(func() error {
		podDiscovery.cleanTombstonedTargets()
		return nil
	})
	errGroup.Wait()
}

func create() (*fakeNow, *fakeTailSource, *PodDiscovery) {
	fakeNow := newFakeNow()
	k8sCli := fake.NewSimpleClientset()
	tailSource := newFakeTailSource()
	podInformer := k8s.NewPodInformer(k8sCli, "node1")
	podDiscovery := NewPodDiscovery(PodDiscoveryOpts{
		NodeName:    "node1",
		PodInformer: podInformer,
	}, tailSource)
	podDiscovery.nowFunc = fakeNow.Now
	return fakeNow, tailSource, podDiscovery
}

func scrapedPod(name string) *v1.Pod {
	return &v1.Pod{
		Name:      name,
		Namespace: "default",
		UID:       types.UID(uuid.New().String()),
		Annotations: map[string]string{
			"adx-mon/scrape":          "true",
			"adx-mon/log-destination": "db:table",
		},
		Spec: v1.PodSpec{
			NodeName: "node1",
			Containers: []v1.Container{
				{
					Name:  "container1",
					Image: "image1",
				},
			},
		},
		Status: v1.PodStatus{
			Phase: v1.PodRunning,
			ContainerStatuses: []v1.ContainerStatus{
				{
					Name:        "container1",
					Image:       "image1",
					ContainerID: "docker://container1id",
				},
			},
		},
	}
}

func notScrapedPod(name string) *v1.Pod {
	return &v1.Pod{
		Name:        name,
		Namespace:   "default",
		UID:         types.UID(uuid.New().String()),
		Annotations: map[string]string{},
		Spec: v1.PodSpec{
			NodeName: "node1",
			Containers: []v1.Container{
				{
					Name:  "container1",
					Image: "image1",
				},
			},
		},
		Status: v1.PodStatus{
			Phase: v1.PodRunning,
			ContainerStatuses: []v1.ContainerStatus{
				{
					Name:        "container1",
					Image:       "image1",
					ContainerID: "docker://container1id",
				},
			},
		},
	}
}

type fakeTailSource struct {
	targetAdds    chan FileTailTarget
	targetRemoves chan string
	targetUpdates chan FileTailTarget
	addErrFunc    func(FileTailTarget) error
	closeFunc     context.CancelFunc
	ctx           context.Context

	// ignoreUpdates simulates a tailer that never reads its update channel.
	ignoreUpdates  bool
	lastUpdateChan <-chan FileTailTarget
	// removeRelease, when set, makes RemoveTarget block until it is closed.
	removeRelease chan struct{}
}

func newFakeTailSource() *fakeTailSource {
	return newFakeTailSourceWithErr(func(FileTailTarget) error { return nil })
}

func newFakeTailSourceWithErr(addErrFunc func(FileTailTarget) error) *fakeTailSource {
	ctx, cancel := context.WithCancel(context.Background())
	return &fakeTailSource{
		addErrFunc:    addErrFunc,
		targetAdds:    make(chan FileTailTarget, 1024),
		targetRemoves: make(chan string, 1024),
		targetUpdates: make(chan FileTailTarget, 1024),
		closeFunc:     cancel,
		ctx:           ctx,
	}
}

func (f *fakeTailSource) Close() {
	f.closeFunc()
}

func (f *fakeTailSource) AddTarget(target FileTailTarget, updateChan <-chan FileTailTarget) error {
	f.targetAdds <- target

	err := f.addErrFunc(target)
	if err != nil {
		return err
	}
	f.lastUpdateChan = updateChan
	if f.ignoreUpdates {
		return nil
	}
	go func() {
		for {
			select {
			case <-f.ctx.Done():
				return
			case update := <-updateChan:
				f.targetUpdates <- update
			}
		}
	}()
	return nil
}

func (f *fakeTailSource) removeTarget(filePath string) {
	f.targetRemoves <- filePath
	if f.removeRelease != nil {
		<-f.removeRelease
	}
}

type pausingPodInformer struct {
	inner   k8s.PodInformerInterface
	handler cache.ResourceEventHandler
	removed chan struct{}
}

func (i *pausingPodInformer) Add(ctx context.Context, handler cache.ResourceEventHandler) (cache.ResourceEventHandlerRegistration, error) {
	i.handler = handler
	return i.inner.Add(ctx, handler)
}

func (i *pausingPodInformer) Remove(registration cache.ResourceEventHandlerRegistration) error {
	close(i.removed)
	return i.inner.Remove(registration)
}

type fakeNow struct {
	Current time.Time
}

func newFakeNow() *fakeNow {
	return &fakeNow{Current: time.Now()}
}

func (fn *fakeNow) Now() time.Time {
	return fn.Current
}

func (fn *fakeNow) Advance(duration time.Duration) {
	fn.Current = fn.Current.Add(duration)
}
