package kustainer

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/kql"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// type KustainerContainer struct {
// 	testcontainers.Container
// }

// func Run(ctx context.Context, img string, opts ...testcontainers.ContainerCustomizer) (*KustainerContainer, error) {
// 	req := testcontainers.ContainerRequest{
// 		Image:        img,
// 		ExposedPorts: []string{"8080/tcp"},
// 		Env: map[string]string{
// 			"ACCEPT_EULA": "Y",
// 		},
// 		WaitingFor: wait.ForAll(
// 			wait.ForListeningPort("8080/tcp"),
// 			wait.ForLog(".*Hit 'CTRL-C' or 'CTRL-BREAK' to quit.*").AsRegexp(),
// 		),
// 	}
// 	genericContainerReq := testcontainers.GenericContainerRequest{
// 		ContainerRequest: req,
// 		Started:          true,
// 	}

// 	for _, opt := range opts {
// 		if err := opt.Customize(&genericContainerReq); err != nil {
// 			return nil, err
// 		}
// 	}

// 	container, err := testcontainers.GenericContainer(ctx, genericContainerReq)
// 	var c *KustainerContainer
// 	if container != nil {
// 		c = &KustainerContainer{Container: container}
// 	}

// 	if err != nil {
// 		return c, fmt.Errorf("generic container: %w", err)
// 	}

// 	return c, nil
// }

// func WithNetwork(networkName string) testcontainers.CustomizeRequestOption {
// 	return func(req *testcontainers.GenericContainerRequest) error {
// 		req.Networks = []string{networkName}
// 		return nil
// 	}
// }

// func (c *KustainerContainer) MustConnectionUrl(ctx context.Context) string {
// 	connectionString, err := c.ConnectionUrl(ctx)
// 	if err != nil {
// 		panic(err)
// 	}
// 	return connectionString
// }

// func (c *KustainerContainer) ConnectionUrl(ctx context.Context) (string, error) {
// 	containerPort, err := c.MappedPort(ctx, "8080/tcp")
// 	if err != nil {
// 		return "", err
// 	}

// 	host, err := c.Host(ctx)
// 	if err != nil {
// 		return "", err
// 	}

// 	return fmt.Sprintf("http://%s:%s", host, containerPort.Port()), nil
// }

// func (c *KustainerContainer) Service(ctx context.Context) (*corev1.Service, error) {
// 	containerPort, err := c.MappedPort(ctx, "8080/tcp")
// 	if err != nil {
// 		return nil, err
// 	}
// 	return &corev1.Service{
// 		ObjectMeta: metav1.ObjectMeta{
// 			Name: "kustainer",
// 		},
// 		Spec: corev1.ServiceSpec{
// 			Type: corev1.ServiceTypeNodePort,
// 			Selector: map[string]string{
// 				"app": "kustainer",
// 			},
// 			Ports: []corev1.ServicePort{
// 				{
// 					Name:       "kustainer",
// 					Protocol:   corev1.ProtocolTCP,
// 					NodePort:   int32(containerPort.Int()),
// 					TargetPort: intstr.FromInt(8080),
// 					Port:       8080,
// 				},
// 			},
// 		},
// 	}, nil
// }

// func (c *KustainerContainer) CreateDatabase(ctx context.Context, dbName string) error {
// 	connectionString, err := c.ConnectionUrl(ctx)
// 	if err != nil {
// 		return fmt.Errorf("failed to get connection string: %w", err)
// 	}

// 	cb := kusto.NewConnectionStringBuilder(connectionString)
// 	client, err := kusto.New(cb)
// 	if err != nil {
// 		return fmt.Errorf("new kusto client: %w", err)
// 	}
// 	defer client.Close()

// 	stmt := kql.New(".create database ").AddUnsafe(dbName).AddLiteral(" volatile")
// 	_, err = client.Mgmt(ctx, "", stmt)
// 	if err != nil && !strings.Contains(err.Error(), "already exists") {
// 		return fmt.Errorf("create database %s: %w", dbName, err)
// 	}

