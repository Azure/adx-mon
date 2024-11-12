package collector

import (
	"context"
	"fmt"
	"os"
	"slices"

	"github.com/Azure/adx-mon/pkg/testutils/k8s"
	"github.com/testcontainers/testcontainers-go"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	DefaultImage = "ghcr.io/azure/adx-mon/collector"
)

var DefaultTag = testcontainers.SessionID()

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

func Run(ctx context.Context, c *k8s.Cluster) error {
	// Build Ingestor from source
	err := Build(ctx)
	if err != nil {
		return fmt.Errorf("failed to build collector container: %w", err)
	}
	collectorImage := fmt.Sprintf("%s:%s", DefaultImage, DefaultTag)
	if err := c.LoadImages(ctx, collectorImage); err != nil {
		return fmt.Errorf("failed to load image: %w", err)
	}

	client, err := kubernetes.NewForConfig(c.GetRestConfig())
	if err != nil {
		return fmt.Errorf("failed to create k8s client: %w", err)
	}

	daemonSets, err := client.AppsV1().DaemonSets("adx-mon").List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list daemonsets: %w", err)
	}

	for _, daemonSet := range daemonSets.Items {
		if daemonSet.Name != "collector" {
			continue
		}

		if err := client.AppsV1().DaemonSets(daemonSet.Namespace).Delete(ctx, daemonSet.Name, metav1.DeleteOptions{}); err != nil {
			return fmt.Errorf("failed to delete daemonset: %w", err)
		}

		var (
			volumes  []corev1.Volume
			filtered = []string{"etcmachineid", "etc-pki-ca-certs", "storage"}
		)
		for _, volume := range daemonSet.Spec.Template.Spec.Volumes {
			if slices.Contains(filtered, volume.Name) {
				continue
			}
			volumes = append(volumes, volume)
		}
		daemonSet.Spec.Template.Spec.Volumes = volumes

		for idx, container := range daemonSet.Spec.Template.Spec.Containers {
			if container.Name != "collector" {
				continue
			}

			var volumeMounts []corev1.VolumeMount
			for _, volumeMount := range container.VolumeMounts {
				if slices.Contains(filtered, volumeMount.Name) {
					continue
				}
				volumeMounts = append(volumeMounts, volumeMount)
			}
			daemonSet.Spec.Template.Spec.Containers[idx].VolumeMounts = volumeMounts

			daemonSet.Spec.Template.Spec.Containers[idx].ImagePullPolicy = corev1.PullNever // we'll load it ourselves
			daemonSet.Spec.Template.Spec.Containers[idx].Image = collectorImage
		}

		daemonSet.ResourceVersion = "" // clear resource version to force a create
		_, err = client.AppsV1().DaemonSets(daemonSet.Namespace).Create(ctx, &daemonSet, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create daemonset: %w", err)
		}

		break
	}

	// We're highly capacity contrained in kustainer so we're going to limit the amount of telemetry
	if err := client.AppsV1().Deployments("adx-mon").Delete(ctx, "collector-singleton", metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("failed to delete deployment: %w", err)
	}

	return nil
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
