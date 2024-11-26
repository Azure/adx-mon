package ingestor

import (
	"context"
	"fmt"
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
	req := testcontainers.ContainerRequest{
		Name: "ingestor" + testcontainers.SessionID(),
		FromDockerfile: testcontainers.FromDockerfile{
			Repo:          DefaultImage,
			Tag:           DefaultTag,
			Context:       "../../..", // repo base
			Dockerfile:    "build/images/Dockerfile.ingestor",
			PrintBuildLog: true,
			KeepImage:     true,
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
