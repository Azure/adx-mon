package k8s

import (
	"context"
	"fmt"
	"os"

	"github.com/Azure/adx-mon/pkg/logger"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// DiscoverImagePullSecrets retrieves the imagePullSecrets from the current pod.
// This allows the operator to propagate its own pull secrets to workloads it creates.
// Returns an empty slice if running outside a pod or if no pull secrets are configured.
//
// Required environment variables (typically set via Kubernetes downward API):
//
//	env:
//	  - name: POD_NAME
//	    valueFrom:
//	      fieldRef:
//	        fieldPath: metadata.name
//	  - name: POD_NAMESPACE
//	    valueFrom:
//	      fieldRef:
//	        fieldPath: metadata.namespace
func DiscoverImagePullSecrets(ctx context.Context, clientset kubernetes.Interface) []corev1.LocalObjectReference {
	namespace := os.Getenv("POD_NAMESPACE")
	podName := os.Getenv("POD_NAME")

	if namespace == "" || podName == "" {
		logger.Debugf("POD_NAMESPACE or POD_NAME not set, cannot discover imagePullSecrets")
		return nil
	}

	pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		logger.Warnf("Failed to get operator pod %s/%s: %v", namespace, podName, err)
		return nil
	}

	if len(pod.Spec.ImagePullSecrets) == 0 {
		logger.Debugf("No imagePullSecrets found on operator pod")
		return nil
	}

	secrets := make([]corev1.LocalObjectReference, len(pod.Spec.ImagePullSecrets))
	copy(secrets, pod.Spec.ImagePullSecrets)

	names := make([]string, len(secrets))
	for i, s := range secrets {
		names[i] = s.Name
	}
	logger.Infof("Discovered imagePullSecrets from operator pod: %v", names)

	return secrets
}

// ImagePullSecretsToNames converts LocalObjectReferences to a slice of secret names.
// Useful for logging or template rendering.
func ImagePullSecretsToNames(secrets []corev1.LocalObjectReference) []string {
	names := make([]string, len(secrets))
	for i, s := range secrets {
		names[i] = s.Name
	}
	return names
}

// MustDiscoverImagePullSecrets is like DiscoverImagePullSecrets but returns an error
// if the discovery fails when running inside a pod (POD_NAME/POD_NAMESPACE are set).
func MustDiscoverImagePullSecrets(ctx context.Context, clientset kubernetes.Interface) ([]corev1.LocalObjectReference, error) {
	namespace := os.Getenv("POD_NAMESPACE")
	podName := os.Getenv("POD_NAME")

	// If not running in a pod, return empty (not an error)
	if namespace == "" || podName == "" {
		return nil, nil
	}

	pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get operator pod %s/%s: %w", namespace, podName, err)
	}

	if len(pod.Spec.ImagePullSecrets) == 0 {
		return nil, nil
	}

	secrets := make([]corev1.LocalObjectReference, len(pod.Spec.ImagePullSecrets))
	copy(secrets, pod.Spec.ImagePullSecrets)

	names := ImagePullSecretsToNames(secrets)
	logger.Infof("Discovered imagePullSecrets from operator pod: %v", names)

	return secrets, nil
}
