package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/otlp"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/testutils"
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
	"k8s.io/client-go/tools/clientcmd"
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
		WriteTimeSeriesFn:  nil,
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

func TestCoordinatorInK8s(t *testing.T) {
	testutils.IntegrationTest(t)

	// This is an integration test where a Coordinator is created using a k3s cluster
	// configuration and a statefulset is then created that matches the Coordinator's
	// peer predicate. What we're testing is that the Coordinator is correctly utilizing
	// a k8s informer and its predicate is correctly identifying peer pods and its leader state.
	otlpWriter := func(ctx context.Context, database, table string, logs *otlp.Logs) error {
		t.Helper()
		return nil
	}

	timeSeriesWriter := func(ctx context.Context, ts []*prompb.TimeSeries) error {
		t.Helper()
		return nil
	}

	ctx := context.Background()
	k3sContainer, err := k3s.Run(ctx, "rancher/k3s:v1.31.2-k3s1")
	testcontainers.CleanupContainer(t, k3sContainer)
	require.NoError(t, err)

	kubeconfig, err := testutils.WriteKubeConfig(ctx, k3sContainer, t.TempDir())
	require.NoError(t, err)

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	require.NoError(t, err)

	client, err := kubernetes.NewForConfig(config)
	require.NoError(t, err)

	opts := &CoordinatorOpts{
		WriteTimeSeriesFn:  timeSeriesWriter,
		WriteOTLPLogsFn:    otlpWriter,
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

	require.NoError(t, c.WriteOTLPLogs(ctx, "test-database", "test-table", &otlp.Logs{}))
	require.NoError(t, c.Write(ctx, &prompb.WriteRequest{}))

	require.Eventually(t, func() bool {
		return c.IsLeader()
	}, time.Minute, 100*time.Millisecond)

	require.NoError(t, c.Close())
}
