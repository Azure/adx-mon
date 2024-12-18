//go:build !disableDocker

package ingestor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	DefaultImage = "ingestor"
	DefaultTag   = "latest"
)

type IngestorContainer struct {
	testcontainers.Container
}

func Run(ctx context.Context, opts ...testcontainers.ContainerCustomizer) (*IngestorContainer, error) {
	var relative string
	for iter := range 4 {
		relative = strings.Repeat("../", iter)
		if _, err := os.Stat(filepath.Join(relative, "build/images/Dockerfile.ingestor")); err == nil {
			break
		}
	}

	req := testcontainers.ContainerRequest{
		Name: "ingestor" + testcontainers.SessionID(),
		FromDockerfile: testcontainers.FromDockerfile{
			Repo:       DefaultImage,
			Tag:        DefaultTag,
			Context:    relative, // repo base
			Dockerfile: "build/images/Dockerfile.ingestor",
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

					// Manifests are installed as static pod manifests, which is why we don't include
					// Ingestor's StatefulSet in the k8s.yaml manifest, as doing so prevents us from
					// mutating the image in the StatefulSet.
					if err := InstallManifests(ctx, k); err != nil {
						return fmt.Errorf("failed to install manifests: %w", err)
					}

					return nil
				},
			},
		})

		return nil
	}
}

func InstallManifests(ctx context.Context, k *k3s.K3sContainer) error {
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
}

func StatefulSet() *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ingestor",
			Namespace: "adx-mon",
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: "adx-mon",
			Replicas:    int32Ptr(1),
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "ingestor",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "ingestor",
					},
					Annotations: map[string]string{
						"adx-mon/scrape":          "true",
						"adx-mon/port":            "9091",
						"adx-mon/path":            "/metrics",
						"adx-mon/log-destination": "Logs:Ingestor",
						"adx-mon/log-parsers":     "json",
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: "ingestor",
					Containers: []corev1.Container{
						{
							Name:            "ingestor",
							Image:           "ingestor:latest",
							ImagePullPolicy: corev1.PullNever,
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 9090,
									Name:          "ingestor",
								},
								{
									ContainerPort: 9091,
									Name:          "metrics",
								},
							},
							Env: []corev1.EnvVar{
								{
									Name:  "LOG_LEVEL",
									Value: "INFO",
								},
								{
									Name:  "GODEBUG",
									Value: "http2client=0",
								},
								{
									Name:  "AZURE_RESOURCE",
									Value: "$ADX_URL",
								},
								{
									Name:  "AZURE_CLIENT_ID",
									Value: "$CLIENT_ID",
								},
							},
							Command: []string{"/ingestor"},
							Args: []string{
								"--storage-dir=/mnt/data",
								"--max-segment-age=5s",
								"--max-disk-usage=21474836480",
								"--max-transfer-size=10485760",
								"--max-connections=1000",
								"--insecure-skip-verify",
								"--lift-label=host",
								"--lift-label=cluster",
								"--lift-label=adxmon_namespace=Namespace",
								"--lift-label=adxmon_pod=Pod",
								"--lift-label=adxmon_container=Container",
								"--metrics-kusto-endpoints=Metrics=http://kustainer.default.svc.cluster.local:8080",
								"--logs-kusto-endpoints=Logs=http://kustainer.default.svc.cluster.local:8080",
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "metrics",
									MountPath: "/mnt/data",
								},
								{
									Name:      "etc-pki-ca-certs",
									MountPath: "/etc/pki/ca-trust/extracted",
									ReadOnly:  true,
								},
								{
									Name:      "ca-certs",
									MountPath: "/etc/ssl/certs",
									ReadOnly:  true,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "ca-certs",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/etc/ssl/certs",
									Type: newHostPathType(corev1.HostPathDirectory),
								},
							},
						},
						{
							Name: "etc-pki-ca-certs",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/etc/pki/ca-trust/extracted",
									Type: newHostPathType(corev1.HostPathDirectoryOrCreate),
								},
							},
						},
						{
							Name: "metrics",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/mnt/ingestor",
								},
							},
						},
					},
				},
			},
		},
	}
}

func int32Ptr(i int32) *int32 { return &i }

func newHostPathType(t corev1.HostPathType) *corev1.HostPathType { return &t }

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
