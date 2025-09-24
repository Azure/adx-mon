package collector

import (
	"context"
	"sort"
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
		DisableGzip: true,
	})
	require.NoError(t, err)
	require.NoError(t, s.Open(context.Background()))
	defer s.Close()

	// wait for the scraper to pick up the new target
	time.Sleep(100 * time.Millisecond)

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
		DisableGzip: true,
	})
	require.NoError(t, err)
	require.NoError(t, s.Open(context.Background()))
	defer s.Close()

	// wait for the scraper to pick up the new target
	time.Sleep(100 * time.Millisecond)

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
		DisableGzip: true,
	})
	require.NoError(t, err)
	require.NoError(t, s.Open(context.Background()))
	defer s.Close()

	// wait for the scraper to pick up the new target
	time.Sleep(100 * time.Millisecond)

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
		DisableGzip: true,
	})
	require.NoError(t, err)
	require.NoError(t, s.Open(context.Background()))
	defer s.Close()

	// wait for the scraper to pick up the new target
	time.Sleep(100 * time.Millisecond)

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
		DisableGzip: true,
	})
	require.NoError(t, err)
	require.NoError(t, s.Open(context.Background()))
	defer s.Close()

	// wait for the scraper to pick up the new target
	time.Sleep(100 * time.Millisecond)

	targets := s.scraper.Targets()
	require.Equal(t, 2, len(targets))
	require.Equal(t, "http://172.31.1.18:9000/metrics", targets[0].Addr)
	require.Equal(t, "container", targets[0].Container)
	require.Equal(t, "default", targets[0].Namespace)

	require.Equal(t, "http://localhost:8080/metrics", targets[1].Addr)
	require.Equal(t, "namespace", targets[1].Namespace)
	require.Equal(t, "pod", targets[1].Pod)
	require.Equal(t, "container", targets[1].Container)

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
		DisableGzip: true,
	})
	require.NoError(t, err)
	require.NoError(t, s.Open(context.Background()))
	defer s.Close()

	// wait for the scraper to pick up the new target
	time.Sleep(100 * time.Millisecond)

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
		DisableGzip: true,
	})
	require.NoError(t, err)
	require.NoError(t, s.Open(context.Background()))
	defer s.Close()

	// wait for the scraper to pick up the new target
	time.Sleep(100 * time.Millisecond)

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

func TestMakeTargets_NamedPort(t *testing.T) {
	pod := fakePod("foo", "bar", nil, "node")
	pod.Annotations = map[string]string{
		"adx-mon/scrape": "true",
		"adx-mon/port":   "metrics", // Use named port instead of numeric
	}

	pod.Status.PodIP = "1.2.3.4"
	pod.Spec.Containers = []v1.Container{
		{
			Name: "container",
			Ports: []v1.ContainerPort{
				{
					Name:          "metrics", // Named port
					ContainerPort: 8080,
				},
				{
					Name:          "health", // Another named port
					ContainerPort: 9000,
				},
			},
		},
	}

	targets := makeTargets(pod)
	require.Equal(t, 1, len(targets))
	require.Equal(t, "http://1.2.3.4:8080/metrics", targets[0].Addr)
	require.Equal(t, "foo", targets[0].Namespace)
	require.Equal(t, "bar", targets[0].Pod)
	require.Equal(t, "container", targets[0].Container)
}

func TestMakeTargets_NamedPortInTargets(t *testing.T) {
	pod := fakePod("foo", "bar", nil, "node")
	pod.Annotations = map[string]string{
		"adx-mon/scrape":  "true",
		"adx-mon/targets": "/metrics:metrics,/health:health", // Use named ports
	}

	pod.Status.PodIP = "1.2.3.4"
	pod.Spec.Containers = []v1.Container{
		{
			Name: "container",
			Ports: []v1.ContainerPort{
				{
					Name:          "metrics",
					ContainerPort: 8080,
				},
				{
					Name:          "health",
					ContainerPort: 9000,
				},
			},
		},
	}

	targets := makeTargets(pod)
	require.Equal(t, 2, len(targets))

	// Sort by port for consistent testing
	sort.Slice(targets, func(i, j int) bool {
		return targets[i].Addr < targets[j].Addr
	})

	require.Equal(t, "http://1.2.3.4:8080/metrics", targets[0].Addr)
	require.Equal(t, "http://1.2.3.4:9000/health", targets[1].Addr)
}

func TestMakeTargets_MixedNamedAndNumericPorts(t *testing.T) {
	pod := fakePod("foo", "bar", nil, "node")
	pod.Annotations = map[string]string{
		"adx-mon/scrape":  "true",
		"adx-mon/targets": "/metrics:metrics,/health:9001", // Mix named and numeric ports
	}

	pod.Status.PodIP = "1.2.3.4"
	pod.Spec.Containers = []v1.Container{
		{
			Name: "container",
			Ports: []v1.ContainerPort{
				{
					Name:          "metrics",
					ContainerPort: 8080,
				},
				{
					ContainerPort: 9001, // No name for this port
				},
			},
		},
	}

	targets := makeTargets(pod)
	require.Equal(t, 2, len(targets))

	// Sort by port for consistent testing
	sort.Slice(targets, func(i, j int) bool {
		return targets[i].Addr < targets[j].Addr
	})

	require.Equal(t, "http://1.2.3.4:8080/metrics", targets[0].Addr)
	require.Equal(t, "http://1.2.3.4:9001/health", targets[1].Addr)
}

func TestMakeTargets_NamedPortNotFound(t *testing.T) {
	pod := fakePod("foo", "bar", nil, "node")
	pod.Annotations = map[string]string{
		"adx-mon/scrape": "true",
		"adx-mon/port":   "nonexistent", // Named port that doesn't exist
	}

	pod.Status.PodIP = "1.2.3.4"
	pod.Spec.Containers = []v1.Container{
		{
			Name: "container",
			Ports: []v1.ContainerPort{
				{
					Name:          "metrics",
					ContainerPort: 8080,
				},
			},
		},
	}

	targets := makeTargets(pod)
	require.Equal(t, 0, len(targets)) // Should not find any targets
}

func TestAddTargetFromMap_NamedPort(t *testing.T) {
	targetMap := map[string]string{
		"metrics": "/metrics",
		"8080":    "/health",
	}

	cp := &v1.ContainerPort{
		Name:          "metrics",
		ContainerPort: 9090,
	}

	// Test named port resolution
	target, added := addTargetFromMap("1.2.3.4", "http", "metrics", "namespace", "pod", "container", targetMap, cp)
	require.True(t, added)
	require.Equal(t, "http://1.2.3.4:9090/metrics", target.Addr) // Should resolve to numeric port
	require.Equal(t, "namespace", target.Namespace)
	require.Equal(t, "pod", target.Pod)
	require.Equal(t, "container", target.Container)

	// targetMap should have the named port removed
	_, exists := targetMap["metrics"]
	require.False(t, exists)

	// Test numeric port (unchanged behavior)
	target2, added2 := addTargetFromMap("1.2.3.4", "http", "8080", "namespace", "pod", "container", targetMap, cp)
	require.True(t, added2)
	require.Equal(t, "http://1.2.3.4:8080/health", target2.Addr) // Should use numeric port as-is
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
