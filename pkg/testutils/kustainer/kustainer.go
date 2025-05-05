//go:build !disableDocker

package kustainer

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/kql"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	"github.com/testcontainers/testcontainers-go/wait"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kwait "k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

type KustainerContainer struct {
	testcontainers.Container

	endpoint    string
	stop        chan struct{}
	mu          sync.Mutex // protects endpoint and stop
	monitorOnce sync.Once  // ensures monitor is started only once
}

func Run(ctx context.Context, img string, opts ...testcontainers.ContainerCustomizer) (*KustainerContainer, error) {
	req := testcontainers.ContainerRequest{
		Image:        img,
		ExposedPorts: []string{"8080/tcp"},
		Env: map[string]string{
			"ACCEPT_EULA": "Y",
		},
		WaitingFor: wait.ForAll(
			wait.ForListeningPort("8080/tcp"),
			wait.ForLog(".*Hit 'CTRL-C' or 'CTRL-BREAK' to quit.*").AsRegexp(),
		),
	}
	genericContainerReq := testcontainers.GenericContainerRequest{
		ContainerRequest: req,
	}

	for _, opt := range opts {
		if err := opt.Customize(&genericContainerReq); err != nil {
			return nil, err
		}
	}

	container, err := testcontainers.GenericContainer(ctx, genericContainerReq)
	var c *KustainerContainer
	if container != nil {
		c = &KustainerContainer{Container: container}
	}

	if err != nil {
		return c, fmt.Errorf("generic container: %w", err)
	}

	return c, nil
}

func (c *KustainerContainer) PortForward(ctx context.Context, config *rest.Config) error {
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	var podName string
	// Wait for pod to exist and be running/ready
	err = kwait.PollUntilContextTimeout(ctx, 1*time.Second, 10*time.Minute, true, func(ctx context.Context) (bool, error) {
		pods, err := clientset.CoreV1().Pods("default").List(ctx, metav1.ListOptions{
			LabelSelector: "app=kustainer",
		})
		if err != nil || len(pods.Items) == 0 {
			return false, nil
		}
		pod := pods.Items[0]
		if pod.Status.Phase != corev1.PodRunning {
			return false, nil
		}
		for _, cs := range pod.Status.ContainerStatuses {
			if !cs.Ready {
				return false, nil
			}
		}
		podName = pod.Name
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("failed to get ready pod: %w", err)
	}

	err = kwait.PollUntilContextTimeout(ctx, 1*time.Second, 10*time.Minute, true, func(ctx context.Context) (bool, error) {
		if err := c.waitForLog(ctx, clientset, podName, "Hit 'CTRL-C' or 'CTRL-BREAK' to quit"); err != nil {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("failed create container: %w", err)
	}

	// Retry port-forward on failure, with backoff
	var lastErr error
	for i := range 5 {
		err = c.connect(ctx, config, podName)
		if err == nil {
			// Start connection monitor only once
			c.monitorOnce.Do(func() {
				go c.monitorConnection(ctx, config, podName)
			})
			return nil
		}
		lastErr = err
		// Exponential backoff
		time.Sleep(time.Second * time.Duration(2<<i))
	}
	return fmt.Errorf("failed to connect to kustainer after retries: %w", lastErr)
}

// monitorConnection periodically checks the endpoint and reconnects if needed.
func (c *KustainerContainer) monitorConnection(ctx context.Context, config *rest.Config, podName string) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		c.mu.Lock()
		endpoint := c.endpoint
		stop := c.stop
		c.mu.Unlock()
		if endpoint == "" {
			time.Sleep(2 * time.Second)
			continue
		}
		// Use net.Dial to check if the port is open
		u, err := url.Parse(endpoint)
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		host := u.Host
		if !strings.Contains(host, ":") {
			host += ":8080"
		}
		conn, err := net.DialTimeout("tcp", host, 2*time.Second)
		if err != nil {
			// Connection is unhealthy, attempt reconnect
			if stop != nil {
				close(stop)
			}
			// Try to reconnect
			for i := 0; i < 5; i++ {
				if ctx.Err() != nil {
					return
				}
				err := c.connect(ctx, config, podName)
				if err == nil {
					break
				}
				time.Sleep(time.Second * time.Duration(2<<i))
			}
		} else {
			conn.Close()
		}
		time.Sleep(3 * time.Second)
	}
}

func (c *KustainerContainer) Close() error {
	if c.stop != nil {
		close(c.stop)
	}
	return nil
}

func WithCluster(ctx context.Context, k *k3s.K3sContainer) testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) error {
		req.LifecycleHooks = append(req.LifecycleHooks, testcontainers.ContainerLifecycleHooks{
			PreCreates: []testcontainers.ContainerRequestHook{
				func(ctx context.Context, req testcontainers.ContainerRequest) error {

					rootDir, err := testutils.GetGitRootDir()
					if err != nil {
						return fmt.Errorf("failed to get git root dir: %w", err)
					}

					lfp := filepath.Join(rootDir, "pkg/testutils/kustainer/k8s.yaml")
					rfp := filepath.Join(testutils.K3sManifests, "kustainer.yaml")
					if err := k.CopyFileToContainer(ctx, lfp, rfp, 0644); err != nil {
						return fmt.Errorf("failed to copy file to container: %w", err)
					}

					return nil
				},
			},
		})

		return nil
	}
}

