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

	"github.com/Azure/adx-mon/pkg/testutils/ingestor"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// RunKustainer starts Kustainer and creates databases Logs and Metrics.
// The returned Service is meant to be used in k3s such that containers
// installed in k3s can connect to Kustainer on the host.
// func RunKustainer(t *testing.T, ctx context.Context) (uri string) {
// 	t.Helper()

// 	kustainerContainer, err := kustainer.Run(ctx, "mcr.microsoft.com/azuredataexplorer/kustainer-linux:latest")
// 	testcontainers.CleanupContainer(t, kustainerContainer.Container)
// 	require.NoError(t, err)

// 	uri = kustainerContainer.MustConnectionUrl(ctx)
// 	t.Logf("Kustainer service=%s", uri)
// 	for _, dbName := range []string{"Logs", "Metrics"} {
// 		require.NoError(t, kustainerContainer.CreateDatabase(ctx, dbName))
// 	}

// 	return
// }

func RunCluster(t *testing.T, ctx context.Context) (kubeconfigPath string) {
	t.Helper()

	rootDir, err := getGitRootDir()
	require.NoError(t, err)

	// Start k3s
	k3sContainer, err := k3s.Run(
		ctx,
		"rancher/k3s:v1.27.1-k3s1",
		k3s.WithManifest(filepath.Join(rootDir, "build/k8s/ksm.yaml")),
		k3s.WithManifest(filepath.Join(rootDir, "build/k8s/ingestor.yaml")),
		k3s.WithManifest(filepath.Join(rootDir, "pkg/testutils/kustainer/k8s.yaml")),
	)
	testcontainers.CleanupContainer(t, k3sContainer.Container)
	require.NoError(t, err)

	// Store the kubeconfig for accessing the k3s cluster
	kubeConfigYaml, err := k3sContainer.GetKubeConfig(ctx)
	require.NoError(t, err)

	kubeconfigPath = filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(kubeconfigPath, kubeConfigYaml, 0644))
	t.Logf("Kubeconfig=%s", kubeconfigPath)

	restcfg, err := clientcmd.RESTConfigFromKubeConfig(kubeConfigYaml)
	require.NoError(t, err)

	k8s, err := kubernetes.NewForConfig(restcfg)
	require.NoError(t, err)

	// Build Ingestor from source
	ingestorContainer, err := ingestor.Run(ctx, "")
	testcontainers.CleanupContainer(t, ingestorContainer.Container)
	require.NoError(t, err)

	// Load Ingestor image into k3s
	ingestorImage := fmt.Sprintf("%s:%s", ingestor.DefaultImage, ingestor.DefaultTag)
	require.NoError(t, k3sContainer.LoadImages(ctx, ingestorImage))

	// Customize the Ingestor deployment to use the Kustainer service
	statefulSets, err := k8s.AppsV1().StatefulSets("adx-mon").List(ctx, metav1.ListOptions{})
	require.NoError(t, err)

	for _, statefulSet := range statefulSets.Items {
		if statefulSet.Name != "ingestor" {
			continue
		}

		require.NoError(t, k8s.AppsV1().StatefulSets(statefulSet.Namespace).Delete(ctx, statefulSet.Name, metav1.DeleteOptions{}))

		kustainerUri := "http://kustainer.default.svc.cluster.local:8080"
		statefulSet.Spec.Template.Spec.Containers[0].Env = append(statefulSet.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{
			Name:  "ADX_URL",
			Value: kustainerUri,
		})

		var args []string
		for _, arg := range statefulSet.Spec.Template.Spec.Containers[0].Args {
			args = append(args, strings.Replace(arg, "$ADX_URL", kustainerUri, 1))
		}
		args = append(args, "--kustainer")
		statefulSet.Spec.Template.Spec.Containers[0].Args = args

		statefulSet.Spec.Template.Spec.Containers[0].ImagePullPolicy = corev1.PullNever // we'll load it ourselves
		statefulSet.Spec.Template.Spec.Containers[0].Image = ingestorImage

		statefulSet.ResourceVersion = "" // clear resource version to force a create
		_, err = k8s.AppsV1().StatefulSets(statefulSet.Namespace).Create(ctx, &statefulSet, metav1.CreateOptions{})
		require.NoError(t, err)

		break
	}

	return
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
