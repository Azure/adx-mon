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
	defaultNamespace   = "adx-mon"
	SHUTDOWN_COMPLETED = "shutdown-completed"
	SHUTDOWN_REQUESTED = "shutdown-requested"
	shutdownTimeout    = 5 * time.Minute
)

// k8sCallTimeout bounds the per-run kubernetes API calls made inside Run.
var k8sCallTimeout = 30 * time.Second

type ShutDownRunner struct {
	k8sClient  kubernetes.Interface
	httpServer *http.Server
	service    ingestor.Interface
	namespace  string
}

func NewShutDownRunner(cli kubernetes.Interface, http *http.Server, svc ingestor.Interface) *ShutDownRunner {
	return NewShutDownRunnerForNamespace(cli, http, svc, defaultNamespace)
}

func NewShutDownRunnerForNamespace(cli kubernetes.Interface, http *http.Server, svc ingestor.Interface, namespace string) *ShutDownRunner {
	return &ShutDownRunner{
		k8sClient:  cli,
		httpServer: http,
		service:    svc,
		namespace:  namespace,
	}
}

func (r *ShutDownRunner) Run(ctx context.Context) error {

	// Bound the kubernetes API calls for this run. The caller context (from
	// the scheduler) has no deadline, so a hung apiserver could block this
	// goroutine for client-go's default request timeout (~32s) per tick,
	// stalling Close's wg.Wait(). Derive a bounded context for the k8s calls
	// only; the service shutdown keeps its own (longer) bound below.
	runCtx, cancel := context.WithTimeout(ctx, k8sCallTimeout)
	defer cancel()

	//get ingestor pod in which this runner is running
	pod, err := r.k8sClient.CoreV1().Pods(r.namespace).Get(runCtx, os.Getenv("HOSTNAME"), metav1.GetOptions{})
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
		// The service shutdown needs up to shutdownTimeout (5 min), which is
		// longer than k8sCallTimeout. Derive it from the ORIGINAL caller
		// context (not runCtx) so it is not cut off by the shorter k8s
		// bound.
		timeoutCtx, cancel := context.WithTimeout(ctx, shutdownTimeout)
		defer cancel()

		if err := r.service.Shutdown(timeoutCtx); err != nil {
			return fmt.Errorf("failed to shutdown the service: %v", err)
		}

		logger.Infof("Service shutdown completed")
		// Create a patch to set the shutdown-completed annotation
		patch := fmt.Appendf(nil, `{"metadata":{"annotations":{"%s":"%s"}}}`, SHUTDOWN_COMPLETED, time.Now().Format(time.RFC3339))
		patchCtx, patchCancel := context.WithTimeout(ctx, k8sCallTimeout)
		defer patchCancel()
		if _, err := r.k8sClient.CoreV1().Pods(r.namespace).Patch(patchCtx, pod.Name, types.StrategicMergePatchType, patch, metav1.PatchOptions{}); err != nil {
			return fmt.Errorf("failed to patch shutdown-completed annotation: %v", err)
		}
	}

	return nil
}

func (r *ShutDownRunner) Name() string {
	return "shutdown"
}