// WithStarted will start the container when it is created.
// You don't want to do this if you want to load the container into a k8s cluster.
func WithStarted() testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) error {
		req.Started = true
		return nil
	}
}

func (c *KustainerContainer) waitForLog(ctx context.Context, client *kubernetes.Clientset, podName, logMsg string) error {
	req := client.CoreV1().Pods("default").GetLogs(podName, &corev1.PodLogOptions{
		Follow: true,
	})
	stream, err := req.Stream(ctx)
	if err != nil {
		return fmt.Errorf("failed to stream logs: %w", err)
	}
	defer stream.Close()

	scanner := bufio.NewScanner(stream)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, logMsg) {
			return nil
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading log stream: %w", err)
	}

	return nil
}

func (c *KustainerContainer) connect(ctx context.Context, config *rest.Config, podName string) error {
	// Create port forward
	transport, upgrader, err := spdy.RoundTripperFor(config)
	if err != nil {
		return fmt.Errorf("failed to create round tripper: %w", err)
	}

	path := fmt.Sprintf("/api/v1/namespaces/default/pods/%s/portforward", podName)
	hostIP := strings.TrimLeft(config.Host, "htps:/")

	serverURL := url.URL{Scheme: "https", Path: path, Host: hostIP}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, &serverURL)

	ports := []string{"0:8080"}
	stopChan, readyChan := make(chan struct{}, 1), make(chan struct{})
	out, errOut := threadsafeBuffer{}, threadsafeBuffer{}

	pf, err := portforward.New(dialer, ports, stopChan, readyChan, &out, &errOut)
	if err != nil {
		return fmt.Errorf("failed to create port forward: %w", err)
	}

	go func() {
		if err := pf.ForwardPorts(); err != nil {
			fmt.Fprintf(os.Stderr, "port forward error: %v\n", err)
		}
	}()

	select {
	case <-readyChan:
		// Port forward is ready, get the local port
		localPorts, err := pf.GetPorts()
		if err != nil {
			return fmt.Errorf("failed to get local port: %w", err)
		}
		endpoint := fmt.Sprintf("http://localhost:%d", localPorts[0].Local)
		c.mu.Lock()
		c.stop = stopChan
		c.endpoint = endpoint
		c.mu.Unlock()
		return nil
	case <-time.After(10 * time.Second):
		close(stopChan)
		return fmt.Errorf("timeout waiting for port forward")
	case <-ctx.Done():
		close(stopChan)
		return fmt.Errorf("timeout waiting for port forward")
	}
}

