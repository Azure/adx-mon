package collector

import (
	"context"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/k8s"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
)

const MetricListenAddr = ":9090"

func TestService_Open(t *testing.T) {
	dir := t.TempDir()
	cli := fake.NewSimpleClientset()
	informer := k8s.NewPodInformer(cli, "ks8-master-123")
	s, err := NewService(&ServiceOpts{
		StorageDir: dir,
		ListenAddr: MetricListenAddr,
		Scraper: &ScraperOpts{
			PodInformer:    informer,
			ScrapeInterval: 10 * time.Second,
		},
	})
	require.NoError(t, err)
	require.NoError(t, s.Open(context.Background()))
	defer s.Close()

	require.Equal(t, 0, len(s.scraper.Targets()))
}

func TestService_Open_Static(t *testing.T) {
	dir := t.TempDir()
	cli := fake.NewSimpleClientset()
	informer := k8s.NewPodInformer(cli, "ks8-master-123")
	s, err := NewService(&ServiceOpts{
		StorageDir: dir,
		ListenAddr: MetricListenAddr,
		Scraper: &ScraperOpts{
			PodInformer:    informer,
			ScrapeInterval: 10 * time.Second,
			Targets: []ScrapeTarget{
				{Addr: "http://localhost:8080/metrics"},
			},
		},
	})
	require.NoError(t, err)
	require.NoError(t, s.Open(context.Background()))
	defer s.Close()

	require.Equal(t, 1, len(s.scraper.Targets()))
}

func TestService_Open_NoMatchingHost(t *testing.T) {
	dir := t.TempDir()
	cli := fake.NewSimpleClientset(fakePod("default", "pod1", map[string]string{"app": "test"}, "node1"))
	informer := k8s.NewPodInformer(cli, "ks8-master-123")
	s, err := NewService(&ServiceOpts{
		StorageDir: dir,
		ListenAddr: MetricListenAddr,
		Scraper: &ScraperOpts{
			PodInformer:    informer,
			NodeName:       "ks8-master-123",
			ScrapeInterval: 10 * time.Second,
			Targets: []ScrapeTarget{
				{Addr: "http://localhost:8080/metrics"},
			},
		},
	})
	require.NoError(t, err)
	require.NoError(t, s.Open(context.Background()))
	defer s.Close()

	require.Equal(t, 1, len(s.scraper.Targets()))
}

func TestService_Open_NoMetricsAnnotations(t *testing.T) {
	dir := t.TempDir()
	cli := fake.NewSimpleClientset(fakePod("default", "pod1", map[string]string{"app": "test"}, "ks8-master-123"))
	informer := k8s.NewPodInformer(cli, "ks8-master-123")
	s, err := NewService(&ServiceOpts{
		StorageDir: dir,
		ListenAddr: MetricListenAddr,
		Scraper: &ScraperOpts{
			PodInformer:    informer,
			NodeName:       "ks8-master-123",
			ScrapeInterval: 10 * time.Second,
			Targets: []ScrapeTarget{
				{Addr: "http://localhost:8080/metrics"},
			},
		},
	})
	require.NoError(t, err)
	require.NoError(t, s.Open(context.Background()))
	defer s.Close()

	require.Equal(t, 1, len(s.scraper.Targets()))
}

