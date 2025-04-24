package operator

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"time"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/logger"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//go:embed manifests/*.yaml manifests/**/*.yaml
var manifestsFS embed.FS

type Reconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	AdxCtor   AdxClusterCreator
	AdxUpdate AdxClusterCreator
	AdxRdy    AdxClusterReady
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Try to get the Operator CRD
	var operator adxmonv1.Operator
	err := r.Get(ctx, req.NamespacedName, &operator)
	if err == nil {
		// This is an Operator CRD event
		return OrchestrateResources(ctx, r, &operator)
	}
	// Try to get the StatefulSet (ingestor)
	var sts appsv1.StatefulSet
	err = r.Get(ctx, req.NamespacedName, &sts)
	if err == nil && sts.Name == IngestorHandler.Name {
		// This is an Ingestor StatefulSet event
		return IngestorHandler.HandleEvent(ctx, r, &sts, nil)
	}

	// Try to get the DaemonSet (collector)
	var ds appsv1.DaemonSet
	err = r.Get(ctx, req.NamespacedName, &ds)
	if err == nil && ds.Name == CollectorHandler.Name {
		// This is a Collector DaemonSet event
		return CollectorHandler.HandleEvent(ctx, r, &ds, nil)
	}

	return ctrl.Result{}, client.IgnoreNotFound(err)
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.AdxCtor == nil {
		r.AdxCtor = CreateAdxCluster
	}
	if r.AdxUpdate == nil {
		r.AdxUpdate = EnsureAdxClusterConfiguration
	}
	if r.AdxRdy == nil {
		r.AdxRdy = ArmAdxReady
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&adxmonv1.Operator{}).
		Owns(&appsv1.DaemonSet{}).   // collector
		Owns(&appsv1.StatefulSet{}). // ingestor
		Owns(&appsv1.Deployment{}).  // alerter
		Complete(r)
}

func OrchestrateResources(ctx context.Context, r *Reconciler, operator *adxmonv1.Operator) (ctrl.Result, error) {

	// Initial bootstrapping phase where we ensure our CRDs are installed.
	if !meta.IsStatusConditionTrue(operator.Status.Conditions, adxmonv1.InitConditionOwner) {
		return handleInitEvent(ctx, r, operator)
	}

	// Get ADX provisioned and configured
	if !meta.IsStatusConditionTrue(operator.Status.Conditions, adxmonv1.ADXClusterConditionOwner) {
		return handleAdxEvent(ctx, r, operator)
	}

	// Install Ingestor and wait for it to be ready
	if !meta.IsStatusConditionTrue(operator.Status.Conditions, adxmonv1.IngestorClusterConditionOwner) {
		return IngestorHandler.HandleEvent(ctx, r, nil, operator)
	}

	// Install Collector and wait for it to be ready
	if !meta.IsStatusConditionTrue(operator.Status.Conditions, adxmonv1.CollectorClusterConditionOwner) {
		return CollectorHandler.HandleEvent(ctx, r, nil, operator)
	}

	c := metav1.Condition{
		Type:               adxmonv1.OperatorCommandConditionOwner,
		Status:             metav1.ConditionTrue,
		Reason:             string(adxmonv1.OperatorServiceReasonInstalled),
		LastTransitionTime: metav1.NewTime(time.Now()),
		ObservedGeneration: operator.GetGeneration(),
	}
	if meta.SetStatusCondition(&operator.Status.Conditions, c) {
		if err := r.Status().Update(ctx, operator); err != nil {
			logger.Errorf("Failed to update status: %v", err)
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func handleInitEvent(ctx context.Context, r *Reconciler, operator *adxmonv1.Operator) (ctrl.Result, error) {
	// Initial condition, begin by installing CRDs
	if err := InstallCrds(ctx, r); err != nil {
		return ctrl.Result{}, err
	}
	c := metav1.Condition{
		Type:               adxmonv1.InitConditionOwner,
		Status:             metav1.ConditionTrue,
		Reason:             string(adxmonv1.OperatorServiceReasonInstalled),
		LastTransitionTime: metav1.NewTime(time.Now()),
		ObservedGeneration: operator.GetGeneration(),
	}
	if meta.SetStatusCondition(&operator.Status.Conditions, c) {
		if err := r.Status().Update(ctx, operator); err != nil {
			logger.Errorf("Failed to update status: %v", err)
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{Requeue: true}, nil
}

func InstallCrds(ctx context.Context, r *Reconciler) error {
	// Install each CRD from manifestsFS under manifests/crds
	entries, err := manifestsFS.ReadDir("manifests/crds")
	if err != nil {
		return fmt.Errorf("failed to read CRD directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		crdBytes, err := manifestsFS.ReadFile("manifests/crds/" + entry.Name())
		if err != nil {
			return fmt.Errorf("failed to read CRD file %s: %w", entry.Name(), err)
		}

		// Unmarshal YAML to unstructured.Unstructured
		obj := &unstructured.Unstructured{}
		if err := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(crdBytes), 4096).Decode(obj); err != nil {
			return fmt.Errorf("failed to unmarshal CRD file %s: %w", entry.Name(), err)
		}

		// Try to get the existing CRD first
		existing := &unstructured.Unstructured{}
		existing.SetGroupVersionKind(obj.GroupVersionKind())
		err = r.Client.Get(ctx, client.ObjectKey{Name: obj.GetName()}, existing)
		if err != nil {
			if errors.IsNotFound(err) {
				// CRD doesn't exist, create it
				if err := r.Client.Create(ctx, obj); err != nil {
					return fmt.Errorf("failed to create CRD %s: %w", obj.GetName(), err)
				}
				logger.Infof("Created CRD %s", obj.GetName())
			} else {
				return fmt.Errorf("failed to get existing CRD %s: %w", obj.GetName(), err)
			}
		} else {
			// CRD exists, update it
			obj.SetResourceVersion(existing.GetResourceVersion())
			if err := r.Client.Update(ctx, obj); err != nil {
				return fmt.Errorf("failed to update CRD %s: %w", obj.GetName(), err)
			}
			logger.Infof("Updated CRD %s", obj.GetName())
		}
	}
	return nil
}
