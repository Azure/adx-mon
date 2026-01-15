package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	kustoerrors "github.com/Azure/azure-kusto-go/kusto/data/errors"
	kustotable "github.com/Azure/azure-kusto-go/kusto/data/table"
	kustotypes "github.com/Azure/azure-kusto-go/kusto/data/types"
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

func TestCollectInventoryByDatabase(t *testing.T) {
	schemas := map[string][]ADXClusterSchema{
		"ep1": {{Database: "db1", Tables: []string{"t1", "t2"}, Views: []string{"view1"}}},
		"ep2": {{Database: "db1", Tables: []string{"t2"}, Views: []string{"view2", ""}}},
	}
	inventory := collectInventoryByDatabase(schemas)
	require.Contains(t, inventory, "db1")

	var names []string
	for name := range inventory["db1"] {
		names = append(names, name)
	}
	require.ElementsMatch(t, []string{"t1", "t2", "view1", "view2"}, names)
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
		}
		if strings.Contains(f, ".create-or-alter function t2()") {
			foundT2 = true
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

func TestDatabaseExists(t *testing.T) {
	testutils.IntegrationTest(t)
	ctx := context.Background()
	kustoContainer, err := kustainer.Run(ctx, "mcr.microsoft.com/azuredataexplorer/kustainer-linux:latest", kustainer.WithStarted())
	testcontainers.CleanupContainer(t, kustoContainer)
	require.NoError(t, err)

	cluster := &adxmonv1.ADXCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: adxmonv1.ADXClusterSpec{
			ClusterName: "adxmon-test",
			Endpoint:    kustoContainer.ConnectionUrl(),
			Databases: []adxmonv1.ADXClusterDatabaseSpec{
				{DatabaseName: "NetDefaultDB"},
			},
		},
	}

	// Test that the default database exists (NetDefaultDB is created by kustainer)
	exists, err := databaseExists(ctx, cluster, "NetDefaultDB")
	require.NoError(t, err)
	require.True(t, exists, "NetDefaultDB should exist")

	// Test that a non-existent database returns false
	exists, err = databaseExists(ctx, cluster, "NonExistentDB")
	require.NoError(t, err)
	require.False(t, exists, "NonExistentDB should not exist")
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

type stubKustoQueryClient struct {
	t              *testing.T
	iterator       *kusto.RowIterator
	err            error
	expectDatabase string
	expectFunction string
	called         bool
}

func (s *stubKustoQueryClient) Query(ctx context.Context, db string, query kusto.Statement, options ...kusto.QueryOption) (*kusto.RowIterator, error) {
	if s.expectDatabase != "" {
		require.Equal(s.t, s.expectDatabase, db)
	}
	if s.expectFunction != "" {
		require.Contains(s.t, query.String(), s.expectFunction)
	}
	s.called = true
	if s.err != nil {
		return nil, s.err
	}
	return s.iterator, nil
}

func newMockBlockedClusterIterator(t *testing.T, rows []blockedClusterRow) *kusto.RowIterator {
	t.Helper()
	columns := kustotable.Columns{
		{Name: "ClusterEndpoint", Type: kustotypes.String},
		{Name: "Endpoint", Type: kustotypes.String},
	}
	mockRows, err := kusto.NewMockRows(columns)
	require.NoError(t, err)
	for _, row := range rows {
		require.NoError(t, mockRows.Struct(&row))
	}
	iter := &kusto.RowIterator{}
	require.NoError(t, iter.Mock(mockRows))
	return iter
}

func makeBlockedCluster(static []string, fn *adxmonv1.ADXClusterFederationBlockedClustersFunctionSpec) *adxmonv1.ADXCluster {
	return &adxmonv1.ADXCluster{
		Spec: adxmonv1.ADXClusterSpec{
			ClusterName: "test-cluster",
			Federation: &adxmonv1.ADXClusterFederationSpec{
				BlockedClusters: &adxmonv1.ADXClusterFederationBlockedClustersSpec{
					Static:        append([]string(nil), static...),
					KustoFunction: fn,
				},
			},
		},
	}
}

func TestNormalizeEndpoint(t *testing.T) {
	t.Parallel()
	testCases := map[string]struct {
		input string
		want  string
	}{
		"empty":       {input: "", want: ""},
		"trim":        {input: "  HTTPS://Example.kusto.windows.net/  ", want: "https://example.kusto.windows.net"},
		"no-scheme":   {input: "FoO/", want: "foo"},
		"already-ok":  {input: "https://cluster", want: "https://cluster"},
		"multi-slash": {input: "https://cluster///", want: "https://cluster"},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, normalizeEndpoint(tc.input))
		})
	}
}

