package alerter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	DefaultImage = "alerter"
	DefaultTag   = "latest"
)

type AlerterContainer struct {
	testcontainers.Container
}

func Run(ctx context.Context, opts ...testcontainers.ContainerCustomizer) (*AlerterContainer, error) {
	var relative string
	for iter := range 4 {
		relative = strings.Repeat("../", iter)
		if _, err := os.Stat(filepath.Join(relative, "build/images/Dockerfile.alerter")); err == nil {
			break
		}
	}

	req := testcontainers.ContainerRequest{
		Name: "alerter" + testcontainers.SessionID(),
		FromDockerfile: testcontainers.FromDockerfile{
			Repo:       DefaultImage,
			Tag:        DefaultTag,
			Context:    relative, // repo base
			Dockerfile: "build/images/Dockerfile.alerter",
			KeepImage:  true,
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
	var c *AlerterContainer
	if container != nil {
		c = &AlerterContainer{Container: container}
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
		req.WaitingFor = wait.ForAll(
			wait.ForListeningPort("8080/tcp"),
			wait.ForLog(".*Starting adx-mon alerter.*").AsRegexp(),
		)
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

					lfp := filepath.Join(rootDir, "pkg/testutils/alerter/k8s.yaml")
					rfp := filepath.Join(testutils.K3sManifests, "alerter.yaml")
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

type KustoTableSchema struct{}

func (k *KustoTableSchema) TableName() string {
	return "Collector"
}

func (k *KustoTableSchema) CslColumns() []string {
	return []string{
		"msg:string",
		"lvl:string",
		"ts:datetime",
		"namespace:string",
		"container:string",
		"pod:string",
		"host:string",
	}
}
