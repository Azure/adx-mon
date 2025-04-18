package operator

import (
	"context"
	"time"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

func handleAdxEvent(ctx context.Context, r *Reconciler, operator *adxmonv1.Operator) (ctrl.Result, error) {
	if clustersAreDone(operator) {
		c := metav1.Condition{
			Type:               adxmonv1.ADXClusterConditionOwner,
			Status:             metav1.ConditionTrue,
			Reason:             PhaseEnsureKusto.String(),
			Message:            "All Kusto clusters are ready",
			LastTransitionTime: metav1.NewTime(time.Now()),
		}
		if err := updateCondition(r, ctx, operator, c); err != nil {
			return ctrl.Result{Requeue: true}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// TODO Ensure ASO Kusto CRD is created, check its status

	c := metav1.Condition{
		Type:               adxmonv1.ADXClusterConditionOwner,
		Status:             metav1.ConditionUnknown,
		Reason:             PhaseEnsureKusto.String(),
		Message:            "Ensuring Kusto cluster and database via ASO",
		LastTransitionTime: metav1.NewTime(time.Now()),
	}
	_ = updateCondition(r, ctx, operator, c)
	return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, nil
}
