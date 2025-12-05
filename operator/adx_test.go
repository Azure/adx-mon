package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/Azure/adx-mon/pkg/testutils/kustainer"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/kql"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/kusto/armkusto"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type fakeCredential struct{}

func (fakeCredential) GetToken(ctx context.Context, _ policy.TokenRequestOptions) (azcore.AccessToken, error) {
	return azcore.AccessToken{Token: "fake", ExpiresOn: time.Now().Add(time.Hour)}, nil
}

func TestEnsureDatabasesHandlesNilFederation(t *testing.T) {
	t.Parallel()

	cluster := &adxmonv1.ADXCluster{
		Spec: adxmonv1.ADXClusterSpec{
			ClusterName: "test",
			Endpoint:    "https://example.kusto.windows.net",
			Provision: &adxmonv1.ADXClusterProvisionSpec{
				SubscriptionId: "00000000-0000-0000-0000-000000000000",
				ResourceGroup:  "rg-test",
				Location:       "eastus",
			},
		},
	}

	created, err := ensureDatabases(context.Background(), cluster, fakeCredential{})
	require.NoError(t, err)
	require.False(t, created)
}

func TestEnsureDatabasesSkipsWithoutProvision(t *testing.T) {
	t.Parallel()

	cluster := &adxmonv1.ADXCluster{
		Spec: adxmonv1.ADXClusterSpec{
			ClusterName: "test",
			Endpoint:    "https://example.kusto.windows.net",
		},
	}

	created, err := ensureDatabases(context.Background(), cluster, fakeCredential{})
	require.NoError(t, err)
	require.False(t, created)
}

func TestEnsureDatabasesRequiresSubscriptionAndResourceGroup(t *testing.T) {
	t.Parallel()

	cluster := &adxmonv1.ADXCluster{
		Spec: adxmonv1.ADXClusterSpec{
			ClusterName: "test",
			Endpoint:    "https://example.kusto.windows.net",
			Provision:   &adxmonv1.ADXClusterProvisionSpec{},
		},
	}

	_, err := ensureDatabases(context.Background(), cluster, fakeCredential{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing subscriptionId or resourceGroup")
}

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
		wantType    *armkusto.IdentityType
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
			wantIDs:     nil,
			wantType:    to.Ptr(armkusto.IdentityTypeNone),
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
			// Tests that the operator transitions the cluster to user-assigned identities when requested
			// without removing the existing system identity.
			name: "identity type not user assigned",
			resp: armkusto.ClustersClientGetResponse{
				Cluster: armkusto.Cluster{
					Identity: &armkusto.Identity{
						Type: to.Ptr(armkusto.IdentityTypeSystemAssigned),
					},
				},
			},
			applied: &adxmonv1.AppliedProvisionState{},
			cluster: &adxmonv1.ADXCluster{
				Spec: adxmonv1.ADXClusterSpec{
					Provision: &adxmonv1.ADXClusterProvisionSpec{
						UserAssignedIdentities: []string{"/id1"},
					},
				},
			},
			wantUpdated: true,
			wantIDs:     []string{"/id1"},
			wantType:    to.Ptr(armkusto.IdentityTypeSystemAssignedUserAssigned),
		},
		{
			name: "removing managed identity preserves system assignment",
			resp: armkusto.ClustersClientGetResponse{
				Cluster: armkusto.Cluster{
					Identity: &armkusto.Identity{
						Type:                   to.Ptr(armkusto.IdentityTypeSystemAssignedUserAssigned),
						UserAssignedIdentities: makeUserAssignedIdentitiesMap("/managed"),
					},
				},
			},
			applied: &adxmonv1.AppliedProvisionState{
				UserAssignedIdentities: []string{"/managed"},
			},
			cluster: &adxmonv1.ADXCluster{
				Spec: adxmonv1.ADXClusterSpec{
					Provision: &adxmonv1.ADXClusterProvisionSpec{},
				},
			},
			wantUpdated: true,
			wantIDs:     nil,
			wantType:    to.Ptr(armkusto.IdentityTypeSystemAssigned),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clusterUpdate := armkusto.Cluster{
				Identity: tt.resp.Identity,
			}
			var updated bool
			clusterUpdate, updated = diffIdentities(tt.resp, tt.applied, tt.cluster, clusterUpdate)
			require.Equal(t, tt.wantUpdated, updated)
			if tt.wantIDs == nil {
				require.Nil(t, clusterUpdate.Identity.UserAssignedIdentities)
			} else {
				var gotIDs []string
				for id := range clusterUpdate.Identity.UserAssignedIdentities {
					gotIDs = append(gotIDs, id)
				}
				require.ElementsMatch(t, tt.wantIDs, gotIDs)
			}
			if tt.wantType != nil {
				require.NotNil(t, clusterUpdate.Identity.Type)
				require.Equal(t, *tt.wantType, *clusterUpdate.Identity.Type)
			}
		})
	}
}