func TestRestErrorIndicatesNotFound(t *testing.T) {
	t.Parallel()
	makeHTTPError := func(body string) *kustoerrors.HttpError {
		return kustoerrors.HTTP(
			kustoerrors.OpQuery,
			"404",
			http.StatusNotFound,
			io.NopCloser(strings.NewReader(body)),
			"prefix",
		)
	}

	t.Run("code", func(t *testing.T) {
		err := makeHTTPError(`{"error":{"code":"NotFound"}}`)
		require.True(t, restErrorIndicatesNotFound(&err.KustoError))
	})

	t.Run("message", func(t *testing.T) {
		err := makeHTTPError(`{"error":{"message":"Function does not exist"}}`)
		require.True(t, restErrorIndicatesNotFound(&err.KustoError))
	})
}

func TestContainsNotFoundText(t *testing.T) {
	t.Parallel()
	require.True(t, containsNotFoundText("Entity does not refer to any known entity"))
	require.True(t, containsNotFoundText("Function not found"))
	require.False(t, containsNotFoundText("all systems go"))
}

func TestIsBlockedFunctionNotFoundError(t *testing.T) {
	t.Parallel()
	makeHTTPError := func(body string) error {
		return kustoerrors.HTTP(
			kustoerrors.OpQuery,
			"404",
			http.StatusNotFound,
			io.NopCloser(strings.NewReader(body)),
			"prefix",
		)
	}

	t.Run("kusto", func(t *testing.T) {
		err := makeHTTPError(`{"error":{"code":"NotFound"}}`)
		require.True(t, isBlockedFunctionNotFoundError(err))
	})

	t.Run("generic", func(t *testing.T) {
		require.True(t, isBlockedFunctionNotFoundError(fmt.Errorf("Function does not exist")))
		require.False(t, isBlockedFunctionNotFoundError(fmt.Errorf("permission denied")))
	})
}

func TestFilterSchemaByBlockedEndpoints(t *testing.T) {
	t.Parallel()
	schema := map[string][]ADXClusterSchema{
		"https://foo":  {{Database: "db1"}},
		"https://bar/": {{Database: "db2"}},
		"https://baz":  {{Database: "db3"}},
	}
	blocked := map[string]string{
		normalizeEndpoint("https://foo/"): "foo",
		normalizeEndpoint("https://bar"):  "bar",
		normalizeEndpoint("https://qux"):  "qux",
	}

	removed, matched := filterSchemaByBlockedEndpoints(schema, blocked)
	require.Equal(t, 2, removed)
	require.Equal(t, 2, matched)
	require.NotContains(t, schema, "https://foo")
	require.NotContains(t, schema, "https://bar/")
	require.Contains(t, schema, "https://baz")
}

func TestResolveBlockedClusterEndpoints_StaticOnly(t *testing.T) {
	t.Parallel()
	cluster := makeBlockedCluster([]string{" HTTPS://Foo/ ", " bar "}, nil)
	result, err := resolveBlockedClusterEndpoints(context.Background(), nil, cluster)
	require.NoError(t, err)
	require.Len(t, result, 2)
	require.Equal(t, "HTTPS://Foo/", result["https://foo"])
	require.Equal(t, "bar", result["bar"])
}

