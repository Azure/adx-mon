package testutils

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	v1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/ingestor/storage"
	"github.com/Azure/adx-mon/pkg/crd"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const K3sManifests = "/var/lib/rancher/k3s/server/manifests/"

func K8sRestConfig(ctx context.Context, k *k3s.K3sContainer) (*rest.Config, error) {
	scheme := clientgoscheme.Scheme
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add client-go scheme: %w", err)
	}
	if err := v1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add v1 scheme: %w", err)
	}

	kubeConfigYaml, err := k.GetKubeConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get kubeconfig: %w", err)
	}

	restcfg, err := clientcmd.RESTConfigFromKubeConfig(kubeConfigYaml)
	if err != nil {
		return nil, fmt.Errorf("failed to get rest config: %w", err)
	}

	return restcfg, nil
}

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

func VerifyCRDFunctionInstalled(ctx context.Context, t *testing.T, kubeConfigYaml, fnName string) {
	t.Helper()

	// The Function "Sample" is invalid: metadata.name: Invalid value: "Sample": a lowercase RFC 1123 subdomain
	fnName = strings.ToLower(fnName)

	scheme := clientgoscheme.Scheme
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, v1.AddToScheme(scheme))

	config, err := clientcmd.BuildConfigFromFlags("", kubeConfigYaml)
	require.NoError(t, err)

	client, err := ctrlclient.New(config, ctrlclient.Options{
		WarningHandler: ctrlclient.WarningHandlerOptions{
			SuppressWarnings: true,
		},
	})
	require.NoError(t, err)

	functionStorage := &storage.Functions{}
	opts := crd.Options{
		CtrlCli: client,
		List:    &v1.FunctionList{},
		Store:   functionStorage,
	}
	operator := crd.New(opts)
	require.NoError(t, operator.Open(context.Background()))
	defer operator.Close()

	require.Eventually(t, func() bool {

		functions := functionStorage.List()
		for _, function := range functions {
			if function.GetName() == fnName {
				return function.Status.Status == v1.Success
			}
		}

		return false
	}, 10*time.Minute, time.Second)
}