func TestService_Open_Matching(t *testing.T) {
	dir := t.TempDir()
	pod := fakePod("default", "pod1", map[string]string{"app": "test"}, "ks8-master-123")
	pod.Annotations = map[string]string{
		"adx-mon/scrape": "true",
	}
	pod.Status.PodIP = "172.31.1.18"
	pod.Spec.Containers = []v1.Container{
		{
			Name: "container",
			Ports: []v1.ContainerPort{
				{
					ContainerPort: 9000,
				},
			},
		},
	}
	cli := fake.NewSimpleClientset(pod)
	informer := k8s.NewPodInformer(cli, "ks8-master-123")
	s, err := NewService(&ServiceOpts{
		StorageDir: dir,
		ListenAddr: MetricListenAddr,
		Scraper: &ScraperOpts{
			PodInformer:    informer,
			NodeName:       "ks8-master-123",
			ScrapeInterval: 10 * time.Second,
			Targets: []ScrapeTarget{
				{
					Addr:      "http://localhost:8080/metrics",
					Namespace: "namespace",
					Pod:       "pod",
					Container: "container",
				},
			},
		},
	})
	require.NoError(t, err)
	require.NoError(t, s.Open(context.Background()))
	defer s.Close()

	targets := s.scraper.Targets()
	require.Equal(t, 2, len(targets))
	require.Equal(t, "http://localhost:8080/metrics", targets[0].Addr)
	require.Equal(t, "namespace", targets[0].Namespace)
	require.Equal(t, "pod", targets[0].Pod)
	require.Equal(t, "container", targets[0].Container)

	require.Equal(t, "http://172.31.1.18:9000/metrics", targets[1].Addr)
	require.Equal(t, "container", targets[1].Container)
	require.Equal(t, "default", targets[1].Namespace)
}

func TestService_Open_HostPort(t *testing.T) {
	dir := t.TempDir()
	pod := fakePod("default", "pod1", map[string]string{"app": "test"}, "ks8-master-123")
	pod.Annotations = map[string]string{
		"adx-mon/scrape": "true",
		"adx-mon/port":   "10254",
	}
	pod.Status.PodIP = "172.31.1.18"
	pod.Spec.Containers = []v1.Container{
		{
			Name: "container",
			Ports: []v1.ContainerPort{
				{
					ContainerPort: 9000,
				},
			},
			ReadinessProbe: &v1.Probe{
				ProbeHandler: v1.ProbeHandler{
					HTTPGet: &v1.HTTPGetAction{
						Port: intstr.FromInt(10254),
					},
				},
			},
		},
	}
	cli := fake.NewSimpleClientset(pod)
	informer := k8s.NewPodInformer(cli, "ks8-master-123")
	s, err := NewService(&ServiceOpts{
		StorageDir: dir,
		ListenAddr: MetricListenAddr,
		Scraper: &ScraperOpts{
			PodInformer:    informer,
			NodeName:       "ks8-master-123",
			ScrapeInterval: 10 * time.Second,
		},
	})
	require.NoError(t, err)
	require.NoError(t, s.Open(context.Background()))
	defer s.Close()

	targets := s.scraper.Targets()
	require.Equal(t, 1, len(targets))
	require.Equal(t, "http://172.31.1.18:10254/metrics", targets[0].Addr)
	require.Equal(t, "default", targets[0].Namespace)
	require.Equal(t, "pod1", targets[0].Pod)
	require.Equal(t, "container", targets[0].Container)
}

func TestService_Open_MatchingPort(t *testing.T) {
	dir := t.TempDir()
	pod := fakePod("default", "pod1", map[string]string{"app": "test"}, "ks8-master-123")
	pod.Annotations = map[string]string{
		"adx-mon/scrape": "true",
		"adx-mon/port":   "8080",
	}
	pod.Spec.Containers = []v1.Container{
		{
			Name: "container",
			Ports: []v1.ContainerPort{
				{
					ContainerPort: 8080,
				},
			},
		},
	}
	pod.Status.PodIP = "172.31.1.18"
	cli := fake.NewSimpleClientset(pod)
	informer := k8s.NewPodInformer(cli, "ks8-master-123")
	s, err := NewService(&ServiceOpts{
		StorageDir: dir,
		ListenAddr: MetricListenAddr,
		Scraper: &ScraperOpts{
			PodInformer:    informer,
			NodeName:       "ks8-master-123",
			ScrapeInterval: 10 * time.Second,
			Targets: []ScrapeTarget{
				{Addr: "http://localhost:8080/metrics"},
			},
		},
	})
	require.NoError(t, err)
	require.NoError(t, s.Open(context.Background()))
	defer s.Close()

	targets := s.scraper.Targets()
	require.Equal(t, 2, len(targets))
	require.Equal(t, "http://localhost:8080/metrics", targets[0].Addr)
	require.Equal(t, "http://172.31.1.18:8080/metrics", targets[1].Addr)
	require.Equal(t, "container", targets[1].Container)
	require.Equal(t, "default", targets[1].Namespace)
}