func TestMergeDatabaseSpecs(t *testing.T) {
	tests := []struct {
		name       string
		userDbs    []adxmonv1.ADXClusterDatabaseSpec
		discovered []adxmonv1.ADXClusterDatabaseSpec
		wantMerged []adxmonv1.ADXClusterDatabaseSpec
	}{
		{
			name: "adds discovered databases",
			userDbs: []adxmonv1.ADXClusterDatabaseSpec{
				{DatabaseName: "userdb", TelemetryType: adxmonv1.DatabaseTelemetryMetrics},
			},
			discovered: []adxmonv1.ADXClusterDatabaseSpec{
				{DatabaseName: "federateddb"},
			},
			wantMerged: []adxmonv1.ADXClusterDatabaseSpec{
				{DatabaseName: "federateddb"},
				{DatabaseName: "userdb", TelemetryType: adxmonv1.DatabaseTelemetryMetrics},
			},
		},
		{
			name: "overrides empty telemetry",
			userDbs: []adxmonv1.ADXClusterDatabaseSpec{
				{DatabaseName: "logs"},
			},
			discovered: []adxmonv1.ADXClusterDatabaseSpec{
				{DatabaseName: "logs", TelemetryType: adxmonv1.DatabaseTelemetryLogs},
			},
			wantMerged: []adxmonv1.ADXClusterDatabaseSpec{
				{DatabaseName: "logs", TelemetryType: adxmonv1.DatabaseTelemetryLogs},
			},
		},
		{
			name: "preserves explicit telemetry",
			userDbs: []adxmonv1.ADXClusterDatabaseSpec{
				{DatabaseName: "metrics", TelemetryType: adxmonv1.DatabaseTelemetryMetrics},
			},
			discovered: []adxmonv1.ADXClusterDatabaseSpec{
				{DatabaseName: "metrics"},
			},
			wantMerged: []adxmonv1.ADXClusterDatabaseSpec{
				{DatabaseName: "metrics", TelemetryType: adxmonv1.DatabaseTelemetryMetrics},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			merged := mergeDatabaseSpecs(tt.userDbs, tt.discovered)
			require.Equal(t, tt.wantMerged, merged)
		})
	}
}

func TestEnsureHeartbeatTable(t *testing.T) {
	testutils.IntegrationTest(t)
	ctx := context.Background()
	kustoContainer, err := kustainer.Run(ctx, "mcr.microsoft.com/azuredataexplorer/kustainer-linux:latest", kustainer.WithStarted())
	testcontainers.CleanupContainer(t, kustoContainer)
	require.NoError(t, err)

	var (
		dbName  = "NetDefaultDB"
		tblName = "Heartbeat"
	)
	crd := &adxmonv1.ADXCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-operator",
			Namespace: "default",
		},
		Spec: adxmonv1.ADXClusterSpec{
			ClusterName: "adxmon-test",
			Endpoint:    kustoContainer.ConnectionUrl(),
			Role:        to.Ptr(adxmonv1.ClusterRoleFederated),
			Federation: &adxmonv1.ADXClusterFederationSpec{
				HeartbeatDatabase: to.Ptr(dbName),
				HeartbeatTable:    to.Ptr(tblName),
			},
		},
	}
	created, err := ensureHeartbeatTable(ctx, crd)
	require.NoError(t, err)
	require.True(t, created)

	// Second time the table should already exist
	created, err = ensureHeartbeatTable(ctx, crd)
	require.NoError(t, err)
	require.False(t, created)
}

