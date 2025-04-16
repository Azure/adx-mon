package runner

import (
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/types"
	"net/http"
	"os"
	"time"

	"github.com/Azure/adx-mon/ingestor"
	"github.com/Azure/adx-mon/pkg/logger"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	namespace          = "adx-mon"
	SHUTDOWN_COMPLETED = "shutdown-completed"
	SHUTDOWN_REQUESTED = "shutdown-requested"
	shutdownTimeout    = 5 * time.Minute
)

type ShutDownRunner struct {
	k8sClient  kubernetes.Interface
	httpServer *http.Server
	service    ingestor.Interface
}

func NewShutDownRunner(cli kubernetes.Interface, http *http.Server, svc ingestor.Interface) *ShutDownRunner {
	return &ShutDownRunner{
		k8sClient:  cli,
		httpServer: http,
		service:    svc,
	}
}

func (r *ShutDownRunner) Run(ctx context.Context) error {

	//get ingestor pod in which this runner is running
	pod, err := r.k8sClient.CoreV1().Pods(namespace).Get(ctx, os.Getenv("HOSTNAME"), metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get pod annotations: %v", err)
	}

	//check if shutdown-completed annotation is set
	if _, ok := pod.Annotations[SHUTDOWN_COMPLETED]; ok {
		logger.Infof("Shutdown already completed on the pod, skipping shutting down")
		return nil
	}

	//shutdown the service
	if _, ok := pod.Annotations[SHUTDOWN_REQUESTED]; ok {
		logger.Infof("Shutting down the service")
		if err := r.httpServer.Close(); err != nil {
			return fmt.Errorf("failed to close http server: %v", err)
		}
		timeoutCtx, cancel := context.WithTimeout(ctx, shutdownTimeout)
		defer cancel()

		if err := r.service.Shutdown(timeoutCtx); err != nil {
			return fmt.Errorf("failed to shutdown the service: %v", err)
		}

		logger.Infof("Service shutdown completed")
		// Create a patch to set the shutdown-completed annotation
		patch := []byte(fmt.Sprintf(`{"metadata":{"annotations":{"%s":"%s"}}}`, SHUTDOWN_COMPLETED, time.Now().Format(time.RFC3339)))
		if _, err := r.k8sClient.CoreV1().Pods(namespace).Patch(ctx, pod.Name, types.StrategicMergePatchType, patch, metav1.PatchOptions{}); err != nil {
			return fmt.Errorf("failed to patch shutdown-completed annotation: %v", err)
		}
	}

	return nil
}

func (r *ShutDownRunner) Name() string {
	return "shutdown"
}
