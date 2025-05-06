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
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/kusto/armkusto"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/stretchr/testify/require"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
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

func TestAdxClusterCreate(t *testing.T) {
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
		WithStatusSubresource(&adxmonv1.ADXCluster{}).
		Build()

	// Create the reconciler
	r := &AdxReconciler{
		Client: fakeClient, // Use the fake client
		Scheme: scheme,
	}

	// Create a test CRD
	cluster := &adxmonv1.ADXCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-operator",
			Namespace: "default",
		},
		Spec: adxmonv1.ADXClusterSpec{
			ClusterName: fmt.Sprintf("adxmon-%d", time.Now().Unix()),
			Provision: &adxmonv1.ADXClusterProvisionSpec{
				SubscriptionId: subscriptionID,
				ResourceGroup:  t.Name(),
				Location:       "canadacentral",
			},
		},
	}
	require.NoError(t, fakeClient.Create(context.Background(), cluster))

	t.Cleanup(func() {
		t.Logf("Cleaning up resources for test: subscription-id=%s, resource-group=%s", subscriptionID, t.Name())

		cred, err := azidentity.NewDefaultAzureCredential(nil)
		require.NoError(t, err)

		rgClient, err := armresources.NewResourceGroupsClient(subscriptionID, cred, nil)
		require.NoError(t, err)

		_, err = rgClient.BeginDelete(context.Background(), t.Name(), nil)
		require.NoError(t, err)
	})

	var fetched adxmonv1.ADXCluster
	require.Eventually(t, func() bool {
		err = r.Client.Get(context.Background(), types.NamespacedName{
			Name:      cluster.GetName(),
			Namespace: cluster.GetNamespace(),
		}, &fetched)
		if err != nil {
			return false
		}

		result, err := r.Reconcile(context.Background(), reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      cluster.GetName(),
				Namespace: cluster.GetNamespace(),
			},
		})
		require.NoError(t, err)
		require.NotNil(t, result)

		return meta.IsStatusConditionTrue(fetched.Status.Conditions, adxmonv1.ADXClusterConditionOwner)
	}, 30*time.Minute, time.Minute, "Wait for Kusto to become ready")
}

func TestDiffSkus(t *testing.T) {
	tests := []struct {
		name                  string
		resp                  armkusto.ClustersClientGetResponse // The state of the cluster in Azure
		appliedProvisionState *adxmonv1.AppliedProvisionState    // The configuration from when the cluster was previously upserted
		cluster               *adxmonv1.ADXCluster               // The current configuration, or the desired state
		wantUpdated           bool
		wantSkuName           string
		wantTier              string
	}{
		// Actual state matches the previous configuration and the current configuration, nothing to do
		{
			name: "no changes needed",
			resp: armkusto.ClustersClientGetResponse{
				Cluster: armkusto.Cluster{
					SKU: &armkusto.AzureSKU{
						Name: (*armkusto.AzureSKUName)(to.Ptr("Standard_L8as_v3")),
						Tier: (*armkusto.AzureSKUTier)(to.Ptr("Standard")),
					},
					Name: to.Ptr("adxmon-test"),
				},
			},
			appliedProvisionState: &adxmonv1.AppliedProvisionState{
				SkuName: "Standard_L8as_v3",
				Tier:    "Standard",
			},
			cluster: &adxmonv1.ADXCluster{
				Spec: adxmonv1.ADXClusterSpec{
					Provision: &adxmonv1.ADXClusterProvisionSpec{
						SkuName: "Standard_L8as_v3",
						Tier:    "Standard",
					},
				},
			},
			wantUpdated: false,
		},
		// Our CRD / configuration was updated, and the actual state in Azure matches the previously applied state, so we can update
		{
			name: "sku name updated",
			resp: armkusto.ClustersClientGetResponse{
				Cluster: armkusto.Cluster{
					SKU: &armkusto.AzureSKU{
						Name: (*armkusto.AzureSKUName)(to.Ptr("Standard_L8as_v3")),
						Tier: (*armkusto.AzureSKUTier)(to.Ptr("Standard")),
					},
					Name: to.Ptr("adxmon-test"),
				},
			},
			appliedProvisionState: &adxmonv1.AppliedProvisionState{
				SkuName: "Standard_L8as_v3",
				Tier:    "Standard",
			},
			cluster: &adxmonv1.ADXCluster{
				Spec: adxmonv1.ADXClusterSpec{
					Provision: &adxmonv1.ADXClusterProvisionSpec{
						SkuName: "Standard_L16as_v3",
						Tier:    "Standard",
					},
				},
			},
			wantUpdated: true,
			wantSkuName: "Standard_L16as_v3",
			wantTier:    "Standard",
		},
		// The Sku name was updated in Azure, we expect to take no action
		{
			name: "sku name updated manually",
			resp: armkusto.ClustersClientGetResponse{
				Cluster: armkusto.Cluster{
					SKU: &armkusto.AzureSKU{
						Name: (*armkusto.AzureSKUName)(to.Ptr("Standard_L16as_v3")),
						Tier: (*armkusto.AzureSKUTier)(to.Ptr("Standard")),
					},
					Name: to.Ptr("adxmon-test"),
				},
			},
			appliedProvisionState: &adxmonv1.AppliedProvisionState{
				SkuName: "Standard_L8as_v3",
				Tier:    "Standard",
			},
			cluster: &adxmonv1.ADXCluster{
				Spec: adxmonv1.ADXClusterSpec{
					Provision: &adxmonv1.ADXClusterProvisionSpec{
						SkuName: "Standard_L16as_v3",
						Tier:    "Standard",
					},
				},
			},
			wantUpdated: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clusterUpdate, updated := diffSkus(tt.resp, tt.appliedProvisionState, tt.cluster)
			require.Equal(t, tt.wantUpdated, updated)

			if updated {
				require.NotNil(t, clusterUpdate.SKU)
				require.Equal(t, tt.wantSkuName, string(*clusterUpdate.SKU.Name))
				require.Equal(t, tt.wantTier, string(*clusterUpdate.SKU.Tier))
			}
		})
	}
}

