package v1

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

func TestSummaryRulesSpecFromYAML(t *testing.T) {
	yamlStr := `apiVersion: adx-mon.azure.com/v1
kind: SummaryRule
metadata:
  name: HourlyAvg
  namespace: adx-mon
spec:
  database: Metrics
  body: |
    SomeMetric
    | where Timestamp between (_startTime .. _endtime)
    | summarize avg(Value) by bin(Timestamp, 1h)
  table: SomeMetricHourlyAvg
  interval: 1h`

	var sr SummaryRule
	err := yaml.Unmarshal([]byte(yamlStr), &sr)
	require.NoError(t, err)
	require.Equal(t, "Metrics", sr.Spec.Database)
	require.Equal(t, "HourlyAvg", sr.GetName())
	require.Equal(t, "adx-mon", sr.GetNamespace())
	require.Equal(t, "SomeMetricHourlyAvg", sr.Spec.Table)
	require.Equal(t, metav1.Duration{Duration: time.Hour}, sr.Spec.Interval)
}

func TestAsyncOperations(t *testing.T) {
	var sr SummaryRule

	// We expect the initial AsyncOperation to be empty
	ops := sr.GetAsyncOperations()
	require.Equal(t, 0, len(ops))

	// Create an AsyncOperation, get it, and validate its contents
	sr.SetAsyncOperation(AsyncOperation{OperationId: "a"})
	ops = sr.GetAsyncOperations()
	require.Equal(t, 1, len(ops))
	require.Equal(t, "a", ops[0].OperationId)
	c := meta.FindStatusCondition(sr.Status.Conditions, SummaryRuleOperationIdOwner)
	require.NotNil(t, c)
	require.Equal(t, metav1.ConditionUnknown, c.Status)
	require.Equal(t, SummaryRuleOperationIdOwner, c.Type)
	require.NotZero(t, c.LastTransitionTime)

	// Create a second AsyncOperation, get it, and validate its contents
	sr.SetAsyncOperation(AsyncOperation{OperationId: "b"})
	ops = sr.GetAsyncOperations()
	require.Equal(t, 2, len(ops))
	require.Equal(t, "b", ops[1].OperationId)
	c = meta.FindStatusCondition(sr.Status.Conditions, SummaryRuleOperationIdOwner)
	require.NotNil(t, c)
	require.Equal(t, metav1.ConditionUnknown, c.Status)
	require.Equal(t, SummaryRuleOperationIdOwner, c.Type)
	require.NotZero(t, c.LastTransitionTime)

	// Remove the first AsyncOperation, get it, and validate its contents
	sr.RemoveAsyncOperation("a")
	ops = sr.GetAsyncOperations()
	require.Equal(t, 1, len(ops))
	require.Equal(t, "b", ops[0].OperationId)
	c = meta.FindStatusCondition(sr.Status.Conditions, SummaryRuleOperationIdOwner)
	require.NotNil(t, c)
	require.Equal(t, metav1.ConditionUnknown, c.Status)
	require.Equal(t, SummaryRuleOperationIdOwner, c.Type)
	require.NotZero(t, c.LastTransitionTime)

	// Remove the second AsyncOperation, get it, and validate its contents
	sr.RemoveAsyncOperation("b")
	ops = sr.GetAsyncOperations()
	require.Equal(t, 0, len(ops))
	c = meta.FindStatusCondition(sr.Status.Conditions, SummaryRuleOperationIdOwner)
	require.NotNil(t, c)
	require.Equal(t, metav1.ConditionTrue, c.Status)
	require.Equal(t, SummaryRuleOperationIdOwner, c.Type)
	require.NotZero(t, c.LastTransitionTime)

	// Create a thousand AsyncOperations, get them, ensure the array is of size 200, which is our max
	for i := 0; i < 1000; i++ {
		sr.SetAsyncOperation(AsyncOperation{OperationId: strconv.Itoa(i)})
	}
	ops = sr.GetAsyncOperations()
	require.Equal(t, 200, len(ops))
}
