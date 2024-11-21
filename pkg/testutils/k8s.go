package testutils

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	v1 "github.com/Azure/adx-mon/api/v1"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
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

func InstallFunctionsCRD(ctx context.Context, k *k3s.K3sContainer, dir string) error {
	crdPath := filepath.Join(dir, "crd.yaml")
	if err := CopyFile("../../kustomize/bases/functions_crd.yaml", crdPath); err != nil {
		return fmt.Errorf("failed to copy crd file: %w", err)
	}
	return k.CopyFileToContainer(ctx, crdPath, filepath.Join(K3sManifests, "crd.yaml"), 0644)
}

func GetKubeConfig(ctx context.Context, k *k3s.K3sContainer, dir string) (ctrlCli ctrlclient.Client, err error) {
	scheme := clientgoscheme.Scheme
	if err = clientgoscheme.AddToScheme(scheme); err != nil {
		err = fmt.Errorf("failed to add client-go scheme: %w", err)
		return
	}
	if err = v1.AddToScheme(scheme); err != nil {
		err = fmt.Errorf("failed to add adx-mon scheme: %w", err)
		return
	}

	kubeconfig, err := WriteKubeConfig(ctx, k, dir)
	if err != nil {
		err = fmt.Errorf("failed to write kubeconfig: %w", err)
		return
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		err = fmt.Errorf("failed to build config from flags: %w", err)
		return
	}
	ctrlCli, err = ctrlclient.New(config, ctrlclient.Options{
		WarningHandler: ctrlclient.WarningHandlerOptions{
			SuppressWarnings: true,
		},
	})
	if err != nil {
		err = fmt.Errorf("failed to create client: %w", err)
		return
	}

	return
}
