package k8s

import (
	"context"

	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/klog/v2"

	"github.com/Azure/adx-mon/pkg/logger"
)

// BuildConfigFromFlags is a helper function that builds configs from a master
// url or a kubeconfig filepath. These are passed in as command line flags for cluster
// components. Warnings should reflect this usage. If neither masterUrl or kubeconfigPath
// are passed in we fallback to inClusterConfig. If inClusterConfig fails, we fallback
// to the default config.
//
// Copied from upstream and just removed the "using in-cluster config" warning.
// see: https://github.com/kubernetes/client-go/blob/master/tools/clientcmd/client_config.go
func BuildConfigFromFlags(masterUrl, kubeconfigPath string) (*restclient.Config, error) {
	if kubeconfigPath == "" && masterUrl == "" {
		kubeconfig, err := restclient.InClusterConfig()
		if err == nil {
			return kubeconfig, nil
		}
		klog.Warning("error creating inClusterConfig, falling back to default config: ", err)
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath},
		&clientcmd.ConfigOverrides{ClusterInfo: clientcmdapi.Cluster{Server: masterUrl}}).ClientConfig()
}

type WarningLogger struct{}

func (WarningLogger) HandleWarningHeader(code int, agent string, message string) {
	logWarning(code, agent, message)
}

func (WarningLogger) HandleWarningHeaderWithContext(_ context.Context, code int, agent string, message string) {
	logWarning(code, agent, message)
}

func logWarning(code int, agent string, message string) {
	if code != 299 || message == "" {
		return
	}

	if agent != "" {
		logger.Warnf("client-go: (%s): %s", agent, message)
		return
	}

	logger.Warnf("client-go: <unknown>: %s", message)
}
