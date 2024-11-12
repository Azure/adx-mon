package collector

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
	DefaultImage = "collector"
	DefaultTag   = "latest"
)

type CollectorContainer struct {
	testcontainers.Container
}

func Run(ctx context.Context, opts ...testcontainers.ContainerCustomizer) (*CollectorContainer, error) {
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
		Name: "collector" + testcontainers.SessionID(),
		FromDockerfile: testcontainers.FromDockerfile{
			Repo:          DefaultImage,
			Tag:           DefaultTag,
			Context:       rootDir,
			PrintBuildLog: true,
		},
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
	var c *CollectorContainer
	if container != nil {
		c = &CollectorContainer{Container: container}
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

					lfp := filepath.Join(rootDir, "pkg/testutils/collector/k8s.yaml")
					rfp := filepath.Join(testutils.K3sManifests, "collector.yaml")
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

var dockerfile = `
FROM golang:1.22 as builder

ADD . /code
WORKDIR /code

# Headers required for journal input build
RUN apt update && apt install -y libsystemd-dev

RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o ./bin/collector ./cmd/collector

# Install systemd lib to get libsystemd.so
FROM mcr.microsoft.com/cbl-mariner/base/core:2.0 as libsystemdsource

RUN tdnf install -y systemd

FROM mcr.microsoft.com/cbl-mariner/distroless/base:2.0

LABEL org.opencontainers.image.source https://github.com/Azure/adx-mon

# Binary looks under /usr/lib64 for libsystemd.so and other required so files
# Found with 'export LD_DEBUG=libs' and running the binary
COPY --from=libsystemdsource /usr/lib/libsystemd.so /usr/lib/liblzma.so.5 /usr/lib/libzstd.so.1 /usr/lib/liblz4.so.1 /usr/lib/libcap.so.2 /usr/lib/libgcrypt.so.20 /usr/lib/libgpg-error.so.0 /usr/lib/libgcc_s.so.1 /usr/lib64/
COPY --from=builder /code/bin/collector /collector

ENTRYPOINT ["/collector"]
`
