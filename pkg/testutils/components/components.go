package components

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Azure/adx-mon/pkg/testutils/ingestor"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type Options struct {
	K3sImage string
}

type Components struct {
	k3sContainers *k3s.K3sContainer
}

func Run(ctx context.Context, opts Options) (*Components, error) {
	k3sContainer, err := startK3s(ctx, opts.K3sImage)
	if err != nil {
		return nil, fmt.Errorf("failed to run k3s container: %w", err)
	}

	c := &Components{
		k3sContainers: k3sContainer,
	}

	if err := c.replaceIngestor(ctx); err != nil {
		return nil, fmt.Errorf("failed to replace ingestor: %w", err)
	}

	return c, nil
}

func (c *Components) Close(ctx context.Context) error {
	return c.k3sContainers.Terminate(ctx)
}

func (c *Components) GetKubeConfig(ctx context.Context) ([]byte, error) {
	return c.k3sContainers.GetKubeConfig(ctx)
}

func (c *Components) replaceIngestor(ctx context.Context) error {
	// Build Ingestor from source
	ingestorContainer, err := ingestor.Run(ctx, "")
	if err != nil {
		return fmt.Errorf("failed to run ingestor container: %w", err)
	}
	defer ingestorContainer.Terminate(ctx)

	ingestorImage := fmt.Sprintf("%s:%s", ingestor.DefaultImage, ingestor.DefaultTag)
	if err := c.k3sContainers.LoadImages(ctx, ingestorImage); err != nil {
		return fmt.Errorf("failed to load ingestor image: %w", err)
	}

	kubeConfigYaml, err := c.k3sContainers.GetKubeConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to get kubeconfig: %w", err)
	}

	restcfg, err := clientcmd.RESTConfigFromKubeConfig(kubeConfigYaml)
	if err != nil {
		return fmt.Errorf("failed to get rest config: %w", err)
	}

	k8s, err := kubernetes.NewForConfig(restcfg)
	if err != nil {
		return fmt.Errorf("failed to create k8s client: %w", err)
	}

	statefulSets, err := k8s.AppsV1().StatefulSets("adx-mon").List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list stateful sets: %w", err)
	}

	for _, statefulSet := range statefulSets.Items {
		if statefulSet.Name != "ingestor" {
			continue
		}

		if err := k8s.AppsV1().StatefulSets(statefulSet.Namespace).Delete(ctx, statefulSet.Name, metav1.DeleteOptions{}); err != nil {
			return fmt.Errorf("failed to delete stateful set: %w", err)
		}

		connectionURL := "http://kustainer.default.svc.cluster.local:8080" // Use the Service DNS name
		statefulSet.Spec.Template.Spec.Containers[0].Env = append(statefulSet.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{
			Name:  "ADX_URL",
			Value: connectionURL,
		})

		var args []string
		for _, arg := range statefulSet.Spec.Template.Spec.Containers[0].Args {
			args = append(args, strings.Replace(arg, "$ADX_URL", connectionURL, 1))
		}
		args = append(args, "--kustainer")
		statefulSet.Spec.Template.Spec.Containers[0].Args = args

		statefulSet.Spec.Template.Spec.Containers[0].ImagePullPolicy = corev1.PullNever // we'll load it ourselves
		statefulSet.Spec.Template.Spec.Containers[0].Image = ingestorImage

		statefulSet.ResourceVersion = "" // clear resource version to force a create
		_, err = k8s.AppsV1().StatefulSets(statefulSet.Namespace).Create(ctx, &statefulSet, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create stateful set: %w", err)
		}

		break
	}

	return nil
}

func startK3s(ctx context.Context, image string) (*k3s.K3sContainer, error) {
	rootDir, err := getGitRootDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get git root directory: %w", err)
	}
	k3sContainer, err := k3s.Run(
		ctx,
		image,
		k3s.WithManifest(filepath.Join(rootDir, "build/k8s/ksm.yaml")),
		k3s.WithManifest(filepath.Join(rootDir, "build/k8s/ingestor.yaml")),
		// k3s.WithManifest(filepath.Join(rootDir, "pkg/testutils/kustainer/k8s.yaml")),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to run k3s container: %w", err)
	}

	return k3sContainer, nil
}

func getGitRootDir() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}
