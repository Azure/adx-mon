package cluster

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	fakek8s "k8s.io/client-go/kubernetes/fake"
	v12 "k8s.io/client-go/listers/core/v1"

	"github.com/Azure/adx-mon/pkg/k8s"
	"github.com/Azure/adx-mon/pkg/testutils"
)

func TestCoordinator_NewPeer(t *testing.T) {
	self := &v1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ingestor-0",
			Namespace: "adx-mon",
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind: "StatefulSet",
					Name: "ingestor",
				},
			},
		},
		Status: v1.PodStatus{
			PodIP: "10.200.0.1",
			Conditions: []v1.PodCondition{
				{
					Type:   v1.PodInitialized,
					Status: v1.ConditionTrue,
				},
				{
					Type:   v1.PodReady,
					Status: v1.ConditionTrue,
				},
			},
		},
	}

	newPeer := &v1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ingestor-1",
			Namespace: "adx-mon",
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind: "StatefulSet",
					Name: "ingestor",
				},
			},
		},
		Status: v1.PodStatus{
			PodIP: "10.200.0.2",
			Conditions: []v1.PodCondition{
				{
					Type:   v1.PodInitialized,
					Status: v1.ConditionTrue,
				},
				{
					Type:   v1.PodReady,
					Status: v1.ConditionTrue,
				},
			},
		},
	}

	kcli := fakek8s.NewSimpleClientset(&v1.PodList{Items: []v1.Pod{*self}})

	c, err := NewCoordinator(&CoordinatorOpts{
		K8sCli:             kcli,
		Namespace:          "adx-mon",
		Hostname:           "ingestor-0",
		InsecureSkipVerify: false,
	})
	require.NoError(t, err)
	require.NoError(t, c.Open(context.Background()))

	coord := c.(*coordinator)
	coord.mu.RLock()
	require.Equal(t, 1, len(coord.peers))
	coord.mu.RUnlock()

	// Swap in fake pod lister to simulate a new peer
	coord.mu.Lock()
	coord.pl = &fakePodLister{pods: []*v1.Pod{self, newPeer}}
	coord.mu.Unlock()

	coord.OnAdd(newPeer, false)
	coord.mu.RLock()
	require.Equal(t, 2, len(coord.peers))
	coord.mu.RUnlock()
	require.NoError(t, c.Close())

}

func TestCoordinator_LostPeer(t *testing.T) {
	self := &v1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ingestor-0",
			Namespace: "adx-mon",
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind: "StatefulSet",
					Name: "ingestor",
				},
			},
			Labels: map[string]string{
				"app": "ingestor",
			},
		},
		Status: v1.PodStatus{
			PodIP: "10.200.0.1",
			Conditions: []v1.PodCondition{
				{
					Type:   v1.PodInitialized,
					Status: v1.ConditionTrue,
				},
				{
					Type:   v1.PodReady,
					Status: v1.ConditionTrue,
				},
			},
		},
	}

	newPeer := &v1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ingestor-1",
			Namespace: "adx-mon",
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind: "StatefulSet",
					Name: "ingestor",
				},
			},
			Labels: map[string]string{
				"app": "ingestor",
			},
		},
		Status: v1.PodStatus{
			PodIP: "10.200.0.2",
			Conditions: []v1.PodCondition{
				{
					Type:   v1.PodInitialized,
					Status: v1.ConditionTrue,
				},
				{
					Type:   v1.PodReady,
					Status: v1.ConditionTrue,
				},
			},
		},
	}

	kcli := fakek8s.NewSimpleClientset(&v1.PodList{Items: []v1.Pod{*self, *newPeer}})

	c, err := NewCoordinator(&CoordinatorOpts{
		K8sCli:             kcli,
		Namespace:          "adx-mon",
		Hostname:           "ingestor-0",
		InsecureSkipVerify: false,
	})
	require.NoError(t, err)
	require.NoError(t, c.Open(context.Background()))

	coord := c.(*coordinator)
	coord.mu.RLock()
	require.Equal(t, 2, len(coord.peers))
	coord.mu.RUnlock()

	newPeer = &v1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ingestor-1",
			Namespace: "adx-mon",
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind: "StatefulSet",
					Name: "ingestor",
				},
			},
		},
		Status: v1.PodStatus{
			PodIP: "10.200.0.2",
			Conditions: []v1.PodCondition{
				{
					Type:   v1.PodInitialized,
					Status: v1.ConditionTrue,
				},
				{
					Type:   v1.PodReady,
					Status: v1.ConditionFalse, // Pod went NotReady
				},
			},
		},
	}

	// Swap in fake pod lister to simulate a new peer
	coord.mu.Lock()
	coord.pl = &fakePodLister{pods: []*v1.Pod{self, newPeer}}
	coord.mu.Unlock()

	coord.OnDelete(newPeer)
	coord.mu.RLock()
	require.Equal(t, 1, len(coord.peers))
	coord.mu.RUnlock()
	require.NoError(t, c.Close())

}