func TestHeartbeatFederatedClusters(t *testing.T) {
	testutils.IntegrationTest(t)
	ctx := context.Background()
	kustainerFederatedCluster, err := kustainer.Run(ctx, "mcr.microsoft.com/azuredataexplorer/kustainer-linux:latest", kustainer.WithStarted())
	testcontainers.CleanupContainer(t, kustainerFederatedCluster)
	require.NoError(t, err)

	kustainerPartitionedCluster, err := kustainer.Run(ctx, "mcr.microsoft.com/azuredataexplorer/kustainer-linux:latest", kustainer.WithStarted())
	testcontainers.CleanupContainer(t, kustainerPartitionedCluster)
	require.NoError(t, err)

	// Create the federated heartbeat table
	var (
		dbName  = "NetDefaultDB"
		tblName = "Heartbeat"
	)
	crd := &adxmonv1.ADXCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-operator",
			Namespace: "default",
		},
		Spec: adxmonv1.ADXClusterSpec{
			ClusterName: "adxmon-federated-test",
			Endpoint:    kustainerFederatedCluster.ConnectionUrl(),
			Role:        to.Ptr(adxmonv1.ClusterRoleFederated),
			Federation: &adxmonv1.ADXClusterFederationSpec{
				HeartbeatDatabase: to.Ptr(dbName),
				HeartbeatTable:    to.Ptr(tblName),
			},
		},
	}
	created, err := ensureHeartbeatTable(ctx, crd)
	require.NoError(t, err)
	require.True(t, created)

	// Create some tables in the partitioned cluster
	ep := kusto.NewConnectionStringBuilder(kustainerPartitionedCluster.ConnectionUrl())
	client, err := kusto.New(ep)
	require.NoError(t, err)

	for _, tableName := range []string{"Table1", "Table2"} {
		stmt := kql.New("").AddUnsafe(fmt.Sprintf(".create table %s (x: int)", tableName))
		_, err = client.Mgmt(ctx, dbName, stmt)
		require.NoError(t, err)
	}

	// Heartbeat the partitioned cluster
	heartbeat := &adxmonv1.ADXCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-operator",
			Namespace: "default",
		},
		Spec: adxmonv1.ADXClusterSpec{
			ClusterName: "adxmon-partitioned-test",
			Endpoint:    kustainerPartitionedCluster.ConnectionUrl(),
			Role:        to.Ptr(adxmonv1.ClusterRolePartition),
			Federation: &adxmonv1.ADXClusterFederationSpec{
				FederationTargets: []adxmonv1.ADXClusterFederatedClusterSpec{
					{
						Endpoint:          kustainerFederatedCluster.ConnectionUrl(),
						HeartbeatDatabase: dbName,
						HeartbeatTable:    tblName,
					},
				},
				Partitioning: &map[string]string{
					"geo": "eu",
				},
			},
		},
	}
	r := &AdxReconciler{}
	result, err := r.HeartbeatFederatedClusters(ctx, heartbeat)
	require.NoError(t, err)
	require.NotZero(t, result)
}

func TestParseHeartbeatRows(t *testing.T) {
	rawSchema := `[{"database":"db1","tables":["t1","t2"]}]`
	rawMeta := map[string]string{"geo": "us"}
	rows := []HeartbeatRow{
		{
			Timestamp:         time.Now(),
			ClusterEndpoint:   "ep1",
			Schema:            json.RawMessage(rawSchema),
			PartitionMetadata: rawMeta,
		},
	}
	schemaByEndpoint, metaByEndpoint := parseHeartbeatRows(rows)
	require.Contains(t, schemaByEndpoint, "ep1")
	require.Equal(t, "db1", schemaByEndpoint["ep1"][0].Database)
	require.Equal(t, rawMeta, metaByEndpoint["ep1"])
}

func TestExtractDatabasesFromSchemas(t *testing.T) {
	schemas := map[string][]ADXClusterSchema{
		"ep1": {{Database: "db1"}},
		"ep2": {{Database: "db2"}},
	}
	dbs := extractDatabasesFromSchemas(schemas)
	require.Contains(t, dbs, "db1")
	require.Contains(t, dbs, "db2")
}

func TestMapTablesToEndpoints(t *testing.T) {
	schemas := map[string][]ADXClusterSchema{
		"ep1": {{Database: "db1", Tables: []string{"t1", "t2"}}},
		"ep2": {{Database: "db1", Tables: []string{"t2"}}},
	}
	m := mapTablesToEndpoints(schemas)
	require.ElementsMatch(t, m["db1"]["t1"], []string{"ep1"})
	require.ElementsMatch(t, m["db1"]["t2"], []string{"ep1", "ep2"})
}

