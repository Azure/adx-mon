package runner

import (
	"context"
	"net/http"
	"os"

	"github.com/Azure/adx-mon/ingestor"
	"github.com/Azure/adx-mon/pkg/logger"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	namespace          = "adx-mon"
	SHUTDOWN_COMPLETED = "shutdown-completed"
	SHUTDOWN_REQUESTED = "shutdown-requested"
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
		logger.Errorf("failed to get pod annotations: %v", err)
		return err
	}

	//check if shutdown-completed annotation is set
	if _, ok := pod.Annotations[SHUTDOWN_COMPLETED]; ok {
		logger.Infof("shutdown already completed on the pod, skipping shutting down")
		return nil
	}

	//shutdown the service
	if _, ok := pod.Annotations[SHUTDOWN_REQUESTED]; ok {
		logger.Infof("shutting down the service")
		if err := r.httpServer.Close(); err != nil {
			logger.Errorf("failed to close http server: %v", err)
			return err
		}

		if err := r.service.HandleShutdown(); err != nil {
			logger.Errorf("failed to shutdown the service: %v", err)
			return err
		}
		logger.Infof("service shutdown completed")
		//set shutdown-completed annotation
		pod.Annotations[SHUTDOWN_COMPLETED] = "true"
		if _, err := r.k8sClient.CoreV1().Pods(namespace).Update(ctx, pod, metav1.UpdateOptions{}); err != nil {
			logger.Errorf("failed to set shutdown-completed annotation: %v", err)
			return err
		}
	}

	return nil
}

func (r *ShutDownRunner) Name() string {
	return "shutdown"
}
