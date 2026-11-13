package runner

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

func TestShutDownRunner_ShutdownNotCalledIfNotAnnotated(t *testing.T) {
	const namespace = "test-namespace"
	// Set up the fake Kubernetes client
	client := fake.NewClientset()

	// Create a fake pod with annotations
	pod := &corev1.Pod{
		Name:        "test-pod",
		Namespace:   namespace,
		Annotations: map[string]string{},
	}
	client.CoreV1().Pods(namespace).Create(context.Background(), pod, metav1.CreateOptions{})

	// Set the HOSTNAME environment variable to the pod name
	os.Setenv("HOSTNAME", "test-pod")

	// Create a fake HTTP server
	httpServer := &http.Server{}

	// Create a fake service
	service := &fakeIngestorService{}

	// Create the ShutDownRunner
	runner := NewShutDownRunnerForNamespace(client, httpServer, service, namespace)

	// Run the ShutDownRunner
	err := runner.Run(context.Background())

	assert.NoError(t, err)
	assert.False(t, service.ShutdownCalled)

}

func TestShutDownRunner_ShutdownNotCalledIfAnnotated(t *testing.T) {
	const namespace = "test-namespace"
	// Set up the fake Kubernetes client
	client := fake.NewClientset()

	// Create a fake pod with annotations
	pod := &corev1.Pod{
		Name:      "test-pod",
		Namespace: namespace,
		Annotations: map[string]string{
			SHUTDOWN_REQUESTED: "true",
		},
	}
	client.CoreV1().Pods(namespace).Create(context.Background(), pod, metav1.CreateOptions{})

	// Set the HOSTNAME environment variable to the pod name
	os.Setenv("HOSTNAME", "test-pod")

	// Create a fake HTTP server
	httpServer := &http.Server{}

	// Create a fake service
	service := &fakeIngestorService{}

	// Create the ShutDownRunner
	runner := NewShutDownRunnerForNamespace(client, httpServer, service, namespace)

	// Run the ShutDownRunner
	err := runner.Run(context.Background())

	assert.NoError(t, err)
	assert.True(t, service.ShutdownCalled)

	updatedPod, err := client.CoreV1().Pods(namespace).Get(context.Background(), "test-pod", metav1.GetOptions{})
	_, ok := updatedPod.Annotations[SHUTDOWN_COMPLETED]
	assert.Equal(t, true, ok)

}

