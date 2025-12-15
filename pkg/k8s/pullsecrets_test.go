package k8s

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestDiscoverImagePullSecrets(t *testing.T) {
	tests := []struct {
		name           string
		podNamespace   string
		podName        string
		pod            *corev1.Pod
		expectedNames  []string
		expectNil      bool
	}{
		{
			name:         "no env vars set",
			podNamespace: "",
			podName:      "",
			expectNil:    true,
		},
		{
			name:         "pod not found",
			podNamespace: "test-ns",
			podName:      "missing-pod",
			expectNil:    true,
		},
		{
			name:         "pod has no pull secrets",
			podNamespace: "test-ns",
			podName:      "operator",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "operator",
					Namespace: "test-ns",
				},
				Spec: corev1.PodSpec{},
			},
			expectNil: true,
		},
		{
			name:         "pod has pull secrets",
			podNamespace: "test-ns",
			podName:      "operator",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "operator",
					Namespace: "test-ns",
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets: []corev1.LocalObjectReference{
						{Name: "acr-pull-secret"},
						{Name: "another-secret"},
					},
				},
			},
			expectedNames: []string{"acr-pull-secret", "another-secret"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			if tt.podNamespace != "" {
				os.Setenv("POD_NAMESPACE", tt.podNamespace)
				defer os.Unsetenv("POD_NAMESPACE")
			} else {
				os.Unsetenv("POD_NAMESPACE")
			}
			if tt.podName != "" {
				os.Setenv("POD_NAME", tt.podName)
				defer os.Unsetenv("POD_NAME")
			} else {
				os.Unsetenv("POD_NAME")
			}

			// Create fake clientset
			var clientset *fake.Clientset
			if tt.pod != nil {
				clientset = fake.NewSimpleClientset(tt.pod)
			} else {
				clientset = fake.NewSimpleClientset()
			}

			// Run discovery
			secrets := DiscoverImagePullSecrets(context.Background(), clientset)

			if tt.expectNil {
				require.Nil(t, secrets)
			} else {
				require.NotNil(t, secrets)
				names := ImagePullSecretsToNames(secrets)
				require.Equal(t, tt.expectedNames, names)
			}
		})
	}
}

func TestMustDiscoverImagePullSecrets(t *testing.T) {
	tests := []struct {
		name          string
		podNamespace  string
		podName       string
		pod           *corev1.Pod
		expectedNames []string
		expectError   bool
	}{
		{
			name:         "no env vars - returns nil without error",
			podNamespace: "",
			podName:      "",
			expectError:  false,
		},
		{
			name:         "pod not found - returns error",
			podNamespace: "test-ns",
			podName:      "missing-pod",
			expectError:  true,
		},
		{
			name:         "pod has pull secrets",
			podNamespace: "test-ns",
			podName:      "operator",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "operator",
					Namespace: "test-ns",
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets: []corev1.LocalObjectReference{
						{Name: "acr-pull-secret"},
					},
				},
			},
			expectedNames: []string{"acr-pull-secret"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.podNamespace != "" {
				os.Setenv("POD_NAMESPACE", tt.podNamespace)
				defer os.Unsetenv("POD_NAMESPACE")
			} else {
				os.Unsetenv("POD_NAMESPACE")
			}
			if tt.podName != "" {
				os.Setenv("POD_NAME", tt.podName)
				defer os.Unsetenv("POD_NAME")
			} else {
				os.Unsetenv("POD_NAME")
			}

			var clientset *fake.Clientset
			if tt.pod != nil {
				clientset = fake.NewSimpleClientset(tt.pod)
			} else {
				clientset = fake.NewSimpleClientset()
			}

			secrets, err := MustDiscoverImagePullSecrets(context.Background(), clientset)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				if tt.expectedNames != nil {
					names := ImagePullSecretsToNames(secrets)
					require.Equal(t, tt.expectedNames, names)
				}
			}
		})
	}
}

func TestImagePullSecretsToNames(t *testing.T) {
	secrets := []corev1.LocalObjectReference{
		{Name: "secret-a"},
		{Name: "secret-b"},
		{Name: "secret-c"},
	}

	names := ImagePullSecretsToNames(secrets)

	require.Equal(t, []string{"secret-a", "secret-b", "secret-c"}, names)
}
