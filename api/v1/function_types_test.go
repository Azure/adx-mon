package v1

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func TestFunction_GetCondition(t *testing.T) {
	t.Run("returns nil when no conditions exist", func(t *testing.T) {
		fn := &Function{}
		condition := fn.GetCondition()
		require.Nil(t, condition)
	})

	t.Run("returns correct condition when it exists", func(t *testing.T) {
		fn := &Function{
			Status: FunctionStatus{
				Conditions: []metav1.Condition{
					{
						Type:   FunctionReconciled,
						Status: metav1.ConditionTrue,
						Reason: "ReconcileSucceeded",
					},
				},
			},
		}
		condition := fn.GetCondition()
		require.NotNil(t, condition)
		require.Equal(t, FunctionReconciled, condition.Type)
		require.Equal(t, metav1.ConditionTrue, condition.Status)
	})

	t.Run("returns correct condition when multiple conditions exist", func(t *testing.T) {
		fn := &Function{
			Status: FunctionStatus{
				Conditions: []metav1.Condition{
					{
						Type:   FunctionDatabaseMatch,
						Status: metav1.ConditionTrue,
					},
					{
						Type:   FunctionReconciled,
						Status: metav1.ConditionFalse,
						Reason: "ReconcileFailed",
					},
					{
						Type:   FunctionCriteriaMatch,
						Status: metav1.ConditionTrue,
					},
				},
			},
		}
		condition := fn.GetCondition()
		require.NotNil(t, condition)
		require.Equal(t, FunctionReconciled, condition.Type)
		require.Equal(t, metav1.ConditionFalse, condition.Status)
	})
}

func TestFunction_SetCondition(t *testing.T) {
	t.Run("creates new condition", func(t *testing.T) {
		fn := &Function{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
		}
		fn.SetCondition(metav1.ConditionTrue, "ReconcileSucceeded", "Function created successfully")
		
		require.Len(t, fn.Status.Conditions, 1)
		condition := fn.GetCondition()
		require.NotNil(t, condition)
		require.Equal(t, FunctionReconciled, condition.Type)
		require.Equal(t, metav1.ConditionTrue, condition.Status)
		require.Equal(t, "ReconcileSucceeded", condition.Reason)
		require.Equal(t, "Function created successfully", condition.Message)
		require.Equal(t, int64(1), condition.ObservedGeneration)
		require.False(t, condition.LastTransitionTime.IsZero())
	})

	t.Run("updates existing condition", func(t *testing.T) {
		fn := &Function{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 2,
			},
			Status: FunctionStatus{
				Conditions: []metav1.Condition{
					{
						Type:               FunctionReconciled,
						Status:             metav1.ConditionTrue,
						Reason:             "ReconcileSucceeded",
						ObservedGeneration: 1,
					},
				},
			},
		}
		fn.SetCondition(metav1.ConditionFalse, "ReconcileFailed", "Kusto error")
		
		require.Len(t, fn.Status.Conditions, 1)
		condition := fn.GetCondition()
		require.NotNil(t, condition)
		require.Equal(t, metav1.ConditionFalse, condition.Status)
		require.Equal(t, "ReconcileFailed", condition.Reason)
		require.Equal(t, "Kusto error", condition.Message)
		require.Equal(t, int64(2), condition.ObservedGeneration)
	})
}

func TestFunction_SetDatabaseMatchCondition(t *testing.T) {
	t.Run("sets matched condition", func(t *testing.T) {
		fn := &Function{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
		}
		fn.SetDatabaseMatchCondition(true, "Metrics", "Metrics")
		
		require.Len(t, fn.Status.Conditions, 1)
		condition := fn.Status.Conditions[0]
		require.Equal(t, FunctionDatabaseMatch, condition.Type)
		require.Equal(t, metav1.ConditionTrue, condition.Status)
		require.Equal(t, "DatabaseMatched", condition.Reason)
		require.Contains(t, condition.Message, "Metrics")
		require.Contains(t, condition.Message, "matches endpoint database")
		require.Equal(t, int64(1), condition.ObservedGeneration)
	})

	t.Run("sets mismatch condition", func(t *testing.T) {
		fn := &Function{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
		}
		fn.SetDatabaseMatchCondition(false, "AKSProd", "AKSprod")
		
		require.Len(t, fn.Status.Conditions, 1)
		condition := fn.Status.Conditions[0]
		require.Equal(t, FunctionDatabaseMatch, condition.Type)
		require.Equal(t, metav1.ConditionFalse, condition.Status)
		require.Equal(t, "DatabaseMismatch", condition.Reason)
		require.Contains(t, condition.Message, "AKSProd")
		require.Contains(t, condition.Message, "AKSprod")
		require.Contains(t, condition.Message, "does not match")
		require.Equal(t, int64(1), condition.ObservedGeneration)
	})
}

