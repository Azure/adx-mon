/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"errors"
	"time"

	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/azure-kusto-go/kusto/kql"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
)

// FunctionReconciler reconciles a Function object
type FunctionReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	KustoClis []metrics.StatementExecutor
}

const (
	// RetryAfter is the time to wait before retrying a failed Kusto operation
	RetryAfter = 10 * time.Minute
)

// +kubebuilder:rbac:groups=adx-mon.azure.com,resources=functions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=adx-mon.azure.com,resources=functions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=adx-mon.azure.com,resources=functions/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *FunctionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	function := &adxmonv1.Function{}
	if err := r.Get(ctx, req.NamespacedName, function); err != nil {
		log.Error(err, "unable to fetch function")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if function.Spec.Suspend != nil && *function.Spec.Suspend {
		// The object is suspended, so we don't need to do anything
		return ctrl.Result{}, nil
	}

	// name of our custom finalizer
	finalizerName := "function.adx-mon.azure.com/finalizer"

	// examine DeletionTimestamp to determine if object is under deletion
	if function.ObjectMeta.DeletionTimestamp.IsZero() {
		// The object is not being deleted, so if it does not have our finalizer,
		// then lets add the finalizer and update the object. This is equivalent
		// to registering our finalizer.
		if !controllerutil.ContainsFinalizer(function, finalizerName) {
			controllerutil.AddFinalizer(function, finalizerName)
			if err := r.Update(ctx, function); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		// The object is being deleted
		if controllerutil.ContainsFinalizer(function, finalizerName) {

			if err := r.ReconcileKusto(ctx, function); err != nil {
				function.Status = adxmonv1.FunctionStatus{
					LastTimeReconciled: metav1.Now(),
					Message:            "Failed to delete function in Kusto cluster",
					// TODO set error from kusto
					Error:  "",
					Status: adxmonv1.Failed,
				}
				if err := r.Status().Update(ctx, function); err != nil {
					log.Error(err, "unable to update Function status")
				}
				return ctrl.Result{Requeue: true, RequeueAfter: RetryAfter}, nil
			}

			// remove our finalizer from the list and update it.
			controllerutil.RemoveFinalizer(function, finalizerName)
			if err := r.Update(ctx, function); err != nil {
				return ctrl.Result{}, err
			}
		}

		// Stop reconciliation as the item is being deleted
		return ctrl.Result{}, nil
	}

	// If the ObservedGeneration does not match the current generation, we need to reconcile
	// the function, the CRD has been updated.
	if function.Status.Status == adxmonv1.Success && function.Status.ObservedGeneration == function.GetGeneration() {
		// Nothing for us to do, the function has already been reconciled
		return ctrl.Result{}, nil
	}

	// TODO KQL validation. If invalid, set the status as being a permanent failure
	// and don't bother requeuing the function.

	if err := r.ReconcileKusto(ctx, function); err != nil {
		function.Status = adxmonv1.FunctionStatus{
			LastTimeReconciled: metav1.Now(),
			Message:            "Failed to delete function in Kusto cluster",
			// TODO set error from kusto
			Error:  "",
			Status: adxmonv1.Failed,
		}
		if err := r.Status().Update(ctx, function); err != nil {
			log.Error(err, "unable to update Function status")
		}
		return ctrl.Result{Requeue: true, RequeueAfter: RetryAfter}, nil
	}

	function.Status = adxmonv1.FunctionStatus{
		ObservedGeneration: function.GetGeneration(),
		LastTimeReconciled: metav1.Now(),
		Message:            "Function reconciled successfully",
		Status:             adxmonv1.Success,
	}
	if err := r.Status().Update(ctx, function); err != nil {
		log.Error(err, "unable to update Function status")
	}

	return ctrl.Result{}, nil
}

func (r *FunctionReconciler) ReconcileKusto(ctx context.Context, fn *adxmonv1.Function) error {

	var stmt *kql.Builder
	if fn.ObjectMeta.DeletionTimestamp.IsZero() {
		stmt = kql.New("").AddUnsafe(fn.Spec.Body)
	} else {
		stmt = kql.New(".drop function ").AddUnsafe(fn.GetName()).AddLiteral(" ifexists")
	}

	for _, kustoCli := range r.KustoClis {
		if kustoCli.Database() == fn.Spec.Database {
			_, err := kustoCli.Mgmt(ctx, stmt)
			return err
		}
	}

	return errors.New("no matching database found")
}

var (
	fnOwnerKey = ".metadata.controller"
	apiGVStr   = adxmonv1.GroupVersion.String()
)

// SetupWithManager sets up the controller with the Manager.
func (r *FunctionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &adxmonv1.Function{}, fnOwnerKey, func(rawObj client.Object) []string {
		function := rawObj.(*adxmonv1.Function)
		owner := metav1.GetControllerOf(function)
		if owner == nil {
			return nil
		}
		// ...make sure it's a Function...
		if owner.APIVersion != apiGVStr || owner.Kind != "Function" {
			return nil
		}

		// ...and if so, return it
		return []string{owner.Name}
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&adxmonv1.Function{}).
		Complete(r)
}
