package v1

import (
	"testing"

	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

func TestFunctionSpecFromYAML(t *testing.T) {
	yamlStr := `apiVersion: adx-mon.azure.com/v1
kind: Function
metadata:
  name: prom_increase
  namespace: adx-mon
spec:
  database: Metrics
  body: |
    .create-or-alter function prom_increase (T:(Timestamp:datetime, SeriesId: long, Labels:dynamic, Value:real), interval:timespan=1m) {
    T
    | where isnan(Value)==false
    | extend h=SeriesId
    | partition hint.strategy=shuffle by h (
      as Series
      | order by h, Timestamp asc
      | extend prevVal=prev(Value)
      | extend diff=Value-prevVal
      | extend Value=case(h == prev(h), case(diff < 0, next(Value)-Value, diff), real(0))
      | project-away prevVal, diff, h
    )}`

	var fn Function
	err := yaml.Unmarshal([]byte(yamlStr), &fn)
	require.NoError(t, err)
	require.Equal(t, "Metrics", fn.Spec.Database)
	require.Equal(t, "prom_increase", fn.GetName())
	require.Equal(t, "adx-mon", fn.GetNamespace())
}

func TestFunctionGetCondition(t *testing.T) {
	fn := &Function{}
	fn.Generation = 3
	require.Nil(t, fn.GetCondition())

	fn.SetReconcileCondition(metav1.ConditionTrue, "Test", "ok")
	cond := fn.GetCondition()
	require.NotNil(t, cond)
	require.Equal(t, FunctionReconciled, cond.Type)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, int64(3), cond.ObservedGeneration)
}

func TestFunctionSetReconcileCondition(t *testing.T) {
	fn := &Function{}
	fn.Generation = 5
	fn.SetReconcileCondition(metav1.ConditionFalse, "DatabaseMismatch", "Function skipped")

	cond := meta.FindStatusCondition(fn.Status.Conditions, FunctionReconciled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, "DatabaseMismatch", cond.Reason)
	require.Equal(t, "Function skipped", cond.Message)
	require.Equal(t, int64(5), fn.Status.ObservedGeneration)
	require.False(t, cond.LastTransitionTime.IsZero())

	previousTransition := cond.LastTransitionTime

	fn.SetReconcileCondition(metav1.ConditionTrue, "ReconcileSucceeded", "Created in ADX")
	cond = meta.FindStatusCondition(fn.Status.Conditions, FunctionReconciled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, "ReconcileSucceeded", cond.Reason)
	require.Equal(t, "Created in ADX", cond.Message)
	require.Equal(t, int64(5), cond.ObservedGeneration)
	require.True(t, !cond.LastTransitionTime.Time.Before(previousTransition.Time))
}

func TestFunctionSetDatabaseMatchCondition(t *testing.T) {
	fn := &Function{}
	fn.Generation = 9

	fn.SetDatabaseMatchCondition(false, "ProdDB", "prod, staging")
	cond := meta.FindStatusCondition(fn.Status.Conditions, FunctionDatabaseMatch)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, "DatabaseMismatch", cond.Reason)
	require.Contains(t, cond.Message, "ProdDB")

	fn.SetDatabaseMatchCondition(true, "ProdDB", "prod")
	cond = meta.FindStatusCondition(fn.Status.Conditions, FunctionDatabaseMatch)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, "DatabaseMatched", cond.Reason)

	fn.SetDatabaseMatchCondition(true, AllDatabases, "prod")
	cond = meta.FindStatusCondition(fn.Status.Conditions, FunctionDatabaseMatch)
	require.NotNil(t, cond)
	require.Equal(t, "DatabaseWildcard", cond.Reason)
}

func TestFunctionSetCriteriaMatchCondition(t *testing.T) {
	labels := map[string]string{"region": "eastus", "environment": "prod"}

	fn := &Function{}
	fn.Generation = 7
	fn.SetCriteriaMatchCondition(true, "env == 'prod'", nil, labels)
	cond := meta.FindStatusCondition(fn.Status.Conditions, FunctionCriteriaMatch)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, ReasonCriteriaMatched, cond.Reason)
	require.Contains(t, cond.Message, "env == 'prod'")
	require.Contains(t, cond.Message, "environment=prod")

	fn.SetCriteriaMatchCondition(false, "env == 'prod'", nil, labels)
	cond = meta.FindStatusCondition(fn.Status.Conditions, FunctionCriteriaMatch)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, ReasonCriteriaNotMatched, cond.Reason)

	fn.SetCriteriaMatchCondition(false, "env == 'prod'", assertiveErr("parse error"), labels)
	cond = meta.FindStatusCondition(fn.Status.Conditions, FunctionCriteriaMatch)
	require.NotNil(t, cond)
	require.Equal(t, ReasonCriteriaExpressionError, cond.Reason)
	require.Contains(t, cond.Message, "parse error")
}

func TestFunctionMultipleConditionsCoexist(t *testing.T) {
	fn := &Function{}
	fn.Generation = 2
	labels := map[string]string{"region": "westus"}

	fn.SetDatabaseMatchCondition(true, "Prod", "prod")
	fn.SetCriteriaMatchCondition(true, "region == 'westus'", nil, labels)
	fn.SetReconcileCondition(metav1.ConditionTrue, "ReconcileSucceeded", "Created")

	dbCond := meta.FindStatusCondition(fn.Status.Conditions, FunctionDatabaseMatch)
	critCond := meta.FindStatusCondition(fn.Status.Conditions, FunctionCriteriaMatch)
	recCond := meta.FindStatusCondition(fn.Status.Conditions, FunctionReconciled)

	require.NotNil(t, dbCond)
	require.NotNil(t, critCond)
	require.NotNil(t, recCond)
	require.Equal(t, metav1.ConditionTrue, dbCond.Status)
	require.Equal(t, metav1.ConditionTrue, critCond.Status)
	require.Equal(t, metav1.ConditionTrue, recCond.Status)
}

type assertiveErr string

func (a assertiveErr) Error() string {
	return string(a)
}