// TestShutDownRunner_K8sCallsBoundedByRunDeadline verifies that the k8s calls
// made inside Run are bounded by a per-run deadline derived from the caller
// context, rather than using the caller context unbounded.
//
// A hung apiserver would block a real (non-cancellable) k8s call for
// client-go's default request timeout (~32s) per scheduler tick, stalling
// Close's wg.Wait(). The fix derives a bounded context for the k8s calls.
//
// The test uses a real (non-fake) k8s client backed by a local HTTP server
// that blocks the Get request until the request context is canceled
// (honoring cancellation, like a real apiserver would via
// http.Request.Context()). This exercises the real client path, which
// honors request cancellation, unlike the fake clientset's reactor (whose
// action does not carry the request context).
//
// Before the fix, Run passed the unbounded caller context (no deadline) to
// Pods().Get, so the request's context was never canceled and the handler
// blocked indefinitely; Run did not return within the bound, and the test
// failed.
//
// After the fix, Run passes a bounded context (k8sCallTimeout) to
// Pods().Get, so the request's context is canceled after k8sCallTimeout,
// the handler returns, Run returns within the bound, and the test passes.
func TestShutDownRunner_K8sCallsBoundedByRunDeadline(t *testing.T) {
	const namespace = "test-namespace"
	const podName = "test-pod"

	// Override the per-run k8s call timeout to a small value for the test.
	origK8sCallTimeout := k8sCallTimeout
	k8sCallTimeout = 200 * time.Millisecond
	t.Cleanup(func() { k8sCallTimeout = origK8sCallTimeout })

	// Set the HOSTNAME environment variable to the pod name.
	oldHostname, hadHostname := os.LookupEnv("HOSTNAME")
	os.Setenv("HOSTNAME", podName)
	t.Cleanup(func() {
		if hadHostname {
			os.Setenv("HOSTNAME", oldHostname)
		} else {
			os.Unsetenv("HOSTNAME")
		}
	})

	// Track whether the handler actually blocked (i.e. the request context
	// was not canceled before the handler ran). This confirms the handler
	// is exercising the "hang" path rather than returning early.
	var blocked atomic.Bool

	// Local HTTP server that blocks the Get request until the request
	// context is canceled. This models a hung apiserver that would
	// otherwise block for client-go's default request timeout (~32s).
	// Because the handler uses r.Context(), the real k8s client's request
	// context (derived from the context Run passes to Get) is honored:
	// when Run passes a bounded context, the request is canceled after
	// k8sCallTimeout and the handler returns.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			// The request was canceled (by a bounded context). This is the
			// "fixed" path: the k8s call was bounded and canceled.
			http.Error(w, r.Context().Err().Error(), http.StatusRequestTimeout)
			return
		case <-time.After(30 * time.Second):
			// The request was not canceled within 30s. This is the
			// "unbounded" path: the k8s call used an unbounded context.
			blocked.Store(true)
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, "{}")
			return
		}
	}))
	defer srv.Close()

	// Build a real (non-fake) k8s client pointed at the local server.
	// Using the real client path ensures the request context is honored
	// (the fake clientset's reactor does not carry the request context).
	restConfig := &rest.Config{
		Host:        srv.URL,
		ContentType: "application/json",
	}
	k8sClient, err := kubernetes.NewForConfig(restConfig)
	assert.NoError(t, err)

	// Create a fake HTTP server and service (unused since the Get request
	// should be canceled before the pod is returned, so Run never reaches
	// the shutdown path).
	httpServer := &http.Server{}
	service := &fakeIngestorService{}

	// Create the ShutDownRunner with the real client.
	runner := NewShutDownRunnerForNamespace(k8sClient, httpServer, service, namespace)

	// Call Run in a goroutine with an unbounded caller context (mirrors the
	// scheduler, which passes s.ctx with no deadline). The only thing that
	// can stop the k8s Get call is a bounded per-run deadline derived inside
	// Run.
	runCtx := context.Background()
	errCh := make(chan error, 1)
	go func() { errCh <- runner.Run(runCtx) }()

	// Assert Run returns within a bound well under client-go's default
	// request timeout (~32s). Before the fix, Run passed the unbounded
	// caller context, so the request was never canceled and the handler
	// blocked for 30s; Run did not return within the bound, failing the
	// assertion. After the fix, Run passes a bounded context
	// (k8sCallTimeout=200ms), so the request is canceled after ~200ms and
	// Run returns within the bound.
	bound := k8sCallTimeout + 5*time.Second
	select {
	case err := <-errCh:
		// The k8s Get call should have been canceled (by the bounded
		// context), so Run should return an error wrapping the context
		// deadline.
		assert.Error(t, err, "Run should return an error when the k8s Get call is bounded by the run deadline")
		assert.Contains(t, err.Error(), "context deadline exceeded",
			"Run should return a deadline error, got: %v", err)
		assert.False(t, blocked.Load(),
			"handler should have observed the request cancellation, meaning the k8s call was bounded")
	case <-time.After(bound):
		assert.Fail(t, "Run did not return within the k8s call timeout bound; the k8s Get call appears to be unbounded (caller ctx used instead of a bounded per-run context)")
	}
}

type fakeIngestorService struct {
	ShutdownCalled bool
}

func (f *fakeIngestorService) HandleReady(w http.ResponseWriter, r *http.Request) {
	return
}

func (f *fakeIngestorService) HandleTransfer(w http.ResponseWriter, r *http.Request) {
	return
}

func (f *fakeIngestorService) Shutdown(ctx context.Context) error {
	f.ShutdownCalled = true
	return nil
}

func (f *fakeIngestorService) Open(ctx context.Context) error {
	return nil
}

func (f *fakeIngestorService) Close() error {
	return nil
}

func (f *fakeIngestorService) UploadSegments(ctx context.Context) error {
	return nil
}

func (f *fakeIngestorService) DisableWrites() error {
	return nil
}
