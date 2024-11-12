package ingestor

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Azure/adx-mon/pkg/testutils/k8s"
	"github.com/Azure/adx-mon/pkg/testutils/log"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// TODO: Create a reusable container so we don't have to re-build
// each test run. https://golang.testcontainers.org/features/creating_container/#reusable-container
// Use the current git-sha as the tag for the image.

const (
	DefaultImage = "ghcr.io/azure/adx-mon/ingestor"
)

var DefaultTag = testcontainers.SessionID()

type IngestorContainer struct {
	testcontainers.Container
}

func Build(ctx context.Context) error {
	f, err := os.Create("../../../Dockerfile")
	if err != nil {
		return fmt.Errorf("failed to open Dockerfile: %w", err)
	}
	defer func() {
		f.Close()
		os.Remove(f.Name())
	}()
	if _, err := f.WriteString(dockerfile); err != nil {
		return fmt.Errorf("failed to write Dockerfile: %w", err)
	}
	req := testcontainers.ContainerRequest{
		FromDockerfile: testcontainers.FromDockerfile{
			Repo:          DefaultImage,
			Tag:           DefaultTag,
			Context:       "../../../.",
			PrintBuildLog: true,
			KeepImage:     true,
		},
	}

	genericContainerReq := testcontainers.GenericContainerRequest{
		ContainerRequest: req,
	}

	_, err = testcontainers.GenericContainer(ctx, genericContainerReq)
	return err
}

func Run(ctx context.Context, img string, opts ...testcontainers.ContainerCustomizer) (*IngestorContainer, error) {
	req := testcontainers.ContainerRequest{
		Image:        img,
		ExposedPorts: []string{"9090/tcp"},
		Env: map[string]string{
			"LOG_LEVEL": "DEBUG",
		},
		Cmd: []string{
			"--insecure-skip-verify",
			"--disable-peer-transfer",
			"--max-segment-size", "1024", // We want to quickly get logs into kustainer
			"--storage-dir", "/",
		},
		WaitingFor: wait.ForAll(
			wait.ForListeningPort("9090/tcp"),
			wait.ForLog(".*Listening at.*").AsRegexp(),
		),
	}

	genericContainerReq := testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	}

	for _, opt := range opts {
		if err := opt.Customize(&genericContainerReq); err != nil {
			return nil, err
		}
	}

	container, err := testcontainers.GenericContainer(ctx, genericContainerReq)
	var c *IngestorContainer
	if container != nil {
		c = &IngestorContainer{Container: container}
	}

	if err != nil {
		return c, fmt.Errorf("generic container: %w", err)
	}

	return c, nil
}

func WithKubeconfig(kubeconfigPath string) testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) error {
		manifest := filepath.Base(kubeconfigPath)
		target := "/var/lib/.kube/" + manifest

		req.Files = append(req.Files, testcontainers.ContainerFile{
			HostFilePath:      kubeconfigPath,
			ContainerFilePath: target,
		})
		req.Cmd = append(req.Cmd, "--kubeconfig", target)

		return nil
	}
}

func WithNamespace(namespaceName string) testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) error {
		req.Cmd = append(req.Cmd, "--namespace", namespaceName)
		return nil
	}
}

func WithKustoLogsEndpoint(endpoint, dbName string) testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) error {
		req.Cmd = append(req.Cmd, fmt.Sprintf("--logs-kusto-endpoints=%s=%s", dbName, endpoint))
		return nil
	}
}

func WithLogger(logger log.Logger) testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) error {
		req.LogConsumerCfg = &testcontainers.LogConsumerConfig{
			Consumers: []testcontainers.LogConsumer{logger},
		}
		return nil
	}
}

func RunWithKustoEndpoint(ctx context.Context, c *k8s.Cluster) error {
	// Build Ingestor from source
	err := Build(ctx)
	if err != nil {
		return fmt.Errorf("failed to build ingestor container: %w", err)
	}
	ingestorImage := fmt.Sprintf("%s:%s", DefaultImage, DefaultTag)
	if err := c.LoadImages(ctx, ingestorImage); err != nil {
		return fmt.Errorf("failed to load image: %w", err)
	}

	client, err := kubernetes.NewForConfig(c.GetRestConfig())
	if err != nil {
		return fmt.Errorf("failed to create k8s client: %w", err)
	}

	statefulSets, err := client.AppsV1().StatefulSets("adx-mon").List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list stateful sets: %w", err)
	}

	for _, statefulSet := range statefulSets.Items {
		if statefulSet.Name != "ingestor" {
			continue
		}

		if err := client.AppsV1().StatefulSets(statefulSet.Namespace).Delete(ctx, statefulSet.Name, metav1.DeleteOptions{}); err != nil {
			return fmt.Errorf("failed to delete stateful set: %w", err)
		}

		connectionURL := "http://kustainer.default.svc.cluster.local:8080" // Use the Service DNS name
		statefulSet.Spec.Template.Spec.Containers[0].Env = append(statefulSet.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{
			Name:  "ADX_URL",
			Value: connectionURL,
		})

		var args []string
		for _, arg := range statefulSet.Spec.Template.Spec.Containers[0].Args {
			args = append(args, strings.Replace(arg, "$ADX_URL", connectionURL, 1))
		}
		args = append(args, "--kustainer")
		statefulSet.Spec.Template.Spec.Containers[0].Args = args

		statefulSet.Spec.Template.Spec.Containers[0].ImagePullPolicy = corev1.PullNever // we'll load it ourselves
		statefulSet.Spec.Template.Spec.Containers[0].Image = ingestorImage

		statefulSet.ResourceVersion = "" // clear resource version to force a create
		_, err = client.AppsV1().StatefulSets(statefulSet.Namespace).Create(ctx, &statefulSet, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create stateful set: %w", err)
		}

		// Wait for our startup log
		req := client.CoreV1().Pods("adx-mon").GetLogs("ingestor-0", &corev1.PodLogOptions{
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
			if strings.Contains(line, "Listening at") {
				return nil
			}
		}

		if err := scanner.Err(); err != nil {
			return fmt.Errorf("error reading log stream: %w", err)
		}

		break
	}

	return nil
}

// TODO
// dockerfile is a copy of build/images/Dockerfile.ingestor - I can't seem to get the context
// to allow adding from the parent directory.
// I tried the following combinations:
// Context:       filepath.Join(rootDir, "/build/images"),
// Dockerfile:    "Dockerfile.ingestor",
//
// Context:       filepath.Join(rootDir, "/build/images"),
// Dockerfile:    "../Dockerfile.ingestor",
//
// Context:       filepath.Join(rootDir, "/build/images/.."),
// Dockerfile:    "Dockerfile.ingestor",
//
// Context:       rootDir,
// Dockerfile:    "build/images/Dockerfile.ingestor",
//
// Context:       "../../../.",
// Dockerfile:    "build/images/Dockerfile.ingestor",
//
// Context:       "../../../.",
// Dockerfile:    "./build/images/Dockerfile.ingestor",
var dockerfile = `
FROM golang:1.22 as builder

ADD ./ /code
WORKDIR /code

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ./bin/ingestor ./cmd/ingestor

FROM mcr.microsoft.com/cbl-mariner/distroless/minimal:2.0

LABEL org.opencontainers.image.source https://github.com/Azure/adx-mon

COPY --from=builder /code/bin /

ENTRYPOINT ["/ingestor"]
`
