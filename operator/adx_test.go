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
