package kustainer

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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

	endpoint string
	stop     chan struct{}
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
	err = kwait.PollUntilContextTimeout(ctx, 1*time.Second, 10*time.Minute, true, func(ctx context.Context) (bool, error) {
		pods, err := clientset.CoreV1().Pods("default").List(ctx, metav1.ListOptions{
			LabelSelector: "app=kustainer",
		})
		if err != nil {
			return false, nil
		}
		if len(pods.Items) == 0 {
			return false, nil
		}

		podName = pods.Items[0].Name
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("failed to get pod name: %w", err)
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

	err = kwait.PollUntilContextTimeout(ctx, 1*time.Second, 10*time.Minute, true, func(ctx context.Context) (bool, error) {
		if err := c.connect(ctx, config, podName); err != nil {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("failed to connect to kustainer: %w", err)
	}

	return nil
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
	out, errOut := new(bytes.Buffer), new(bytes.Buffer)

	pf, err := portforward.New(dialer, ports, stopChan, readyChan, out, errOut)
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
		c.stop = stopChan
		c.endpoint = endpoint
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
