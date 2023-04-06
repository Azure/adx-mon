package collector_test

import (
	"context"
	"github.com/davecgh/go-spew/spew"
	"testing"
	"time"

	"github.com/Azure/adx-mon/collector"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

const MetricListenAddr = ":9090"

func TestService_Open(t *testing.T) {
	cli := fake.NewSimpleClientset()
	s, err := collector.NewService(&collector.ServiceOpts{
		ListentAddr:    MetricListenAddr,
		K8sCli:         cli,
		ScrapeInterval: 10 * time.Second,
	})
	require.NoError(t, err)
	require.NoError(t, s.Open(context.Background()))
	defer s.Close()

	require.Equal(t, 0, len(s.Targets()))
}

func TestService_Open_Static(t *testing.T) {
	cli := fake.NewSimpleClientset()
	s, err := collector.NewService(&collector.ServiceOpts{
		ListentAddr:    MetricListenAddr,
		K8sCli:         cli,
		Targets:        []string{"http://localhost:8080/metrics"},
		ScrapeInterval: 10 * time.Second,
	})
	require.NoError(t, err)
	require.NoError(t, s.Open(context.Background()))
	defer s.Close()

	require.Equal(t, 1, len(s.Targets()))
}

func TestService_Open_NoMatchingHost(t *testing.T) {
	cli := fake.NewSimpleClientset(fakePod("default", "pod1", map[string]string{"app": "test"}, "node1"))
	s, err := collector.NewService(&collector.ServiceOpts{
		ListentAddr:    MetricListenAddr,
		K8sCli:         cli,
		NodeName:       "ks8-master-123",
		Targets:        []string{"http://localhost:8080/metrics"},
		ScrapeInterval: 10 * time.Second,
	})
	require.NoError(t, err)
	require.NoError(t, s.Open(context.Background()))
	defer s.Close()

	require.Equal(t, 1, len(s.Targets()))
}

func TestService_Open_NoMetricsAnnotations(t *testing.T) {
	cli := fake.NewSimpleClientset(fakePod("default", "pod1", map[string]string{"app": "test"}, "ks8-master-123"))
	s, err := collector.NewService(&collector.ServiceOpts{
		ListentAddr:    MetricListenAddr,
		K8sCli:         cli,
		NodeName:       "ks8-master-123",
		Targets:        []string{"http://localhost:8080/metrics"},
		ScrapeInterval: 10 * time.Second,
	})
	require.NoError(t, err)
	require.NoError(t, s.Open(context.Background()))
	defer s.Close()

	require.Equal(t, 1, len(s.Targets()))
}

func TestService_Open_Matching(t *testing.T) {
	pod := fakePod("default", "pod1", map[string]string{"app": "test"}, "ks8-master-123")
	pod.Annotations = map[string]string{
		"prometheus.io/scrape": "true",
	}
	pod.Status.PodIP = "172.31.1.18"
	pod.Spec.Containers = []v1.Container{
		{
			Ports: []v1.ContainerPort{
				{
					ContainerPort: 9000,
				},
			},
		},
	}
	cli := fake.NewSimpleClientset(pod)
	s, err := collector.NewService(&collector.ServiceOpts{
		ListentAddr:    MetricListenAddr,
		K8sCli:         cli,
		NodeName:       "ks8-master-123",
		Targets:        []string{"http://localhost:8080/metrics"},
		ScrapeInterval: 10 * time.Second,
	})
	require.NoError(t, err)
	require.NoError(t, s.Open(context.Background()))
	defer s.Close()

	targets := s.Targets()
	spew.Dump(targets)
	require.Equal(t, 2, len(targets))
	require.Equal(t, "http://localhost:8080/metrics", targets[0].Addr)
	require.Equal(t, "http://172.31.1.18:9000/metrics", targets[1].Addr)
}

func TestService_Open_MatchingPort(t *testing.T) {
	pod := fakePod("default", "pod1", map[string]string{"app": "test"}, "ks8-master-123")
	pod.Annotations = map[string]string{
		"prometheus.io/scrape": "true",
		"prometheus.io/port":   "8080",
	}
	pod.Status.PodIP = "172.31.1.18"
	cli := fake.NewSimpleClientset(pod)
	s, err := collector.NewService(&collector.ServiceOpts{
		ListentAddr:    MetricListenAddr,
		K8sCli:         cli,
		NodeName:       "ks8-master-123",
		Targets:        []string{"http://localhost:8080/metrics"},
		ScrapeInterval: 10 * time.Second,
	})
	require.NoError(t, err)
	require.NoError(t, s.Open(context.Background()))
	defer s.Close()

	targets := s.Targets()
	require.Equal(t, 2, len(targets))
	require.Equal(t, "http://localhost:8080/metrics", targets[0].Addr)
	require.Equal(t, "http://172.31.1.18:8080/metrics", targets[1].Addr)
}

func fakePod(namespace, name string, labels map[string]string, node string) *v1.Pod {
	m := map[string]string{}
	for k, v := range labels {
		m[k] = v
	}

	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels:    m,
		},
		Spec: v1.PodSpec{
			NodeName: node,
		},
	}
}