func (c *KustainerContainer) ConnectionUrl() string {
	if c.endpoint == "" {
		// This means we're running out-of-cluster
		port, err := c.MappedPort(context.Background(), "8080")
		if err != nil {
			return ""
		}
		return "http://localhost:" + port.Port()
	}

	return c.endpoint
}

func (c *KustainerContainer) CreateDatabase(ctx context.Context, dbName string) error {
	cb := kusto.NewConnectionStringBuilder(c.endpoint)
	client, err := kusto.New(cb)
	if err != nil {
		return fmt.Errorf("new kusto client: %w", err)
	}
	defer client.Close()

	stmt := kql.New(".create database ").AddUnsafe(dbName).AddLiteral(" volatile")
	_, err = client.Mgmt(ctx, "", stmt)
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return fmt.Errorf("create database %s: %w", dbName, err)
	}
	return nil
}

type IngestionBatchingPolicy struct {
	MaximumBatchingTimeSpan time.Duration `json:"MaximumBatchingTimeSpan,omitempty"`
	MaximumNumberOfItems    int           `json:"MaximumNumberOfItems,omitempty"`
	MaximumRawDataSizeMB    int           `json:"MaximumRawDataSizeMB,omitempty"`
}

// MarshalJSON customizes the JSON representation of IngestionBatchingPolicy
func (p IngestionBatchingPolicy) MarshalJSON() ([]byte, error) {
	type Alias IngestionBatchingPolicy
	return json.Marshal(&struct {
		MaximumBatchingTimeSpan string `json:"MaximumBatchingTimeSpan"`
		*Alias
	}{
		MaximumBatchingTimeSpan: fmt.Sprintf("%02d:%02d:%02d", int(p.MaximumBatchingTimeSpan.Hours()), int(p.MaximumBatchingTimeSpan.Minutes())%60, int(p.MaximumBatchingTimeSpan.Seconds())%60),
		Alias:                   (*Alias)(&p),
	})
}

// UnmarshalJSON customizes the JSON unmarshalling of IngestionBatchingPolicy
func (p *IngestionBatchingPolicy) UnmarshalJSON(data []byte) error {
	type Alias IngestionBatchingPolicy
	aux := &struct {
		MaximumBatchingTimeSpan string `json:"MaximumBatchingTimeSpan"`
		*Alias
	}{
		Alias: (*Alias)(p),
	}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	duration, err := time.ParseDuration(aux.MaximumBatchingTimeSpan)
	if err != nil {
		return err
	}
	p.MaximumBatchingTimeSpan = duration
	return nil
}

// Custom String method to format duration as "00:00:01"
func (p IngestionBatchingPolicy) String() string {
	return fmt.Sprintf("%02d:%02d:%02d", int(p.MaximumBatchingTimeSpan.Hours()), int(p.MaximumBatchingTimeSpan.Minutes())%60, int(p.MaximumBatchingTimeSpan.Seconds())%60)
}

func (c *KustainerContainer) SetIngestionBatchingPolicy(ctx context.Context, dbName string, p IngestionBatchingPolicy) error {
	cb := kusto.NewConnectionStringBuilder(c.endpoint)
	client, err := kusto.New(cb)
	if err != nil {
		return fmt.Errorf("new kusto client: %w", err)
	}
	defer client.Close()

	policy, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal policy: %w", err)
	}

	stmt := kql.New(".alter database ").AddUnsafe(dbName).AddLiteral(" policy ingestionbatching").
		AddLiteral("```").AddUnsafe(string(policy)).AddLiteral("```")
	_, err = client.Mgmt(ctx, "", stmt)
	if err != nil {
		return fmt.Errorf("create database %s: %w", dbName, err)
	}

	return nil
}

type threadsafeBuffer struct {
	sync.Mutex
	buffer bytes.Buffer
}

func (b *threadsafeBuffer) Write(p []byte) (n int, err error) {
	b.Lock()
	defer b.Unlock()
	return b.buffer.Write(p)
}

func (b *threadsafeBuffer) Read(p []byte) (n int, err error) {
	b.Lock()
	defer b.Unlock()
	return b.buffer.Read(p)
}