type fakePodLister struct {
	pods   []*v1.Pod
	onList func()
}

func (l *fakePodLister) Get(name string) (*v1.Pod, error) {
	for _, p := range l.pods {
		if p.Name == name {
			return p, nil
		}
	}
	return nil, nil
}

func (l *fakePodLister) List(selector labels.Selector) (ret []*v1.Pod, err error) {
	if l.onList != nil {
		l.onList()
	}
	return l.pods, nil
}

func (l *fakePodLister) Pods(namespace string) v12.PodNamespaceLister {
	return l
}

func TestCoordinatorInK8s(t *testing.T) {
	testutils.IntegrationTest(t)

	ctx := context.Background()
	k3sContainer, err := k3s.Run(ctx, "rancher/k3s:v1.31.2-k3s1")
	testcontainers.CleanupContainer(t, k3sContainer)
	require.NoError(t, err)

	kubeconfig, err := testutils.WriteKubeConfig(ctx, k3sContainer, t.TempDir())
	require.NoError(t, err)

	config, err := k8s.BuildConfigFromFlags("", kubeconfig)
	require.NoError(t, err)

	client, err := kubernetes.NewForConfig(config)
	require.NoError(t, err)

	opts := &CoordinatorOpts{
		K8sCli:             client,
		Namespace:          "test-namespace",
		Hostname:           "ingestor-0",
		InsecureSkipVerify: true,
	}
	c, err := NewCoordinator(opts)
	require.NoError(t, err)

	require.NoError(t, c.Open(ctx))

	ns := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: opts.Namespace,
		},
	}
	_, err = client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	require.NoError(t, err)

	replicas := int32(1)
	ss := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ingestor",
			Namespace: opts.Namespace,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "ingestor",
				},
			},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "ingestor",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  "ingestor",
							Image: "mcr.microsoft.com/cbl-mariner/base/nginx:1.22-cm2.0",
						},
					},
				},
			},
		},
	}
	_, err = client.AppsV1().StatefulSets(opts.Namespace).Create(ctx, ss, metav1.CreateOptions{})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return c.IsLeader()
	}, time.Minute, 100*time.Millisecond)

	require.NoError(t, c.Close())
}

func newPeerPod(name string, ready bool) *v1.Pod {
	status := v1.ConditionFalse
	if ready {
		status = v1.ConditionTrue
	}
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       "adx-mon",
			OwnerReferences: []metav1.OwnerReference{{Kind: "StatefulSet", Name: "ingestor"}},
		},
		Status: v1.PodStatus{
			PodIP: "10.200.0.1",
			Conditions: []v1.PodCondition{
				{Type: v1.PodInitialized, Status: v1.ConditionTrue},
				{Type: v1.PodReady, Status: status},
			},
		},
	}
}

// newPeerTestCoordinator returns a coordinator for hostname that lists pods from the returned lister without
// starting informers.
func newPeerTestCoordinator(t *testing.T, hostname string) (*coordinator, *fakePodLister) {
	t.Helper()
	c, err := NewCoordinator(&CoordinatorOpts{Namespace: "adx-mon", Hostname: hostname})
	require.NoError(t, err)
	coord := c.(*coordinator)
	lister := &fakePodLister{}
	coord.pl = lister
	coord.peers = map[string]string{hostname: "https://127.0.0.1:9090"}
	return coord, lister
}

func TestCoordinator_PeersDefaultsToSelf(t *testing.T) {
	c, _ := newPeerTestCoordinator(t, "ingestor-0")
	require.Equal(t, PeerInfo{Count: 1, Rank: 0}, c.Peers())
}

func TestCoordinator_PeersCountAndRank(t *testing.T) {
	c, lister := newPeerTestCoordinator(t, "ingestor-1")
	lister.pods = []*v1.Pod{newPeerPod("ingestor-2", true), newPeerPod("ingestor-0", true), newPeerPod("ingestor-1", true)}

	require.NoError(t, c.syncPeers())
	require.Equal(t, PeerInfo{Count: 3, Rank: 1}, c.Peers())
}

func TestCoordinator_PeersCountsSelfWhenNotReady(t *testing.T) {
	c, lister := newPeerTestCoordinator(t, "ingestor-1")
	lister.pods = []*v1.Pod{newPeerPod("ingestor-0", true), newPeerPod("ingestor-1", false), newPeerPod("ingestor-2", false)}

	require.NoError(t, c.syncPeers())
	require.Equal(t, PeerInfo{Count: 2, Rank: 1}, c.Peers())
}

