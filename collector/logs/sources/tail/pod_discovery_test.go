package tail

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/k8s"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

var (
	assertPollInterval = 10 * time.Millisecond
	assertPollMax      = 1 * time.Second
)

func TestPodDiscoveryLifecycle(t *testing.T) {
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
		podDiscovery.OnUpdate(nil, pod)
		// New container added to scrape
		require.Equal(t, 2, len(tailSource.targetAdds))
		require.Equal(t, 0, len(tailSource.targetRemoves))

		pod.Spec.Containers = pod.Spec.Containers[:1]
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
		podDiscovery.OnUpdate(nil, pod)
		// New container added to scrape
		require.Equal(t, 2, len(tailSource.targetAdds))
		require.Equal(t, 0, len(tailSource.targetRemoves))

		savedContainerList := pod.Spec.Containers
		pod.Spec.Containers = pod.Spec.Containers[:1]
		podDiscovery.OnUpdate(nil, pod)
		// Removed container that is tombstoned, but not removed yet
		cleanTombstonedTargets(podDiscovery)
		require.Equal(t, 0, len(tailSource.targetRemoves))

		pod.Spec.Containers = savedContainerList
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
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			UID:       types.UID(uuid.New().String()),
			Annotations: map[string]string{
				"adx-mon/scrape":          "true",
				"adx-mon/log-destination": "db:table",
			},
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
	}
}

func notScrapedPod(name string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   "default",
			UID:         types.UID(uuid.New().String()),
			Annotations: map[string]string{},
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
	}
}

type fakeTailSource struct {
	targetAdds    chan FileTailTarget
	targetRemoves chan string
	targetUpdates chan FileTailTarget
	addErrFunc    func(FileTailTarget) error
	closeFunc     context.CancelFunc
	ctx           context.Context
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

func (f *fakeTailSource) RemoveTarget(filePath string) {
	f.targetRemoves <- filePath
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
