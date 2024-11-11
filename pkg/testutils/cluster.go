package testutils

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/testutils/ingestor"
	"github.com/Azure/adx-mon/pkg/testutils/kustainer"
	"github.com/Azure/adx-mon/pkg/testutils/log"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func RunCluster(t *testing.T, ctx context.Context) {
	t.Helper()

	// Start Kustainer
	kustainerContainer, err := kustainer.Run(ctx, "mcr.microsoft.com/azuredataexplorer/kustainer-linux:latest")
	testcontainers.CleanupContainer(t, kustainerContainer.Container)
	require.NoError(t, err)

	uri := kustainerContainer.MustConnectionUrl(ctx)
	t.Logf("Kustainer service=%s", uri)
	for _, dbName := range []string{"Logs", "Metrics"} {
		require.NoError(t, kustainerContainer.CreateDatabase(ctx, dbName))
	}

	rootDir, err := getGitRootDir()
	require.NoError(t, err)

	// Start k3s
	k3sContainer, err := k3s.Run(
		ctx,
		"rancher/k3s:v1.27.1-k3s1",
		k3s.WithManifest(filepath.Join(rootDir, "kustomize/bases/functions_crd.yaml")), // Loading CRDs doesn't seem to work
		k3s.WithManifest(filepath.Join(rootDir, "build/k8s/ksm.yaml")),
	)
	testcontainers.CleanupContainer(t, k3sContainer.Container)
	require.NoError(t, err)

	// Store the kubeconfig for accessing the k3s cluster
	kubeConfigYaml, err := k3sContainer.GetKubeConfig(ctx)
	require.NoError(t, err)

	kubeconfigPath := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(kubeconfigPath, kubeConfigYaml, 0644))
	t.Logf("Kubeconfig=%s", kubeconfigPath)

	restcfg, err := clientcmd.RESTConfigFromKubeConfig(kubeConfigYaml)
	require.NoError(t, err)

	require.NoError(t, applyManifestDynamic(restcfg, filepath.Join(rootDir, "kustomize/bases/functions_crd.yaml")))

	// Build Ingestor from source
	ingestorBuildContainer, err := ingestor.Run(
		ctx,
		"",
		ingestor.WithKubeconfig(kubeconfigPath),
		ingestor.WithNamespace("adx-mon"),
		ingestor.WithKustoLogsEndpoint(uri, "Logs"),
		ingestor.WithLogger(log.Logger{T: t}),
	)
	testcontainers.CleanupContainer(t, ingestorBuildContainer.Container)

	time.Sleep(30 * time.Minute)

	require.NoError(t, err)
	require.True(t, false)

	// Now run Ingestor
	// ingestorContainer, err := ingestor.Run(
	// 	ctx,
	// 	fmt.Sprintf("%s:%s", ingestor.DefaultImage, ingestor.DefaultTag),
	// 	ingestor.WithKubeconfig(kubeconfigPath),
	// 	ingestor.WithNamespace("adx-mon"),
	// 	ingestor.WithKustoLogsEndpoint(uri, "Logs"),
	// )
	// testcontainers.CleanupContainer(t, ingestorContainer.Container)
	// require.NoError(t, err)
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

func applyManifestDynamic(restcfg *rest.Config, manifestPath string) error {
	// Create a dynamic client
	dynClient, err := dynamic.NewForConfig(restcfg)
	if err != nil {
		return fmt.Errorf("failed to create dynamic client: %w", err)
	}

	// Read the manifest file
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("failed to read manifest file: %w", err)
	}

	// Convert YAML to unstructured object
	dec := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(manifest), 100)
	var unstructuredObj unstructured.Unstructured
	if err := dec.Decode(&unstructuredObj); err != nil {
		return fmt.Errorf("failed to decode manifest: %w", err)
	}

	// Apply the unstructured object
	gvr := schema.GroupVersionResource{
		Group:    unstructuredObj.GroupVersionKind().Group,
		Version:  unstructuredObj.GroupVersionKind().Version,
		Resource: "customresourcedefinitions",
	}
	_, err = dynClient.Resource(gvr).Create(context.Background(), &unstructuredObj, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create resource: %w", err)
	}

	return nil
}
