package cluster

import (
	"context"
	"github.com/stretchr/testify/require"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	fakek8s "k8s.io/client-go/kubernetes/fake"
	v12 "k8s.io/client-go/listers/core/v1"
	"testing"
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
		WriteTimeSeriesFn:  nil,
		K8sCli:             kcli,
		Namespace:          "adx-mon",
		Hostname:           "ingestor-0",
		InsecureSkipVerify: false,
	})
	require.NoError(t, err)
	require.NoError(t, c.Open(context.Background()))

	coord := c.(*coordinator)
	require.Equal(t, 1, len(coord.peers))

	// Swap in fake pod lister to simulate a new peer
	coord.pl = &fakePodLister{pods: []*v1.Pod{self, newPeer}}

	coord.OnAdd(newPeer)
	require.Equal(t, 2, len(coord.peers))
	require.NoError(t, c.Close())

}

func TestCoordinator_DiscoveryDisabled(t *testing.T) {
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

	kcli := fakek8s.NewSimpleClientset(&v1.PodList{Items: []v1.Pod{*self}})

	// Test with no namespace or hostname, that discovery is disabled
	c, err := NewCoordinator(&CoordinatorOpts{
		WriteTimeSeriesFn:  nil,
		K8sCli:             kcli,
		Namespace:          "",
		Hostname:           "",
		InsecureSkipVerify: false,
	})
	require.NoError(t, c.Open(context.Background()))
	defer c.Close()

	require.NoError(t, err)

	coord := c.(*coordinator)
	require.Equal(t, 1, len(coord.peers))

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

	kcli := fakek8s.NewSimpleClientset(&v1.PodList{Items: []v1.Pod{*self, *newPeer}})

	c, err := NewCoordinator(&CoordinatorOpts{
		WriteTimeSeriesFn:  nil,
		K8sCli:             kcli,
		Namespace:          "adx-mon",
		Hostname:           "ingestor-0",
		InsecureSkipVerify: false,
	})
	require.NoError(t, err)
	require.NoError(t, c.Open(context.Background()))

	coord := c.(*coordinator)
	require.Equal(t, 2, len(coord.peers))

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
	coord.pl = &fakePodLister{pods: []*v1.Pod{self, newPeer}}

	coord.OnDelete(newPeer)
	require.Equal(t, 1, len(coord.peers))
	require.NoError(t, c.Close())

}

type fakePodLister struct {
	pods []*v1.Pod
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
	return l.pods, nil
}

func (l *fakePodLister) Pods(namespace string) v12.PodNamespaceLister {
	return l
}
