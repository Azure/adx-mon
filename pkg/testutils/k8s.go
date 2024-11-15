package testutils

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/testcontainers/testcontainers-go/modules/k3s"
)

const K3sManifests = "/var/lib/rancher/k3s/server/manifests/"

func WriteKubeConfig(ctx context.Context, k *k3s.K3sContainer, dir string) (string, error) {
	kubeConfigYaml, err := k.GetKubeConfig(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get kubeconfig: %w", err)
	}
	kubeConfigPath := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kubeConfigPath, kubeConfigYaml, 0644); err != nil {
		return "", fmt.Errorf("failed to write kubeconfig: %w", err)
	}
	return kubeConfigPath, nil
}