func TestFunction_SetCriteriaMatchCondition(t *testing.T) {
	t.Run("sets matched condition with expression", func(t *testing.T) {
		fn := &Function{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
		}
		fn.SetCriteriaMatchCondition(true, "region == 'eastus'", nil)
		
		require.Len(t, fn.Status.Conditions, 1)
		condition := fn.Status.Conditions[0]
		require.Equal(t, FunctionCriteriaMatch, condition.Type)
		require.Equal(t, metav1.ConditionTrue, condition.Status)
		require.Equal(t, "CriteriaMatched", condition.Reason)
		require.Contains(t, condition.Message, "region == 'eastus'")
		require.Contains(t, condition.Message, "evaluated to true")
		require.Equal(t, int64(1), condition.ObservedGeneration)
	})

	t.Run("sets matched condition without expression", func(t *testing.T) {
		fn := &Function{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
		}
		fn.SetCriteriaMatchCondition(true, "", nil)
		
		require.Len(t, fn.Status.Conditions, 1)
		condition := fn.Status.Conditions[0]
		require.Equal(t, FunctionCriteriaMatch, condition.Type)
		require.Equal(t, metav1.ConditionTrue, condition.Status)
		require.Equal(t, "CriteriaMatched", condition.Reason)
		require.Contains(t, condition.Message, "No criteria expression specified")
		require.Equal(t, int64(1), condition.ObservedGeneration)
	})

	t.Run("sets not matched condition", func(t *testing.T) {
		fn := &Function{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
		}
		fn.SetCriteriaMatchCondition(false, "region == 'westus'", nil)
		
		require.Len(t, fn.Status.Conditions, 1)
		condition := fn.Status.Conditions[0]
		require.Equal(t, FunctionCriteriaMatch, condition.Type)
		require.Equal(t, metav1.ConditionFalse, condition.Status)
		require.Equal(t, "CriteriaNotMatched", condition.Reason)
		require.Contains(t, condition.Message, "region == 'westus'")
		require.Contains(t, condition.Message, "evaluated to false")
		require.Equal(t, int64(1), condition.ObservedGeneration)
	})

	t.Run("sets error condition", func(t *testing.T) {
		fn := &Function{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
		}
		testErr := errors.New("parse error: invalid syntax")
		fn.SetCriteriaMatchCondition(false, "region =! 'eastus'", testErr)
		
		require.Len(t, fn.Status.Conditions, 1)
		condition := fn.Status.Conditions[0]
		require.Equal(t, FunctionCriteriaMatch, condition.Type)
		require.Equal(t, metav1.ConditionFalse, condition.Status)
		require.Equal(t, "CriteriaExpressionError", condition.Reason)
		require.Contains(t, condition.Message, "evaluation failed")
		require.Contains(t, condition.Message, "parse error")
		require.Equal(t, int64(1), condition.ObservedGeneration)
	})
}

func TestFunction_MultipleConditions(t *testing.T) {
	t.Run("can set and retrieve multiple conditions", func(t *testing.T) {
		fn := &Function{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
		}
		
		fn.SetDatabaseMatchCondition(true, "Metrics", "Metrics")
		fn.SetCriteriaMatchCondition(true, "region == 'eastus'", nil)
		fn.SetCondition(metav1.ConditionTrue, "ReconcileSucceeded", "Function created")
		
		require.Len(t, fn.Status.Conditions, 3)
		
		// Verify each condition type exists
		dbCondition := fn.Status.Conditions[0]
		require.Equal(t, FunctionDatabaseMatch, dbCondition.Type)
		require.Equal(t, metav1.ConditionTrue, dbCondition.Status)
		
		criteriaCondition := fn.Status.Conditions[1]
		require.Equal(t, FunctionCriteriaMatch, criteriaCondition.Type)
		require.Equal(t, metav1.ConditionTrue, criteriaCondition.Status)
		
		reconciledCondition := fn.GetCondition()
		require.NotNil(t, reconciledCondition)
		require.Equal(t, FunctionReconciled, reconciledCondition.Type)
		require.Equal(t, metav1.ConditionTrue, reconciledCondition.Status)
	})
}
