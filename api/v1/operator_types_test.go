package v1

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

func TestOperatorSpecFromYAML(t *testing.T) {
	yamlStr := `apiVersion: adx-mon.azure.com/v1
kind: Operator
metadata:
  name: test-operator
  namespace: adx-mon
spec:
  adx:
    clusters:
      - name: test-adx
        endpoint: https://adx.example.com
        connection:
          type: MSI
          clientId: test-client
        databases:
          - name: Metrics
            telemetryType: Metrics
  ingestor:
    image: ingestor:latest
    replicas: 2
  collector:
    image: collector:latest
    ingestorEndpoint: http://ingestor:8080
    ingestorAuth:
      type: token
      tokenSecretRef:
        name: my-secret
        key: token
  alerter:
    image: alerter:latest
`

	var op Operator
	err := yaml.Unmarshal([]byte(yamlStr), &op)
	require.NoError(t, err)
	require.Equal(t, "test-operator", op.GetName())
	require.Equal(t, "adx-mon", op.GetNamespace())
	require.NotNil(t, op.Spec.ADX)
	require.Len(t, op.Spec.ADX.Clusters, 1)
	require.Equal(t, "test-adx", op.Spec.ADX.Clusters[0].Name)
	require.Equal(t, "https://adx.example.com", op.Spec.ADX.Clusters[0].Endpoint)
	require.Equal(t, "MSI", op.Spec.ADX.Clusters[0].Connection.Type)
	require.Equal(t, "test-client", op.Spec.ADX.Clusters[0].Connection.ClientId)
	require.Len(t, op.Spec.ADX.Clusters[0].Databases, 1)
	require.Equal(t, "Metrics", op.Spec.ADX.Clusters[0].Databases[0].Name)
	require.Equal(t, DatabaseTelemetryMetrics, op.Spec.ADX.Clusters[0].Databases[0].TelemetryType)
	require.NotNil(t, op.Spec.Ingestor)
	require.Equal(t, "ingestor:latest", op.Spec.Ingestor.Image)
	require.NotNil(t, op.Spec.Ingestor.Replicas)
	require.Equal(t, int32(2), *op.Spec.Ingestor.Replicas)
	require.NotNil(t, op.Spec.Collector)
	require.Equal(t, "collector:latest", op.Spec.Collector.Image)
	require.Equal(t, "http://ingestor:8080", op.Spec.Collector.IngestorEndpoint)
	require.NotNil(t, op.Spec.Collector.IngestorAuth)
	require.Equal(t, "token", op.Spec.Collector.IngestorAuth.Type)
	require.NotNil(t, op.Spec.Collector.IngestorAuth.TokenSecretRef)
	require.Equal(t, "my-secret", op.Spec.Collector.IngestorAuth.TokenSecretRef.Name)
	require.Equal(t, "token", op.Spec.Collector.IngestorAuth.TokenSecretRef.Key)
	require.NotNil(t, op.Spec.Alerter)
	require.Equal(t, "alerter:latest", op.Spec.Alerter.Image)
}

func TestOperatorStatusConditions(t *testing.T) {
	op := Operator{}
	require.Empty(t, op.Status.Conditions)

	cond := metav1.Condition{
		Type:               OperatorCommandConditionOwner,
		Status:             metav1.ConditionTrue,
		Reason:             "Test",
		Message:            "Test message",
		ObservedGeneration: 1,
	}
	op.Status.Conditions = append(op.Status.Conditions, cond)
	require.Len(t, op.Status.Conditions, 1)
	require.Equal(t, OperatorCommandConditionOwner, op.Status.Conditions[0].Type)
	require.Equal(t, metav1.ConditionTrue, op.Status.Conditions[0].Status)
}
