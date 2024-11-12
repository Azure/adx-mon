package sample

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/Azure/adx-mon/pkg/testutils/k8s"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	DefaultImage = "sample"
	DefaultTag   = "latest"
)

type SampleContainer struct {
	testcontainers.Container
}

func Run(ctx context.Context, opts ...testcontainers.ContainerCustomizer) (*SampleContainer, error) {
	rootDir, err := testutils.GetGitRootDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get git root dir: %s", err)
	}

	req := testcontainers.ContainerRequest{
		FromDockerfile: testcontainers.FromDockerfile{
			Repo:          DefaultImage,
			Tag:           DefaultTag,
			Context:       filepath.Join(rootDir, "pkg/testutils/sample"),
			Dockerfile:    "./container/Dockerfile",
			PrintBuildLog: true,
			KeepImage:     true,
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
	var c *SampleContainer
	if container != nil {
		c = &SampleContainer{Container: container}
	}

	if err != nil {
		return c, fmt.Errorf("generic container: %w", err)
	}

	return c, nil
}

func WithCluster(ctx context.Context, c *k8s.Cluster) testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) error {
		if err := c.LoadImages(ctx, DefaultImage+":"+DefaultTag); err != nil {
			return err
		}

		client, err := kubernetes.NewForConfig(c.GetRestConfig())
		if err != nil {
			return fmt.Errorf("failed to create k8s client: %w", err)
		}

		replicas := int32(1)
		deployment := appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name: "sample",
				Labels: map[string]string{
					"app": "sample",
				},
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: &replicas,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "sample",
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app": "sample",
						},
						Annotations: map[string]string{
							"adx-mon/scrape":          "true",
							"adx-mon/log-destination": "Logs:Sample",
							"adx-mon/log-parsers":     "json",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:            "sample",
								Image:           DefaultImage + ":" + DefaultTag,
								ImagePullPolicy: corev1.PullNever,
							},
						},
					},
				},
			},
		}
		_, err = client.AppsV1().Deployments("default").Create(ctx, &deployment, metav1.CreateOptions{})
		return err
	}
}

// WithStarted will start the container when it is created.
// You don't want to do this if you want to load the container into a k8s cluster.
func WithStarted() testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) error {
		req.Started = true
		req.WaitingFor = wait.ForLog(".*Begin.*").AsRegexp()
		return nil
	}
}
