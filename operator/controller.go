package operator

import (
	"context"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Reconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var operator adxmonv1.Operator
	err := r.Get(ctx, req.NamespacedName, &operator)
	if err == nil {
		// This is an Operator CRD event
		return handleOperatorEvent(ctx, r, &operator)
	}

	// Try DaemonSet
	var ds appsv1.DaemonSet
	if err := r.Get(ctx, req.NamespacedName, &ds); err == nil {
		return handleDaemonSetEvent(ctx, r, &ds)
	}

	// Try StatefulSet
	var sts appsv1.StatefulSet
	if err := r.Get(ctx, req.NamespacedName, &sts); err == nil {
		return handleStatefulSetEvent(ctx, r, &sts, nil)
	}

	// Try Deployment
	var deploy appsv1.Deployment
	if err := r.Get(ctx, req.NamespacedName, &deploy); err == nil {
		return handleDeploymentEvent(ctx, r, &deploy)
	}

	// Try ConfigMap
	var cm corev1.ConfigMap
	if err := r.Get(ctx, req.NamespacedName, &cm); err == nil {
		return handleConfigMapEvent(ctx, r, &cm)
	}

	// Unknown resource type or not found
	return ctrl.Result{}, nil
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&adxmonv1.Operator{}).
		Owns(&appsv1.DaemonSet{}).   // collector
		Owns(&corev1.ConfigMap{}).   // collector
		Owns(&appsv1.StatefulSet{}). // ingestor
		Owns(&appsv1.Deployment{}).  // alerter
		Complete(r)
}
