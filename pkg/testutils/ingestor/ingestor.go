package ingestor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Azure/adx-mon/pkg/testutils/log"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
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
	if img == "" {
		f, err := os.Create("../../Dockerfile")
		if err != nil {
			return nil, fmt.Errorf("failed to open Dockerfile: %w", err)
		}
		defer func() {
			f.Close()
			os.Remove(f.Name())
		}()
		if _, err := f.WriteString(dockerfile); err != nil {
			return nil, fmt.Errorf("failed to write Dockerfile: %w", err)
		}

		req.FromDockerfile = testcontainers.FromDockerfile{
			Repo:          DefaultImage,
			Tag:           DefaultTag,
			Context:       "../..",
			PrintBuildLog: true,
			KeepImage:     true,
		}
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