func TestDiffIdentities(t *testing.T) {
	makeUserAssignedIdentitiesMap := func(ids ...string) map[string]*armkusto.ComponentsSgqdofSchemasIdentityPropertiesUserassignedidentitiesAdditionalproperties {
		m := make(map[string]*armkusto.ComponentsSgqdofSchemasIdentityPropertiesUserassignedidentitiesAdditionalproperties)
		for _, id := range ids {
			m[id] = &armkusto.ComponentsSgqdofSchemasIdentityPropertiesUserassignedidentitiesAdditionalproperties{}
		}
		return m
	}

	tests := []struct {
		name        string
		resp        armkusto.ClustersClientGetResponse // The state of the cluster in Azure
		applied     *adxmonv1.AppliedProvisionState    // The configuration from when the cluster was previously upserted
		cluster     *adxmonv1.ADXCluster               // The current configuration, or the desired state
		wantUpdated bool
		wantIDs     []string
	}{
		{
			// Scenario: Adding a new user-assigned identity via the CRD when none existed before.
			// Tests that the operator detects the addition and updates the cluster accordingly.
			name: "add identity",
			resp: armkusto.ClustersClientGetResponse{
				Cluster: armkusto.Cluster{
					Identity: &armkusto.Identity{
						Type:                   to.Ptr(armkusto.IdentityTypeUserAssigned),
						UserAssignedIdentities: makeUserAssignedIdentitiesMap(),
					},
				},
			},
			applied: &adxmonv1.AppliedProvisionState{
				UserAssignedIdentities: []string{},
			},
			cluster: &adxmonv1.ADXCluster{
				Spec: adxmonv1.ADXClusterSpec{
					Provision: &adxmonv1.ADXClusterProvisionSpec{
						UserAssignedIdentities: []string{"/id1"},
					},
				},
			},
			wantUpdated: true,
			wantIDs:     []string{"/id1"},
		},
		{
			// Scenario: Removing a user-assigned identity from the CRD when one existed before.
			// Tests that the operator detects the removal and updates the cluster accordingly.
			name: "remove identity",
			resp: armkusto.ClustersClientGetResponse{
				Cluster: armkusto.Cluster{
					Identity: &armkusto.Identity{
						Type:                   to.Ptr(armkusto.IdentityTypeUserAssigned),
						UserAssignedIdentities: makeUserAssignedIdentitiesMap("/id1"),
					},
				},
			},
			applied: &adxmonv1.AppliedProvisionState{
				UserAssignedIdentities: []string{"/id1"},
			},
			cluster: &adxmonv1.ADXCluster{
				Spec: adxmonv1.ADXClusterSpec{
					Provision: &adxmonv1.ADXClusterProvisionSpec{
						UserAssignedIdentities: []string{},
					},
				},
			},
			wantUpdated: true,
			wantIDs:     []string{},
		},
		{
			// Scenario: No change in user-assigned identities between CRD, applied, and Azure state.
			// Tests that the operator does not trigger an update when nothing has changed.
			name: "no change",
			resp: armkusto.ClustersClientGetResponse{
				Cluster: armkusto.Cluster{
					Identity: &armkusto.Identity{
						Type:                   to.Ptr(armkusto.IdentityTypeUserAssigned),
						UserAssignedIdentities: makeUserAssignedIdentitiesMap("/id1"),
					},
				},
			},
			applied: &adxmonv1.AppliedProvisionState{
				UserAssignedIdentities: []string{"/id1"},
			},
			cluster: &adxmonv1.ADXCluster{
				Spec: adxmonv1.ADXClusterSpec{
					Provision: &adxmonv1.ADXClusterProvisionSpec{
						UserAssignedIdentities: []string{"/id1"},
					},
				},
			},
			wantUpdated: false,
			wantIDs:     []string{"/id1"},
		},
		{
			// Scenario: There is an unmanaged identity present in Azure that is not tracked by the operator.
			// Tests that the operator preserves unmanaged identities when updating the set.
			name: "preserve unmanaged identity",
			resp: armkusto.ClustersClientGetResponse{
				Cluster: armkusto.Cluster{
					Identity: &armkusto.Identity{
						Type:                   to.Ptr(armkusto.IdentityTypeUserAssigned),
						UserAssignedIdentities: makeUserAssignedIdentitiesMap("/id1", "/unmanaged"),
					},
				},
			},
			applied: &adxmonv1.AppliedProvisionState{
				UserAssignedIdentities: []string{"/id1"},
			},
			cluster: &adxmonv1.ADXCluster{
				Spec: adxmonv1.ADXClusterSpec{
					Provision: &adxmonv1.ADXClusterProvisionSpec{
						UserAssignedIdentities: []string{},
					},
				},
			},
			wantUpdated: true,
			wantIDs:     []string{"/unmanaged"},
		},
		{
			// Scenario: The identity type is not user-assigned (e.g., system-assigned only).
			// Tests that the operator does not attempt to update user-assigned identities in this case.
			name: "identity type not user assigned",
			resp: armkusto.ClustersClientGetResponse{
				Cluster: armkusto.Cluster{
					Identity: &armkusto.Identity{
						Type: to.Ptr(armkusto.IdentityTypeSystemAssigned),
					},
				},
			},
			applied: &adxmonv1.AppliedProvisionState{
				UserAssignedIdentities: []string{"/id1"},
			},
			cluster: &adxmonv1.ADXCluster{
				Spec: adxmonv1.ADXClusterSpec{
					Provision: &adxmonv1.ADXClusterProvisionSpec{
						UserAssignedIdentities: []string{"/id1"},
					},
				},
			},
			wantUpdated: false,
			wantIDs:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clusterUpdate := armkusto.Cluster{
				Identity: tt.resp.Identity,
			}
			updated := diffIdentities(tt.resp, tt.applied, tt.cluster, &clusterUpdate)
			require.Equal(t, tt.wantUpdated, updated)
			if tt.wantIDs == nil {
				require.Nil(t, clusterUpdate.Identity.UserAssignedIdentities)
				return
			}
			var gotIDs []string
			for id := range clusterUpdate.Identity.UserAssignedIdentities {
				gotIDs = append(gotIDs, id)
			}
			require.ElementsMatch(t, tt.wantIDs, gotIDs)
		})
	}
}
