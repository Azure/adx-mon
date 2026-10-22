package v1

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/yaml"
)

func TestADXClusterFederatedClusterSpecJSON(t *testing.T) {
	for _, tt := range []struct {
		name     string
		clientID string
		json     string
	}{
		{
			name: "without managed identity",
			json: `{"endpoint":"https://hub.kusto.windows.net","heartbeatDatabase":"Heartbeat","heartbeatTable":"Partitions"}`,
		},
		{
			name:     "with managed identity",
			clientID: "00000000-0000-0000-0000-000000000001",
			json:     `{"endpoint":"https://hub.kusto.windows.net","heartbeatDatabase":"Heartbeat","heartbeatTable":"Partitions","managedIdentityClientId":"00000000-0000-0000-0000-000000000001"}`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			target := ADXClusterFederatedClusterSpec{
				Endpoint:                "https://hub.kusto.windows.net",
				HeartbeatDatabase:       "Heartbeat",
				HeartbeatTable:          "Partitions",
				ManagedIdentityClientId: tt.clientID,
			}
			data, err := json.Marshal(target)
			require.NoError(t, err)
			require.JSONEq(t, tt.json, string(data))

			var decoded ADXClusterFederatedClusterSpec
			require.NoError(t, json.Unmarshal([]byte(tt.json), &decoded))
			require.Equal(t, target, decoded)
		})
	}
}

func TestADXClusterFederatedClusterSpecCRDSchema(t *testing.T) {
	for _, path := range []string{
		"../../kustomize/bases/adxclusters_crd.yaml",
		"../../operator/manifests/crds/adxclusters_crd.yaml",
	} {
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(path)
			require.NoError(t, err)

			var crd apiextensionsv1.CustomResourceDefinition
			require.NoError(t, yaml.Unmarshal(data, &crd))
			require.NotEmpty(t, crd.Spec.Versions)
			for _, version := range crd.Spec.Versions {
				require.NotNil(t, version.Schema)
				require.NotNil(t, version.Schema.OpenAPIV3Schema)
				targets := version.Schema.OpenAPIV3Schema.Properties["spec"].Properties["federation"].Properties["federatedTargets"]
				require.NotNil(t, targets.Items)
				require.NotNil(t, targets.Items.Schema)
				target := targets.Items.Schema
				require.ElementsMatch(t, []string{"endpoint", "heartbeatDatabase", "heartbeatTable"}, target.Required)
				require.Equal(t, "string", target.Properties["managedIdentityClientId"].Type)
			}
		})
	}
}