func TestMapTablesToEndpointsIncludesViews(t *testing.T) {
	schemas := map[string][]ADXClusterSchema{
		"ep1": {{Database: "db1", Tables: []string{"t1"}, Views: []string{"v1", "v2"}}},
		"ep2": {{Database: "db1", Tables: []string{"t1"}, Views: []string{"v2"}}},
		"ep3": {{Database: "db2", Views: []string{"v3"}}},
	}
	m := mapTablesToEndpoints(schemas)

	// Tables should be mapped
	require.ElementsMatch(t, m["db1"]["t1"], []string{"ep1", "ep2"})

	// Views should also be mapped
	require.ElementsMatch(t, m["db1"]["v1"], []string{"ep1"})
	require.ElementsMatch(t, m["db1"]["v2"], []string{"ep1", "ep2"})
	require.ElementsMatch(t, m["db2"]["v3"], []string{"ep3"})
}

func TestMapSpokeDatabases(t *testing.T) {
	schemas := map[string][]ADXClusterSchema{
		"https://cluster1.kusto.net": {
			{Database: "Metrics", Tables: []string{"t1"}},
			{Database: "Logs", Tables: []string{"l1"}},
		},
		"https://cluster2.kusto.net": {
			{Database: "Metrics", Tables: []string{"t1", "t2"}},
		},
		"https://cluster3.kusto.net": {
			{Database: "Logs", Tables: []string{"l2"}},
		},
	}
	m := mapSpokeDatabases(schemas)

	// Metrics should have cluster1 and cluster2
	require.ElementsMatch(t, m["Metrics"], []string{"https://cluster1.kusto.net", "https://cluster2.kusto.net"})

	// Logs should have cluster1 and cluster3
	require.ElementsMatch(t, m["Logs"], []string{"https://cluster1.kusto.net", "https://cluster3.kusto.net"})
}

func TestGenerateEntityGroupDefinitions(t *testing.T) {
	tests := []struct {
		name             string
		schemaByEndpoint map[string][]ADXClusterSchema
		hubDatabases     []string
		wantEntityGroups map[string][]string // hubDB -> expected entity group statements
		wantReplication  int                 // expected number of hub DBs with entity groups
		wantGroupsPerHub int                 // expected number of entity groups per hub DB (each group = 2 statements: drop + create)
	}{
		{
			name: "single spoke database, multiple hub databases",
			schemaByEndpoint: map[string][]ADXClusterSchema{
				"https://cluster1.kusto.net": {{Database: "Metrics", Tables: []string{"t1"}}},
				"https://cluster2.kusto.net": {{Database: "Metrics", Tables: []string{"t1"}}},
			},
			hubDatabases:     []string{"HubDB1", "HubDB2", "HubDB3"},
			wantReplication:  3,
			wantGroupsPerHub: 1,
		},
		{
			name: "multiple spoke databases, single hub database",
			schemaByEndpoint: map[string][]ADXClusterSchema{
				"https://cluster1.kusto.net": {
					{Database: "Metrics", Tables: []string{"t1"}},
					{Database: "Logs", Tables: []string{"l1"}},
				},
			},
			hubDatabases:     []string{"HubDB"},
			wantReplication:  1,
			wantGroupsPerHub: 2,
		},
		{
			name: "multiple spoke databases, multiple hub databases",
			schemaByEndpoint: map[string][]ADXClusterSchema{
				"https://cluster1.kusto.net": {
					{Database: "Metrics", Tables: []string{"t1"}},
					{Database: "Logs", Tables: []string{"l1"}},
				},
				"https://cluster2.kusto.net": {
					{Database: "Metrics", Tables: []string{"t2"}},
				},
			},
			hubDatabases:     []string{"Hub1", "Hub2"},
			wantReplication:  2,
			wantGroupsPerHub: 2, // MetricsSpoke and LogsSpoke
		},
		{
			name:             "empty schemas",
			schemaByEndpoint: map[string][]ADXClusterSchema{},
			hubDatabases:     []string{"HubDB"},
			wantReplication:  0,
			wantGroupsPerHub: 0,
		},
		{
			name: "empty hub databases",
			schemaByEndpoint: map[string][]ADXClusterSchema{
				"https://cluster1.kusto.net": {{Database: "Metrics", Tables: []string{"t1"}}},
			},
			hubDatabases:     []string{},
			wantReplication:  0,
			wantGroupsPerHub: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generateEntityGroupDefinitions(tt.schemaByEndpoint, tt.hubDatabases)

			// Check replication count
			require.Len(t, result, tt.wantReplication)

			// Check entity groups per hub (each entity group generates 2 statements: drop + create)
			for hubDB, entityGroups := range result {
				require.Len(t, entityGroups, tt.wantGroupsPerHub*2, "hub database %s should have %d statements (%d entity groups x 2 statements each)", hubDB, tt.wantGroupsPerHub*2, tt.wantGroupsPerHub)
			}
		})
	}
}