func TestCoordinator_PeerRanksAreUnique(t *testing.T) {
	names := []string{"ingestor-0", "ingestor-1", "ingestor-10", "ingestor-2", "ingestor-3"}
	var pods []*v1.Pod
	for _, name := range names {
		pods = append(pods, newPeerPod(name, true))
	}

	ranks := map[int]string{}
	for _, name := range names {
		c, lister := newPeerTestCoordinator(t, name)
		lister.pods = pods
		require.NoError(t, c.syncPeers())

		info := c.Peers()
		require.Equal(t, len(names), info.Count)
		_, dup := ranks[info.Rank]
		require.False(t, dup, "duplicate rank %d", info.Rank)
		ranks[info.Rank] = name
	}
	require.Len(t, ranks, len(names))
}

func TestCoordinator_SubscribePeers(t *testing.T) {
	c, lister := newPeerTestCoordinator(t, "ingestor-1")

	var got []PeerInfo
	unsubscribe := c.SubscribePeers(func(info PeerInfo) { got = append(got, info) })

	// Initial sync with only this node does not change the default.
	lister.pods = []*v1.Pod{newPeerPod("ingestor-1", true)}
	require.NoError(t, c.syncPeers())
	require.Empty(t, got)

	lister.pods = append(lister.pods, newPeerPod("ingestor-0", true))
	require.NoError(t, c.syncPeers())
	require.Equal(t, []PeerInfo{{Count: 2, Rank: 1}}, got)

	// A resync without changes does not notify.
	require.NoError(t, c.syncPeers())
	require.Len(t, got, 1)

	lister.pods = lister.pods[:1]
	require.NoError(t, c.syncPeers())
	require.Equal(t, []PeerInfo{{Count: 2, Rank: 1}, {Count: 1, Rank: 0}}, got)

	unsubscribe()
	lister.pods = append(lister.pods, newPeerPod("ingestor-2", true))
	require.NoError(t, c.syncPeers())
	require.Len(t, got, 2)
	require.Equal(t, PeerInfo{Count: 2, Rank: 0}, c.Peers())
}

func TestCoordinator_SubscriberCanReadPeers(t *testing.T) {
	// Subscribers are notified without holding the coordinator lock.
	c, lister := newPeerTestCoordinator(t, "ingestor-0")
	var got PeerInfo
	c.SubscribePeers(func(PeerInfo) { got = c.Peers() })

	lister.pods = []*v1.Pod{newPeerPod("ingestor-0", true), newPeerPod("ingestor-1", true)}
	require.NoError(t, c.syncPeers())
	require.Equal(t, PeerInfo{Count: 2, Rank: 0}, got)
}

func TestCoordinator_PeerNotificationsStayOrdered(t *testing.T) {
	c, lister := newPeerTestCoordinator(t, "ingestor-0")
	firstStarted, unblockFirst := make(chan struct{}), make(chan struct{})
	secondNotified := make(chan struct{}, 1)
	var mu sync.Mutex
	var observed PeerInfo
	c.SubscribePeers(func(info PeerInfo) {
		if info.Count == 2 {
			close(firstStarted)
			<-unblockFirst
		}
		if info.Count == 3 {
			secondNotified <- struct{}{}
		}
		mu.Lock()
		observed = info
		mu.Unlock()
	})

	lister.pods = []*v1.Pod{newPeerPod("ingestor-0", true), newPeerPod("ingestor-1", true)}
	firstDone := make(chan struct{})
	go func() { defer close(firstDone); require.NoError(t, c.syncPeers()) }()
	<-firstStarted

	lister.pods = append(lister.pods, newPeerPod("ingestor-2", true))
	secondListStarted, allowSecondList := make(chan struct{}), make(chan struct{})
	secondSyncStarted := make(chan struct{})
	var listCalls atomic.Int32
	lister.onList = func() {
		if listCalls.Add(1) == 1 {
			close(secondListStarted)
			<-allowSecondList
		}
	}
	secondDone := make(chan struct{})
	go func() {
		defer close(secondDone)
		close(secondSyncStarted)
		require.NoError(t, c.syncPeers())
	}()
	<-secondSyncStarted

	// With syncMu removed, the second sync can list and notify while the first callback is blocked.
	select {
	case <-secondListStarted:
		close(allowSecondList)
		select {
		case <-secondNotified:
			t.Error("second peer notification ran before the first callback completed")
		case <-time.After(time.Second):
			t.Error("second sync did not notify while the first callback was blocked")
		}
	case <-time.After(100 * time.Millisecond):
		close(allowSecondList)
	}
	close(unblockFirst)
	<-firstDone
	<-secondDone
	require.NoError(t, c.syncPeers())

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, c.Peers(), observed, "subscriber must retain the latest peer allocation")
}
