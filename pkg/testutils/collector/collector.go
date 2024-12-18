//go:build !disableDocker

package collector

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
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	DefaultImage = "collector"
	DefaultTag   = "latest"
)

type CollectorContainer struct {
	testcontainers.Container
}

func Run(ctx context.Context, opts ...testcontainers.ContainerCustomizer) (*CollectorContainer, error) {
	var relative string
	for iter := range 4 {
		relative = strings.Repeat("../", iter)
		if _, err := os.Stat(filepath.Join(relative, "build/images/Dockerfile.ingestor")); err == nil {
			break
		}
	}

	req := testcontainers.ContainerRequest{
		Name: "collector" + testcontainers.SessionID(),
		FromDockerfile: testcontainers.FromDockerfile{
			Repo:       DefaultImage,
			Tag:        DefaultTag,
			Context:    relative, // repo base
			Dockerfile: "build/images/Dockerfile.collector",
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

	lfp := filepath.Join(rootDir, "pkg/testutils/collector/k8s.yaml")
	rfp := filepath.Join(testutils.K3sManifests, "collector.yaml")
	if err := k.CopyFileToContainer(ctx, lfp, rfp, 0644); err != nil {
		return fmt.Errorf("failed to copy file to container: %w", err)
	}

	return nil
}

func DaemonSet() *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "DaemonSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "collector",
			Namespace: "adx-mon",
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"adxmon": "collector",
				},
			},
			UpdateStrategy: appsv1.DaemonSetUpdateStrategy{
				Type: appsv1.RollingUpdateDaemonSetStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDaemonSet{
					MaxSurge:       &intstr.IntOrString{Type: intstr.Int, IntVal: 0},
					MaxUnavailable: &intstr.IntOrString{Type: intstr.String, StrVal: "30%"},
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"adxmon": "collector",
					},
					Annotations: map[string]string{
						"adx-mon/scrape":          "true",
						"adx-mon/port":            "9091",
						"adx-mon/path":            "/metrics",
						"adx-mon/log-destination": "Logs:Collector",
						"adx-mon/log-parsers":     "json",
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: "collector",
					Containers: []corev1.Container{
						{
							Name:            "collector",
							Image:           "collector:latest",
							ImagePullPolicy: corev1.PullNever,
							Command:         []string{"/collector"},
							Args: []string{
								"--config=/etc/config/config.toml",
								"--hostname=$(HOSTNAME)",
							},
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 8080,
									Protocol:      corev1.ProtocolTCP,
									HostPort:      3100,
								},
							},
							Env: []corev1.EnvVar{
								{
									Name:  "LOG_LEVEL",
									Value: "INFO",
								},
								{
									Name: "HOSTNAME",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "spec.nodeName",
										},
									},
								},
								{
									Name:  "GODEBUG",
									Value: "http2client=0",
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									MountPath: "/etc/ssl/certs",
									Name:      "ssl-certs",
									ReadOnly:  true,
								},
								{
									Name:      "config-volume",
									MountPath: "/etc/config",
								},
								{
									Name:      "storage",
									MountPath: "/mnt/data",
								},
								{
									Name:      "varlog",
									MountPath: "/var/log",
									ReadOnly:  true,
								},
								{
									Name:      "varlibdockercontainers",
									MountPath: "/var/lib/docker/containers",
									ReadOnly:  true,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "ssl-certs",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/etc/ssl/certs",
									Type: new(corev1.HostPathType),
								},
							},
						},
						{
							Name: "config-volume",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "collector-config",
									},
								},
							},
						},
						{
							Name: "storage",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/mnt/collector",
								},
							},
						},
						{
							Name: "varlog",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/var/log",
								},
							},
						},
						{
							Name: "varlibdockercontainers",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/var/lib/docker/containers",
								},
							},
						},
					},
				},
			},
		},
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