// 	return nil
// }

// type TableColum struct {
// 	Name string
// 	Type string
// }

// func (c *KustainerContainer) CreateTable(ctx context.Context, dbName, tblName string, columns []TableColum) error {
// 	connectionString, err := c.ConnectionUrl(ctx)
// 	if err != nil {
// 		return fmt.Errorf("failed to get connection string: %w", err)
// 	}

// 	cb := kusto.NewConnectionStringBuilder(connectionString)
// 	client, err := kusto.New(cb)
// 	if err != nil {
// 		return fmt.Errorf("new kusto client: %w", err)
// 	}
// 	defer client.Close()

// 	var schema []string
// 	for _, column := range columns {
// 		schema = append(schema, fmt.Sprintf("%s:%s", column.Name, column.Type))
// 	}

// 	stmt := kql.New(".create table ").AddUnsafe(tblName).AddLiteral(" (").AddUnsafe(strings.Join(schema, ", ")).AddLiteral(")")
// 	_, err = client.Mgmt(ctx, dbName, stmt)
// 	if err != nil && !strings.Contains(err.Error(), "already exists") {
// 		return fmt.Errorf("create table %s: %w", tblName, err)
// 	}

// 	return nil
// }

type Kusto struct {
	kubeconfig string
	endpoint   string
	stop       chan struct{}
}

func New(kubeconfig string) *Kusto {
	return &Kusto{kubeconfig: kubeconfig}
}

func (k *Kusto) Open(ctx context.Context) error {
	config, err := clientcmd.BuildConfigFromFlags("", k.kubeconfig)
	if err != nil {
		return fmt.Errorf("failed to build kubeconfig: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create clientset: %w", err)
	}

	// Get the service's pod
	pods, err := clientset.CoreV1().Pods("default").List(ctx, metav1.ListOptions{
		LabelSelector: "app=kustainer",
	})
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return fmt.Errorf("no pods found for service kustainer")
	}

	podName := pods.Items[0].Name

	err = wait.PollUntilContextTimeout(ctx, 1*time.Second, 10*time.Minute, true, func(ctx context.Context) (bool, error) {
		if err := k.WaitForLog(ctx, clientset, podName, "Hit 'CTRL-C' or 'CTRL-BREAK' to quit"); err != nil {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("failed create container: %w", err)
	}

	err = wait.PollUntilContextTimeout(ctx, 1*time.Second, 10*time.Minute, true, func(ctx context.Context) (bool, error) {
		if err := k.connect(ctx, config, podName); err != nil {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("failed to connect to kustainer: %w", err)
	}

	cb := kusto.NewConnectionStringBuilder(k.endpoint)
	client, err := kusto.New(cb)
	if err != nil {
		return fmt.Errorf("new kusto client: %w", err)
	}
	defer client.Close()

	for _, dbName := range []string{"Metrics", "Logs"} {
		stmt := kql.New(".create database ").AddUnsafe(dbName).AddLiteral(" volatile")
		_, err = client.Mgmt(ctx, "", stmt)
		if err != nil && !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("create database %s: %w", dbName, err)
		}
	}

	return nil
}

func (k *Kusto) Close() error {
	return nil
}

func (k *Kusto) Endpoint() string {
	return k.endpoint
}

func (k *Kusto) WaitForLog(ctx context.Context, client *kubernetes.Clientset, podName, logMsg string) error {
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

func (k *Kusto) connect(ctx context.Context, config *rest.Config, podName string) error {
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
		k.stop = stopChan
		k.endpoint = endpoint
		return nil
	case <-time.After(10 * time.Second):
		close(stopChan)
		return fmt.Errorf("timeout waiting for port forward")
	case <-ctx.Done():
		close(stopChan)
		return fmt.Errorf("timeout waiting for port forward")
	}
}