func TestGenerateEntityGroupDefinitions_Naming(t *testing.T) {
	schemaByEndpoint := map[string][]ADXClusterSchema{
		"https://cluster1.kusto.net": {{Database: "Metrics", Tables: []string{"t1"}}},
		"https://cluster2.kusto.net": {{Database: "Logs", Tables: []string{"l1"}}},
	}
	hubDatabases := []string{"HubDB"}

	result := generateEntityGroupDefinitions(schemaByEndpoint, hubDatabases)
	require.Contains(t, result, "HubDB")

	entityGroups := result["HubDB"]
	// 2 entity groups x 2 statements each (drop + create) = 4 statements
	require.Len(t, entityGroups, 4)

	// Check naming pattern: {SpokeDatabaseName}Spoke
	// Statements should be in order: drop LogsSpoke, create LogsSpoke, drop MetricsSpoke, create MetricsSpoke
	// (sorted by database name: Logs < Metrics)
	foundMetricsDrop := false
	foundMetricsCreate := false
	foundLogsDrop := false
	foundLogsCreate := false
	for _, stmt := range entityGroups {
		if stmt == ".drop entity_group MetricsSpoke" {
			foundMetricsDrop = true
		}
		if strings.Contains(stmt, ".create entity_group MetricsSpoke") {
			foundMetricsCreate = true
			require.Contains(t, stmt, "cluster('https://cluster1.kusto.net').database('Metrics')")
		}
		if stmt == ".drop entity_group LogsSpoke" {
			foundLogsDrop = true
		}
		if strings.Contains(stmt, ".create entity_group LogsSpoke") {
			foundLogsCreate = true
			require.Contains(t, stmt, "cluster('https://cluster2.kusto.net').database('Logs')")
		}
	}
	require.True(t, foundMetricsDrop, "should have .drop MetricsSpoke")
	require.True(t, foundMetricsCreate, "should have .create MetricsSpoke")
	require.True(t, foundLogsDrop, "should have .drop LogsSpoke")
	require.True(t, foundLogsCreate, "should have .create LogsSpoke")
}

func TestGenerateEntityGroupDefinitions_MultipleEndpoints(t *testing.T) {
	// Test that a spoke database with multiple endpoints includes all of them
	schemaByEndpoint := map[string][]ADXClusterSchema{
		"https://cluster1.kusto.net": {{Database: "Metrics", Tables: []string{"t1"}}},
		"https://cluster2.kusto.net": {{Database: "Metrics", Tables: []string{"t1"}}},
		"https://cluster3.kusto.net": {{Database: "Metrics", Tables: []string{"t1"}}},
	}
	hubDatabases := []string{"HubDB"}

	result := generateEntityGroupDefinitions(schemaByEndpoint, hubDatabases)
	require.Contains(t, result, "HubDB")
	// 1 entity group x 2 statements (drop + create) = 2 statements
	require.Len(t, result["HubDB"], 2)

	// First statement should be drop, second should be create
	require.Equal(t, ".drop entity_group MetricsSpoke", result["HubDB"][0])

	createStmt := result["HubDB"][1]
	require.Contains(t, createStmt, ".create entity_group MetricsSpoke")
	require.Contains(t, createStmt, "cluster('https://cluster1.kusto.net').database('Metrics')")
	require.Contains(t, createStmt, "cluster('https://cluster2.kusto.net').database('Metrics')")
	require.Contains(t, createStmt, "cluster('https://cluster3.kusto.net').database('Metrics')")
}

