package runner

import (
	"context"
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestShutDownRunner_ShutdownNotCalledIfNotAnnotated(t *testing.T) {
	// Set up the fake Kubernetes client
	client := fake.NewClientset()

	// Create a fake pod with annotations
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-pod",
			Namespace:   namespace,
			Annotations: map[string]string{},
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
	runner := NewShutDownRunner(client, httpServer, service)

	// Run the ShutDownRunner
	err := runner.Run(context.Background())

	assert.NoError(t, err)
	assert.False(t, service.ShutdownCalled)

}

func TestShutDownRunner_ShutdownNotCalledIfAnnotated(t *testing.T) {
	// Set up the fake Kubernetes client
	client := fake.NewClientset()

	// Create a fake pod with annotations
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: namespace,
			Annotations: map[string]string{
				SHUTDOWN_REQUESTED: "true",
			},
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
	runner := NewShutDownRunner(client, httpServer, service)

	// Run the ShutDownRunner
	err := runner.Run(context.Background())

	assert.NoError(t, err)
	assert.True(t, service.ShutdownCalled)

	updatedPod, err := client.CoreV1().Pods(namespace).Get(context.Background(), "test-pod", metav1.GetOptions{})
	assert.Equal(t, "true", updatedPod.Annotations[SHUTDOWN_COMPLETED])

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

func (f *fakeIngestorService) HandleShutdown() error {
	f.ShutdownCalled = true
	return nil
}

func (f *fakeIngestorService) Open(ctx context.Context) error {
	return nil
}

func (f *fakeIngestorService) Close() error {
	return nil
}

func (f *fakeIngestorService) UploadSegments() error {
	return nil
}

func (f *fakeIngestorService) DisableWrites() error {
	return nil
}
