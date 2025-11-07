package v1

import (
	"errors"
	"testing"

	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stretchr/testify/require"
)

func TestFormatClusterLabels(t *testing.T) {
	labels := map[string]string{"environment": "prod", "region": "eastus"}
	formatted := FormatClusterLabels(labels)
	require.Equal(t, "environment=prod, region=eastus", formatted)

	require.Equal(t, "(none)", FormatClusterLabels(nil))
}

func TestNewCriteriaCondition(t *testing.T) {
	labels := map[string]string{"region": "westus"}

	cond := NewCriteriaCondition("test", 7, true, "region == 'westus'", nil, labels)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, ReasonCriteriaMatched, cond.Reason)
	require.Contains(t, cond.Message, "region == 'westus'")
	require.Equal(t, int64(7), cond.ObservedGeneration)

	cond = NewCriteriaCondition("test", 7, false, "region == 'westus'", nil, labels)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, ReasonCriteriaNotMatched, cond.Reason)

	cond = NewCriteriaCondition("test", 7, false, "", nil, labels)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, ReasonCriteriaMatched, cond.Reason)
	require.Equal(t, "No criteria expression configured; defaulting to match", cond.Message)

	cond = NewCriteriaCondition("test", 7, false, "region == 'westus'", errors.New("parse"), labels)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, ReasonCriteriaExpressionError, cond.Reason)
	require.Contains(t, cond.Message, "parse")
}

func TestSetOwnerConditionHelper(t *testing.T) {
	fn := &Function{}
	fn.Generation = 3

	SetOwnerCondition(fn, "demo", metav1.ConditionTrue, "", "completed")

	cond := meta.FindStatusCondition(fn.Status.Conditions, "demo")
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, ConditionCompleted, cond.Reason)
	require.Equal(t, "completed", cond.Message)
	require.Equal(t, int64(3), cond.ObservedGeneration)
}