func TestGenerateEntityGroupDefinitions_DeterministicOrdering(t *testing.T) {
	// Run multiple times to ensure deterministic output
	schemaByEndpoint := map[string][]ADXClusterSchema{
		"https://clusterC.kusto.net": {{Database: "DBZ", Tables: []string{"t1"}}},
		"https://clusterA.kusto.net": {{Database: "DBA", Tables: []string{"t1"}}},
		"https://clusterB.kusto.net": {{Database: "DBM", Tables: []string{"t1"}}},
	}
	hubDatabases := []string{"Hub3", "Hub1", "Hub2"}

	var previousResult map[string][]string
	for i := 0; i < 5; i++ {
		result := generateEntityGroupDefinitions(schemaByEndpoint, hubDatabases)
		if previousResult != nil {
			require.Equal(t, previousResult, result, "results should be deterministic across runs")
		}
		previousResult = result
	}

	// Also verify that entity groups within each hub are sorted by spoke database name
	// Each entity group generates 2 statements (drop + create), so 3 groups = 6 statements
	result := generateEntityGroupDefinitions(schemaByEndpoint, hubDatabases)
	for _, entityGroups := range result {
		require.Len(t, entityGroups, 6) // 3 entity groups x 2 statements each
		// Should be sorted: DBASpoke, DBMSpoke, DBZSpoke (with drop then create for each)
		require.Equal(t, ".drop entity_group DBASpoke", entityGroups[0])
		require.Contains(t, entityGroups[1], ".create entity_group DBASpoke")
		require.Equal(t, ".drop entity_group DBMSpoke", entityGroups[2])
		require.Contains(t, entityGroups[3], ".create entity_group DBMSpoke")
		require.Equal(t, ".drop entity_group DBZSpoke", entityGroups[4])
		require.Contains(t, entityGroups[5], ".create entity_group DBZSpoke")
	}
}

func TestGenerateKustoFunctionDefinitions(t *testing.T) {
	m := map[string]map[string][]string{
		"db1": {
			"t1": {"ep1"},
			"t2": {"ep1", "ep2"},
		},
	}
	funcs := generateKustoFunctionDefinitions(m)
	require.Contains(t, funcs, "db1")
	foundT1 := false
	foundT2 := false
	for _, f := range funcs["db1"] {
		if strings.Contains(f, ".create-or-alter function t1()") {
			foundT1 = true
			// Verify it references the named entity group, not an inline list
			// Note: stored entity groups are referenced without the "entity_group" keyword
			require.Contains(t, f, "macro-expand db1Spoke as X")
			require.NotContains(t, f, "entity_group")
			require.Contains(t, f, "X.t1")
		}
		if strings.Contains(f, ".create-or-alter function t2()") {
			foundT2 = true
			// Verify it references the named entity group, not an inline list
			// Note: stored entity groups are referenced without the "entity_group" keyword
			require.Contains(t, f, "macro-expand db1Spoke as X")
			require.NotContains(t, f, "entity_group")
			require.Contains(t, f, "X.t2")
		}
	}
	require.True(t, foundT1)
	require.True(t, foundT2)
}

func TestSplitKustoScripts(t *testing.T) {
	funcs := []string{"a", "b", "c"}
	preamble := ".execute database script with (ContinueOnErrors=true)\n<|\n"
	const overheadPerScript = 2 // Accounts for additional characters or delimiters in the script

	scripts := splitKustoScripts(funcs, len(preamble)+overheadPerScript) // Only enough room for one function per script
	require.Len(t, scripts, 3)
	for _, script := range scripts {
		require.Len(t, script, 1)
		// Each script part should start with a comment
		require.True(t, strings.HasPrefix(script[0], "//\n"))
	}

	scripts = splitKustoScripts(funcs, 1000) // Large enough for all in one
	require.Len(t, scripts, 1)
	joined := strings.Join(scripts[0], "")
	require.Contains(t, joined, "//\na\n")
	require.Contains(t, joined, "//\nb\n")
	require.Contains(t, joined, "//\nc\n")
}

func TestEnsureHubTables(t *testing.T) {
	testutils.IntegrationTest(t)
	ctx := context.Background()
	kustoContainer, err := kustainer.Run(ctx, "mcr.microsoft.com/azuredataexplorer/kustainer-linux:latest", kustainer.WithStarted())
	testcontainers.CleanupContainer(t, kustoContainer)
	require.NoError(t, err)

	ep := kusto.NewConnectionStringBuilder(kustoContainer.ConnectionUrl())
	client, err := kusto.New(ep)
	require.NoError(t, err)
	defer client.Close()

	database := "NetDefaultDB"
	tables := map[string]string{"MirrorTable": "", "MirrorView": ""}

	for tbl := range tables {
		exists, err := tableExists(ctx, client, database, tbl)
		require.NoError(t, err)
		require.False(t, exists)
	}

	require.NoError(t, ensureHubTables(ctx, client, database, tables))

	for tbl := range tables {
		exists, err := tableExists(ctx, client, database, tbl)
		require.NoError(t, err)
		require.True(t, exists)
	}

	// Re-run to confirm it remains idempotent when tables already exist.
	require.NoError(t, ensureHubTables(ctx, client, database, tables))
}