func TestResolveBlockedClusterEndpoints_FunctionSuccess(t *testing.T) {
	t.Parallel()
	cluster := makeBlockedCluster([]string{"https://static"}, &adxmonv1.ADXClusterFederationBlockedClustersFunctionSpec{
		Database: "db1",
		Name:     "GetBlocked",
	})
	iter := newMockBlockedClusterIterator(t, []blockedClusterRow{
		{ClusterEndpoint: "https://dyn1/"},
		{Endpoint: " https://dyn2 "},
	})
	client := &stubKustoQueryClient{
		t:              t,
		iterator:       iter,
		expectDatabase: "db1",
		expectFunction: "GetBlocked()",
	}
	result, err := resolveBlockedClusterEndpoints(context.Background(), client, cluster)
	require.NoError(t, err)
	require.True(t, client.called)
	require.Len(t, result, 3)
	require.Contains(t, result, "https://static")
	require.Equal(t, "https://dyn1/", result["https://dyn1"])
	require.Contains(t, result, "https://dyn2")
}

func TestResolveBlockedClusterEndpoints_FunctionDedupesStatic(t *testing.T) {
	t.Parallel()
	cluster := makeBlockedCluster([]string{"https://dup"}, &adxmonv1.ADXClusterFederationBlockedClustersFunctionSpec{
		Database: "db1",
		Name:     "GetBlocked",
	})
	iter := newMockBlockedClusterIterator(t, []blockedClusterRow{{ClusterEndpoint: "https://dup/"}})
	client := &stubKustoQueryClient{t: t, iterator: iter}
	result, err := resolveBlockedClusterEndpoints(context.Background(), client, cluster)
	require.NoError(t, err)
	require.True(t, client.called)
	require.Len(t, result, 1)
	require.Equal(t, "https://dup/", result["https://dup"])
}

func TestResolveBlockedClusterEndpoints_FunctionNotFound(t *testing.T) {
	t.Parallel()
	cluster := makeBlockedCluster([]string{"https://static"}, &adxmonv1.ADXClusterFederationBlockedClustersFunctionSpec{
		Database: "db1",
		Name:     "GetBlocked",
	})
	client := &stubKustoQueryClient{t: t, err: fmt.Errorf("function not found")}
	result, err := resolveBlockedClusterEndpoints(context.Background(), client, cluster)
	require.NoError(t, err)
	require.True(t, client.called)
	require.Len(t, result, 1)
	require.Contains(t, result, "https://static")
}

func TestResolveBlockedClusterEndpoints_FunctionError(t *testing.T) {
	t.Parallel()
	cluster := makeBlockedCluster([]string{"https://static"}, &adxmonv1.ADXClusterFederationBlockedClustersFunctionSpec{
		Database: "db1",
		Name:     "GetBlocked",
	})
	client := &stubKustoQueryClient{t: t, err: fmt.Errorf("timeout while executing")}
	result, err := resolveBlockedClusterEndpoints(context.Background(), client, cluster)
	require.Error(t, err)
	require.Nil(t, result)
	require.True(t, client.called)
}

func TestResolveBlockedClusterEndpoints_FunctionRequiresClient(t *testing.T) {
	t.Parallel()
	cluster := makeBlockedCluster([]string{"https://static"}, &adxmonv1.ADXClusterFederationBlockedClustersFunctionSpec{
		Database: "db1",
		Name:     "GetBlocked",
	})
	_, err := resolveBlockedClusterEndpoints(context.Background(), nil, cluster)
	require.Error(t, err)
	require.Contains(t, err.Error(), "kusto client is required")
}

func TestResolveBlockedClusterEndpoints_SkipsWhenFunctionIncomplete(t *testing.T) {
	t.Parallel()
	cluster := makeBlockedCluster([]string{"https://static"}, &adxmonv1.ADXClusterFederationBlockedClustersFunctionSpec{
		Name: "GetBlocked",
	})
	client := &stubKustoQueryClient{t: t}
	result, err := resolveBlockedClusterEndpoints(context.Background(), client, cluster)
	require.NoError(t, err)
	require.False(t, client.called)
	require.Len(t, result, 1)
	require.Contains(t, result, "https://static")
}
