package v1

import (
	"testing"

	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stretchr/testify/require"
)

func TestManagementCommandSetConditionDefaults(t *testing.T) {
	cmd := &ManagementCommand{}
	cmd.Generation = 4

	cmd.SetCondition(metav1.Condition{Status: metav1.ConditionTrue})
	owner := meta.FindStatusCondition(cmd.Status.Conditions, ManagementCommandConditionOwner)
	require.NotNil(t, owner)
	require.Equal(t, metav1.ConditionTrue, owner.Status)
	require.Equal(t, ConditionCompleted, owner.Reason)
	require.Equal(t, int64(4), owner.ObservedGeneration)

	// Simulate UpdateStatus call with empty reason/message should preserve existing reason/message
	ownerReason := "custom"
	ownerMessage := "custom message"
	cmd.SetExecutionCondition(metav1.ConditionFalse, ownerReason, ownerMessage)

	cmd.SetCondition(metav1.Condition{Status: metav1.ConditionFalse})
	owner = meta.FindStatusCondition(cmd.Status.Conditions, ManagementCommandConditionOwner)
	require.Equal(t, ownerReason, owner.Reason)
	require.Equal(t, ownerMessage, owner.Message)
}

func TestManagementCommandCriteriaMatchCondition(t *testing.T) {
	labels := map[string]string{"environment": "prod"}
	cmd := &ManagementCommand{}
	cmd.Generation = 2

	cmd.SetCriteriaMatchCondition(true, "environment == 'prod'", nil, labels)
	crit := meta.FindStatusCondition(cmd.Status.Conditions, ManagementCommandCriteriaMatch)
	require.NotNil(t, crit)
	require.Equal(t, metav1.ConditionTrue, crit.Status)
	require.Equal(t, ReasonCriteriaMatched, crit.Reason)

	cmd.SetCriteriaMatchCondition(false, "environment == 'prod'", assertiveErr("boom"), labels)
	crit = meta.FindStatusCondition(cmd.Status.Conditions, ManagementCommandCriteriaMatch)
	require.NotNil(t, crit)
	require.Equal(t, metav1.ConditionFalse, crit.Status)
	require.Equal(t, ReasonCriteriaExpressionError, crit.Reason)
	require.Contains(t, crit.Message, "boom")
}

func TestManagementCommandExecutionCondition(t *testing.T) {
	cmd := &ManagementCommand{}
	cmd.Generation = 10

	cmd.SetExecutionCondition(metav1.ConditionFalse, ReasonManagementCommandExecutionFailed, "failed")
	exec := meta.FindStatusCondition(cmd.Status.Conditions, ManagementCommandExecuted)
	require.NotNil(t, exec)
	require.Equal(t, metav1.ConditionFalse, exec.Status)
	require.Equal(t, ReasonManagementCommandExecutionFailed, exec.Reason)
	require.Equal(t, "failed", exec.Message)

	owner := meta.FindStatusCondition(cmd.Status.Conditions, ManagementCommandConditionOwner)
	require.NotNil(t, owner)
	require.Equal(t, metav1.ConditionFalse, owner.Status)
	require.Equal(t, ReasonManagementCommandExecutionFailed, owner.Reason)
	require.Equal(t, "failed", owner.Message)

	cmd.SetExecutionCondition(metav1.ConditionTrue, ReasonManagementCommandExecutionSucceeded, "ok")
	exec = meta.FindStatusCondition(cmd.Status.Conditions, ManagementCommandExecuted)
	require.Equal(t, metav1.ConditionTrue, exec.Status)
	require.Equal(t, ReasonManagementCommandExecutionSucceeded, exec.Reason)
	require.Equal(t, "ok", exec.Message)

	owner = meta.FindStatusCondition(cmd.Status.Conditions, ManagementCommandConditionOwner)
	require.Equal(t, metav1.ConditionTrue, owner.Status)
	require.Equal(t, ReasonManagementCommandExecutionSucceeded, owner.Reason)
}