func TestMakeTargets(t *testing.T) {
	pod := fakePod("namespace", "pod", map[string]string{"app": "test"}, "node")
	pod.Annotations = map[string]string{
		"adx-mon/scrape": "true",
		"adx-mon/port":   "10254",
	}
	pod.Status.PodIP = "172.31.1.18"
	pod.Spec.Containers = []v1.Container{
		{
			Name: "container",
			ReadinessProbe: &v1.Probe{
				ProbeHandler: v1.ProbeHandler{
					HTTPGet: &v1.HTTPGetAction{
						Port: intstr.FromInt(10254),
					},
				},
			},
			Ports: []v1.ContainerPort{
				{
					ContainerPort: 8080,
				},
				{
					ContainerPort: 8081,
				},
				{
					ContainerPort: 8082,
				},
			},
		},
	}

	targets := makeTargets(pod)
	require.Equal(t, 1, len(targets))
}

func TestMakeTargetsFromList(t *testing.T) {
	pod := fakePod("namespace", "pod", map[string]string{"app": "test"}, "node")
	pod.Annotations = map[string]string{
		"adx-mon/scrape":  "true",
		"adx-mon/targets": "/metrics:10254, /somethingelse:9999",
	}
	pod.Status.PodIP = "172.31.1.18"
	pod.Spec.Containers = []v1.Container{
		{
			Name: "container",
			ReadinessProbe: &v1.Probe{
				ProbeHandler: v1.ProbeHandler{
					HTTPGet: &v1.HTTPGetAction{
						Port: intstr.FromInt(10254),
					},
				},
			},
			Ports: []v1.ContainerPort{
				{
					ContainerPort: 8080,
				},
				{
					ContainerPort: 8081,
				},
				{
					ContainerPort: 8082,
				},
			},
		},
		{
			Name: "othercontainer",
			ReadinessProbe: &v1.Probe{
				ProbeHandler: v1.ProbeHandler{
					HTTPGet: &v1.HTTPGetAction{
						Port: intstr.FromInt(11111),
					},
				},
			},
			Ports: []v1.ContainerPort{
				{
					ContainerPort: 8080,
				},
				{
					ContainerPort: 8081,
				},
				{
					ContainerPort: 9999,
				},
			},
		},
	}

	targets := makeTargets(pod)
	require.Equal(t, 2, len(targets))
	require.Equal(t, "http://172.31.1.18:10254/metrics", targets[0].Addr)
	require.Equal(t, "http://172.31.1.18:9999/somethingelse", targets[1].Addr)
	require.Equal(t, "othercontainer", targets[1].Container)
}

// invalid target port, should add all container ports to targets
func TestMakeTargetsFromListNegative(t *testing.T) {
	pod := fakePod("namespace", "pod", map[string]string{"app": "test"}, "node")
	pod.Annotations = map[string]string{
		"adx-mon/scrape":  "true",
		"adx-mon/targets": "/somethingelse:9999",
	}
	pod.Status.PodIP = "172.31.1.18"
	pod.Spec.Containers = []v1.Container{
		{
			Name: "container",
			ReadinessProbe: &v1.Probe{
				ProbeHandler: v1.ProbeHandler{
					HTTPGet: &v1.HTTPGetAction{
						Port: intstr.FromInt(10254),
					},
				},
			},
			Ports: []v1.ContainerPort{
				{
					ContainerPort: 8080,
				},
				{
					ContainerPort: 8081,
				},
				{
					ContainerPort: 8082,
				},
			},
		},
	}

	targets := makeTargets(pod)
	require.Equal(t, 0, len(targets))
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
