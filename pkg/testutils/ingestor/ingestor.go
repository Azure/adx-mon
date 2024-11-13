package ingestor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
)

const (
	DefaultImage = "ingestor"
	DefaultTag   = "latest"
)

type IngestorContainer struct {
	testcontainers.Container
}

func Run(ctx context.Context, opts ...testcontainers.ContainerCustomizer) (*IngestorContainer, error) {
	rootDir, err := testutils.GetGitRootDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get git root dir: %w", err)
	}

	f, err := os.Create(filepath.Join(rootDir, "Dockerfile"))
	if err != nil {
		return nil, fmt.Errorf("failed to open Dockerfile: %w", err)
	}
	defer func() {
		os.Remove(f.Name())
	}()

	if _, err := f.WriteString(dockerfile); err != nil {
		return nil, fmt.Errorf("failed to write Dockerfile: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("failed to close Dockerfile: %w", err)
	}

	req := testcontainers.ContainerRequest{
		Name: "ingestor" + testcontainers.SessionID(),
		FromDockerfile: testcontainers.FromDockerfile{
			Repo:          DefaultImage,
			Tag:           DefaultTag,
			Context:       rootDir,
			PrintBuildLog: true,
		},
	}

	genericContainerReq := testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Reuse:            true,
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

// WithStarted will start the container when it is created.
// You don't want to do this if you want to load the container into a k8s cluster.
func WithStarted() testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) error {
		req.Started = true
		return nil
	}
}

func WithCluster(ctx context.Context, k *k3s.K3sContainer) testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) error {
		req.LifecycleHooks = append(req.LifecycleHooks, testcontainers.ContainerLifecycleHooks{
			PreCreates: []testcontainers.ContainerRequestHook{
				func(ctx context.Context, req testcontainers.ContainerRequest) error {

					if err := k.LoadImages(ctx, DefaultImage+":"+DefaultTag); err != nil {
						return fmt.Errorf("failed to load image: %w", err)
					}

					rootDir, err := testutils.GetGitRootDir()
					if err != nil {
						return fmt.Errorf("failed to get git root dir: %w", err)
					}

					lfp := filepath.Join(rootDir, "pkg/testutils/ingestor/k8s.yaml")
					rfp := filepath.Join(testutils.K3sManifests, "ingestor.yaml")
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
