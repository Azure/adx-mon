package integration

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/yaml"
)

// TestSummaryRuleIntervalValidationIntegration tests the CEL validation in a real Kubernetes cluster
func TestSummaryRuleIntervalValidationIntegration(t *testing.T) {
	testutils.IntegrationTest(t)
	ctx := context.Background()

	// Start k3s cluster
	k3sContainer, err := k3s.Run(ctx, "rancher/k3s:v1.31.2-k3s1")
	require.NoError(t, err)
	defer func() {
		if err := k3sContainer.Terminate(ctx); err != nil {
			t.Logf("Failed to terminate k3s container: %v", err)
		}
	}()

	// Get kubeconfig
	restConfig, _, err := testutils.GetKubeConfig(ctx, k3sContainer)
	require.NoError(t, err)

	// Create dynamic client for CRD operations
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	require.NoError(t, err)

	// Apply the SummaryRule CRD to the cluster
	err = applySummaryRuleCRD(ctx, dynamicClient)
	require.NoError(t, err, "Failed to apply SummaryRule CRD")

	// Wait for CRD to be ready
	err = waitForCRDReady(ctx, dynamicClient)
	require.NoError(t, err, "CRD not ready")

	// Test cases for validation
	testCases := []struct {
		name          string
		interval      string
		expectSuccess bool
		expectError   string
	}{
		{
			name:          "Valid interval - 1 minute",
			interval:      "1m",
			expectSuccess: true,
		},
		{
			name:          "Valid interval - 5 minutes",
			interval:      "5m",
			expectSuccess: true,
		},
		{
			name:          "Valid interval - 1 hour",
			interval:      "1h",
			expectSuccess: true,
		},
		{
			name:          "Invalid interval - 30 seconds",
			interval:      "30s",
			expectSuccess: false,
			expectError:   "interval must be at least 1 minute",
		},
		{
			name:          "Invalid interval - 45 seconds",
			interval:      "45s",
			expectSuccess: false,
			expectError:   "interval must be at least 1 minute",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create SummaryRule using the Go struct to ensure proper typing
			summaryRule := &adxmonv1.SummaryRule{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "adx-mon.azure.com/v1",
					Kind:       "SummaryRule",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("test-rule-%d", time.Now().UnixNano()),
					Namespace: "default",
				},
				Spec: adxmonv1.SummaryRuleSpec{
					Database: "TestDB",
					Table:    "TestTable",
					Name:     "TestRule",
					Body:     "TestData\n| summarize count() by bin(Timestamp, 1h)",
					Interval: metav1.Duration{
						Duration: parseDuration(tc.interval),
					},
				},
			}

			// Convert to YAML then to unstructured (to mimic real API server interaction)
			yamlBytes, err := yaml.Marshal(summaryRule)
			require.NoError(t, err)

			var obj map[string]interface{}
			err = yaml.Unmarshal(yamlBytes, &obj)
			require.NoError(t, err)

			// Create unstructured object
			unstructuredObj := &unstructured.Unstructured{Object: obj}

			// Attempt to create the resource
			gvr := schema.GroupVersionResource{
				Group:    "adx-mon.azure.com",
				Version:  "v1",
				Resource: "summaryrules",
			}

			_, err = dynamicClient.Resource(gvr).Namespace("default").Create(ctx, unstructuredObj, metav1.CreateOptions{})

			if tc.expectSuccess {
				require.NoError(t, err, "Expected creation to succeed for interval %s", tc.interval)
				t.Logf("✅ Successfully created SummaryRule with interval %s", tc.interval)
			} else {
				require.Error(t, err, "Expected creation to fail for interval %s", tc.interval)
				
				// Check that it's a validation error with the expected message
				statusErr, ok := err.(*apierrors.StatusError)
				require.True(t, ok, "Expected StatusError, got %T: %v", err, err)
				require.Contains(t, statusErr.ErrStatus.Message, tc.expectError,
					"Expected error message to contain '%s', got: %s", tc.expectError, statusErr.ErrStatus.Message)
				
				t.Logf("❌ Correctly rejected SummaryRule with interval %s: %s", tc.interval, statusErr.ErrStatus.Message)
			}
		})
	}
}

// parseDuration parses a Go duration string
func parseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		panic(fmt.Sprintf("Invalid duration: %s", s))
	}
	return d
}

// applySummaryRuleCRD applies the SummaryRule CRD to the cluster by reading the actual CRD file
func applySummaryRuleCRD(ctx context.Context, dynamicClient dynamic.Interface) error {
	// Read the actual CRD YAML file from the repository
	crdPath := "/home/runner/work/adx-mon/adx-mon/kustomize/bases/summaryrules_crd.yaml"
	crdContent, err := readCRDFile(crdPath)
	if err != nil {
		return fmt.Errorf("failed to read CRD file: %w", err)
	}

	// Parse and apply the CRD
	var crdObj map[string]interface{}
	err = yaml.Unmarshal(crdContent, &crdObj)
	if err != nil {
		return fmt.Errorf("failed to unmarshal CRD YAML: %w", err)
	}

	// Create unstructured object
	unstructuredObj := &unstructured.Unstructured{Object: crdObj}

	gvr := schema.GroupVersionResource{
		Group:    "apiextensions.k8s.io",
		Version:  "v1",
		Resource: "customresourcedefinitions",
	}

	_, err = dynamicClient.Resource(gvr).Create(ctx, unstructuredObj, metav1.CreateOptions{})
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return fmt.Errorf("failed to create CRD: %w", err)
	}

	return nil
}

// readCRDFile reads the CRD file from the filesystem
func readCRDFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return io.ReadAll(file)
}

// waitForCRDReady waits for the CRD to be established and ready
func waitForCRDReady(ctx context.Context, dynamicClient dynamic.Interface) error {
	gvr := schema.GroupVersionResource{
		Group:    "apiextensions.k8s.io",
		Version:  "v1",
		Resource: "customresourcedefinitions",
	}
	
	// Wait up to 60 seconds for CRD to be ready
	for i := 0; i < 60; i++ {
		crd, err := dynamicClient.Resource(gvr).Get(ctx, "summaryrules.adx-mon.azure.com", metav1.GetOptions{})
		if err == nil {
			// Check if CRD is established
			conditions, found, err := unstructured.NestedSlice(crd.Object, "status", "conditions")
			if err == nil && found {
				for _, condition := range conditions {
					if condMap, ok := condition.(map[string]interface{}); ok {
						if condType, found := condMap["type"]; found && condType == "Established" {
							if status, found := condMap["status"]; found && status == "True" {
								// Additional wait for the CRD to be fully ready for resource creation
								time.Sleep(3 * time.Second)
								return nil
							}
						}
					}
				}
			}
		}
		time.Sleep(1 * time.Second)
	}
	
	return fmt.Errorf("CRD did not become ready within timeout")
}