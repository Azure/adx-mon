package operator

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/stretchr/testify/require"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGetIMDSMetadata(t *testing.T) {
	tests := []struct {
		name           string
		serverResponse string
		statusCode     int
		wantRegion     string
		wantSubID      string
		wantRG         string
		wantAKSName    string
		wantOK         bool
	}{
		{
			name: "successful response",
			serverResponse: `{
                "compute": {
                    "location": "eastus",
                    "subscriptionId": "sub-123",
                    "resourceGroupName": "rg-test",
                    "name": "aks-nodepool1-12345678-vmss000000"
                }
            }`,
			statusCode:  200,
			wantRegion:  "eastus",
			wantSubID:   "sub-123",
			wantRG:      "rg-test",
			wantAKSName: "aks",
			wantOK:      true,
		},
		{
			name:           "server error",
			serverResponse: `{}`,
			statusCode:     500,
			wantOK:         false,
		},
		{
			name:           "invalid json",
			serverResponse: `invalid json`,
			statusCode:     200,
			wantOK:         false,
		},
		{
			name: "missing required fields",
			serverResponse: `{
                "compute": {
                    "name": "test-vm"
                }
            }`,
			statusCode: 200,
			wantOK:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify metadata header
				if r.Header.Get("Metadata") != "true" {
					t.Error("Metadata header not set")
				}

				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.serverResponse))
			}))
			defer ts.Close()

			region, subID, rg, aksName, ok := getIMDSMetadata(context.Background(), ts.URL)
			require.Equal(t, tt.wantOK, ok)

			if ok {
				require.Equal(t, tt.wantRegion, region)
				require.Equal(t, tt.wantSubID, subID)
				require.Equal(t, tt.wantRG, rg)
				require.Equal(t, tt.wantAKSName, aksName)
			}
		})
	}
}

func TestArmAdxCluster(t *testing.T) {
	t.Skip("This is a manual test that requires az cli authentication")

	// Use `az login --use-device-code` to authenticate
	// NOTE: This is a long test that will generate a real ADX cluster. The test will
	// attempt to cleanup any created resources.

	// Get subscription ID from az cli
	output, err := exec.Command("az", "account", "show", "--query", "id", "-o", "tsv").Output()
	require.NoError(t, err, "Failed to get subscription ID from az cli")
	subscriptionID := strings.TrimSpace(string(output))
	require.NotEmpty(t, subscriptionID, "Subscription ID is empty")

	// Build scheme
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, adxmonv1.AddToScheme(scheme))

	// Create a fake client
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects().
		WithStatusSubresource(&adxmonv1.Operator{}).
		Build()

	// Create the reconciler
	r := &Reconciler{
		Client:    fakeClient, // Use the fake client
		Scheme:    scheme,
		AdxCtor:   CreateAdxCluster,
		AdxUpdate: EnsureAdxClusterConfiguration,
		AdxRdy:    ArmAdxReady,
	}

	// Create a test operator CR
	operator := &adxmonv1.Operator{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-operator",
			Namespace: "default",
		},
		Spec: adxmonv1.OperatorSpec{
			ADX: &adxmonv1.ADXConfig{
				Clusters: []adxmonv1.ADXClusterSpec{
					{
						Name: fmt.Sprintf("adxmon-%d", time.Now().Unix()),
						Provision: &adxmonv1.ADXClusterProvisionSpec{
							SubscriptionID: subscriptionID,
							ResourceGroup:  t.Name(),
							Region:         "canadacentral",
						},
					},
				},
			},
		},
	}
	require.NoError(t, fakeClient.Create(context.Background(), operator))
	result, err := handleAdxEvent(context.Background(), r, operator)
	require.NoError(t, err)
	require.NotNil(t, result)

	t.Cleanup(func() {
		t.Logf("Cleaning up resources for test: subscription-id=%s, resource-group=%s", subscriptionID, t.Name())

		cred, err := azidentity.NewDefaultAzureCredential(nil)
		require.NoError(t, err)

		rgClient, err := armresources.NewResourceGroupsClient(subscriptionID, cred, nil)
		require.NoError(t, err)

		_, err = rgClient.BeginDelete(context.Background(), t.Name(), nil)
		require.NoError(t, err)
	})

	var fetched adxmonv1.Operator
	require.Eventually(t, func() bool {
		err = r.Client.Get(context.Background(), types.NamespacedName{
			Name:      operator.Name,
			Namespace: operator.Namespace,
		}, &fetched)
		if err != nil {
			return false
		}

		result, err := handleAdxEvent(context.Background(), r, &fetched)
		require.NoError(t, err)
		require.NotNil(t, result)

		return meta.IsStatusConditionTrue(fetched.Status.Conditions, adxmonv1.ADXClusterConditionOwner)
	}, 30*time.Minute, time.Minute, "Wait for Kusto to become ready")
}
