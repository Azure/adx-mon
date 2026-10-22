//go:build !disableDocker

package adxexporter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
)

const (
	DefaultImage = "adxexporter"
	DefaultTag   = "latest"
)

type ADXExporterContainer struct {
	testcontainers.Container
}

func Run(ctx context.Context, opts ...testcontainers.ContainerCustomizer) (*ADXExporterContainer, error) {
	var relative string
	for iter := range 4 {
		relative = strings.Repeat("../", iter)
		if _, err := os.Stat(filepath.Join(relative, "build/images/Dockerfile.adxexporter")); err == nil {
			break
		}
	}

	req := testcontainers.ContainerRequest{
		FromDockerfile: testcontainers.FromDockerfile{
			Repo:       DefaultImage,
			Tag:        DefaultTag,
			Context:    relative,
			Dockerfile: "build/images/Dockerfile.adxexporter",
			KeepImage:  true,
		},
	}

	genericContainerReq := testcontainers.GenericContainerRequest{ContainerRequest: req}
	for _, opt := range opts {
		if err := opt.Customize(&genericContainerReq); err != nil {
			return nil, err
		}
	}

	container, err := testcontainers.GenericContainer(ctx, genericContainerReq)
	var exporter *ADXExporterContainer
	if container != nil {
		exporter = &ADXExporterContainer{Container: container}
	}
	if err != nil {
		return exporter, fmt.Errorf("generic container: %w", err)
	}

	return exporter, nil
}

func WithCluster(ctx context.Context, cluster *k3s.K3sContainer) testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) error {
		req.LifecycleHooks = append(req.LifecycleHooks, testcontainers.ContainerLifecycleHooks{
			PreCreates: []testcontainers.ContainerRequestHook{
				func(ctx context.Context, req testcontainers.ContainerRequest) error {
					if err := cluster.LoadImages(ctx, DefaultImage+":"+DefaultTag); err != nil {
						return fmt.Errorf("failed to load image: %w", err)
					}

					rootDir, err := testutils.GetGitRootDir()
					if err != nil {
						return fmt.Errorf("failed to get git root dir: %w", err)
					}

					localPath := filepath.Join(rootDir, "pkg/testutils/adxexporter/k8s.yaml")
					remotePath := filepath.Join(testutils.K3sManifests, "adxexporter.yaml")
					if err := cluster.CopyFileToContainer(ctx, localPath, remotePath, 0644); err != nil {
						return fmt.Errorf("failed to copy manifest: %w", err)
					}

					return nil
				},
			},
		})

		return nil
	}
}
