package testutils

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	v1 "github.com/Azure/adx-mon/api/v1"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
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

func GetKubeConfig(ctx context.Context, k *k3s.K3sContainer) (*rest.Config, ctrlclient.Client, error) {
	scheme := clientgoscheme.Scheme
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return nil, nil, fmt.Errorf("failed to add client-go scheme: %w", err)
	}
	if err := v1.AddToScheme(scheme); err != nil {
		return nil, nil, fmt.Errorf("failed to add v1 scheme: %w", err)
	}

	kubeConfigYaml, err := k.GetKubeConfig(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get kubeconfig: %w", err)
	}

	restCfg, err := clientcmd.RESTConfigFromKubeConfig(kubeConfigYaml)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get rest config: %w", err)
	}

	ctrlCli, err := ctrlclient.New(restCfg, ctrlclient.Options{
		WarningHandler: ctrlclient.WarningHandlerOptions{
			SuppressWarnings: true,
		},
	})

	return restCfg, ctrlCli, nil
}
