package testutils

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	v1 "github.com/Azure/adx-mon/api/v1"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
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
	restCfg.WarningHandler = rest.NoWarnings{}

	ctrlCli, err := ctrlclient.New(restCfg, ctrlclient.Options{})

	return restCfg, ctrlCli, err
}

func InstallFunctionsCrd(ctx context.Context, k *k3s.K3sContainer) error {
	restCfg, _, err := GetKubeConfig(ctx, k)
	if err != nil {
		return fmt.Errorf("failed to get kubeconfig: %w", err)
	}
	c, err := client.New(restCfg, client.Options{})
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	basesPath, ok := RelativePath("kustomize/bases")
	if !ok {
		return fmt.Errorf("failed to find bases path")
	}

	files, err := filepath.Glob(basesPath + "/*.yaml")
	if err != nil {
		return fmt.Errorf("listing YAML files: %w", err)
	}
	for _, file := range files {
		crdRawBytes, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("reading CRD file: %w", err)
		}
		var obj unstructured.Unstructured
		if err := yaml.Unmarshal(crdRawBytes, &obj); err != nil {
			return err
		}

		gvk := schema.GroupVersionKind{
			Group:   "apiextensions.k8s.io",
			Version: "v1",
			Kind:    "CustomResourceDefinition",
		}
		obj.SetGroupVersionKind(gvk)

		if err := c.Get(context.Background(), client.ObjectKey{Name: obj.GetName()}, &obj); err != nil {
			if apierrors.IsNotFound(err) {
				if err := c.Create(context.Background(), &obj); err != nil {
					return fmt.Errorf("failed to create CRD: %w", err)
				}
				continue
			}
			return err
		}
	}
	return nil
}
