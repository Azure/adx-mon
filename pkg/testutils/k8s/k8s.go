package k8s

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	v1 "github.com/Azure/adx-mon/api/v1"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Cluster struct {
	k3sContainer *k3s.K3sContainer
	restConfig   *rest.Config
	kubeClient   client.Client
	manifests    []string
	kubeconfig   string
	cancel       context.CancelFunc
	pctx         context.Context
}

func New(ctx context.Context, manifests []string) *Cluster {
	pctx, cancel := context.WithCancel(ctx)
	return &Cluster{
		manifests: manifests,
		cancel:    cancel,
		pctx:      pctx,
	}
}

func (c *Cluster) Open(ctx context.Context) error {
	// TODO: Perhaps make adding to the schema an option.
	scheme := clientgoscheme.Scheme
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return fmt.Errorf("failed to add client-go scheme: %w", err)
	}
	if err := v1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("failed to add v1 scheme: %w", err)
	}

	rootDir, err := getGitRootDir()
	if err != nil {
		return fmt.Errorf("failed to get git root dir: %w", err)
	}

	var manifests []testcontainers.ContainerCustomizer
	for _, manifest := range c.manifests {
		manifests = append(manifests, k3s.WithManifest(filepath.Join(rootDir, manifest)))
	}

	k3sContainer, err := k3s.Run(
		ctx,
		// "rancher/k3s:v1.27.1-k3s1",
		"rancher/k3s:v1.31.2-k3s1",
		manifests...,
	)
	if err != nil {
		return fmt.Errorf("failed to run k3s: %w", err)
	}
	c.k3sContainer = k3sContainer

	kubeConfigYaml, err := k3sContainer.GetKubeConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to get kubeconfig: %w", err)
	}

	dir, err := os.MkdirTemp("", "kubeconfig")
	if err != nil {
		return fmt.Errorf("failed to create kubeconfig dir: %w", err)
	}

	kubeconfigPath := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kubeconfigPath, kubeConfigYaml, 0644); err != nil {
		return fmt.Errorf("failed to write kubeconfig: %w", err)
	}
	c.kubeconfig = kubeconfigPath

	restcfg, err := clientcmd.RESTConfigFromKubeConfig(kubeConfigYaml)
	if err != nil {
		return fmt.Errorf("failed to get rest config: %w", err)
	}
	c.restConfig = restcfg

	kubeClient, err := client.New(restcfg, client.Options{})
	if err != nil {
		return fmt.Errorf("failed to create kube client: %w", err)
	}
	c.kubeClient = kubeClient

	return nil
}

func (c *Cluster) Close() error {
	c.cancel()
	return c.k3sContainer.Terminate(context.Background())
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

// InstallCRD installs a CRD from a manifest file.
// Example: c.InstallCRD(ctx, "kustomize/bases/functions_crd.yaml")
func (c *Cluster) InstallCRD(ctx context.Context, crdPath string) error {
	dynClient, err := dynamic.NewForConfig(c.restConfig)
	if err != nil {
		return fmt.Errorf("failed to create dynamic client: %w", err)
	}

	// Read the manifest file
	rootDir, err := getGitRootDir()
	if err != nil {
		return fmt.Errorf("failed to get git root dir: %w", err)
	}

	manifest, err := os.ReadFile(filepath.Join(rootDir, crdPath))
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

func (c *Cluster) GetKubeConfig() string {
	return c.kubeconfig
}

func (c *Cluster) GetRestConfig() *rest.Config {
	return c.restConfig
}

func (c *Cluster) LoadImages(ctx context.Context, images ...string) error {
	return c.k3sContainer.LoadImages(ctx, images...)
}
