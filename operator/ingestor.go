package operator

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"text/template"
	"time"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/celutil"
	"github.com/Azure/adx-mon/pkg/logger"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

//go:embed manifests/crds/functions_crd.yaml manifests/crds/managementcommands_crd.yaml manifests/crds/summaryrules_crd.yaml manifests/ingestor.yaml
var ingestorCrdsFS embed.FS

// Condition reason constants for Ingestor status
const (
	ReasonWaitForReady            = "WaitForReady"
	ReasonCRDsInstalled           = "CRDsInstalled"
	ReasonTemplateError           = "TemplateError"
	ReasonNotReady                = "NotReady"
	ReasonReady                   = "Ready"
	ReasonCriteriaExpressionError = "CriteriaExpressionError"
	ReasonCriteriaExpressionFalse = "CriteriaExpressionFalse"
)

type IngestorReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	waitForReadyReason string
}

func (r *IngestorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ingestor := &adxmonv1.Ingestor{}
	if err := r.Get(ctx, req.NamespacedName, ingestor); err != nil {
		return r.ReconcileComponent(ctx, req)
	}

	if expr := ingestor.Spec.CriteriaExpression; expr != "" {
		labels := getOperatorClusterLabels()
		ok, err := celutil.EvaluateCriteriaExpression(labels, expr)
		if err != nil {
			logger.Errorf("Ingestor %s/%s criteriaExpression error: %v", req.Namespace, req.Name, err)
			// Expression errors are terminal until the CRD changes; set status and exit without requeue.
			c := metav1.Condition{Type: adxmonv1.IngestorConditionOwner, Status: metav1.ConditionFalse, Reason: ReasonCriteriaExpressionError, Message: err.Error(), ObservedGeneration: ingestor.GetGeneration(), LastTransitionTime: metav1.Now()}
			if meta.SetStatusCondition(&ingestor.Status.Conditions, c) {
				if err := r.Status().Update(ctx, ingestor); err != nil {
					logger.Errorf("Failed to update status for Ingestor %s/%s: %v", ingestor.Namespace, ingestor.Name, err)
				}
			}
			return ctrl.Result{}, nil
		}
		if !ok {
			c := metav1.Condition{Type: adxmonv1.IngestorConditionOwner, Status: metav1.ConditionFalse, Reason: ReasonCriteriaExpressionFalse, Message: "criteriaExpression evaluated to false; skipping", ObservedGeneration: ingestor.GetGeneration(), LastTransitionTime: metav1.Now()}
			if meta.SetStatusCondition(&ingestor.Status.Conditions, c) {
				if err := r.Status().Update(ctx, ingestor); err != nil {
					logger.Errorf("Failed to update status for Ingestor %s/%s: %v", ingestor.Namespace, ingestor.Name, err)
				}
			}
			return ctrl.Result{}, nil
		}
	}

	if !ingestor.DeletionTimestamp.IsZero() {
		logger.Infof("Ingestor %s/%s is being deleted, skipping reconciliation", ingestor.Namespace, ingestor.Name)
		return ctrl.Result{}, nil
	}

	condition := meta.FindStatusCondition(ingestor.Status.Conditions, adxmonv1.IngestorConditionOwner)
	switch {
	case condition == nil:
		// First time reconciliation
		return r.CreateIngestor(ctx, ingestor)

	case condition.Reason == r.waitForReadyReason:
		// Ingestor is installing, check if the ADXCluster is ready
		return r.IsReady(ctx, ingestor)

	case condition.Status == metav1.ConditionUnknown:
		// Retry installation of ingestor manifests
		return r.CreateIngestor(ctx, ingestor)

	case condition.ObservedGeneration != ingestor.GetGeneration():
		// CRD has been updated, re-render the ingestor manifests
		return r.CreateIngestor(ctx, ingestor)
	}

	return ctrl.Result{}, nil
}

func (r *IngestorReconciler) IsReady(ctx context.Context, ingestor *adxmonv1.Ingestor) (ctrl.Result, error) {
	var sts appsv1.StatefulSet
	if err := r.Get(ctx, client.ObjectKey{Namespace: ingestor.GetNamespace(), Name: ingestor.GetName()}, &sts); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{RequeueAfter: time.Minute}, nil
		}
		return ctrl.Result{}, err
	}

	if sts.Status.ReadyReplicas == *sts.Spec.Replicas {
		if err := r.setCondition(ctx, ingestor, ReasonReady, "All ingestor replicas are ready", metav1.ConditionTrue); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

func (r *IngestorReconciler) ReconcileComponent(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var sts appsv1.StatefulSet
	if err := r.Get(ctx, req.NamespacedName, &sts); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Fetch the Ingestor CRD
	ingestor := &adxmonv1.Ingestor{}
	if err := r.Get(ctx, req.NamespacedName, ingestor); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Retrieve the applied provisioning state
	stored, err := ingestor.Spec.LoadAppliedProvisioningState()
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to load applied provisioning state: %w", err)
	}

	var update bool

	// Update image if needed
	if r.updateImageIfNeeded(&sts, ingestor) {
		update = true
	}

	// Update replicas if needed
	if r.updateReplicasIfNeeded(&sts, ingestor) {
		update = true
	}

	// Log ExposeExternally if set (not implemented)
	r.logExposeExternally(ingestor)

	// Handle ADXClusterSelector changes and update args if needed
	changed, err := r.handleADXClusterSelectorChange(ctx, &sts, ingestor, stored)
	if err != nil {
		return ctrl.Result{}, err
	}
	if changed {
		update = true
	}

	// Apply updates if any
	if update {
		if err := r.Update(ctx, &sts); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update StatefulSet: %w", err)
		}
		if err := r.setCondition(ctx, ingestor, r.waitForReadyReason, "Ingestor manifest updating...", metav1.ConditionUnknown); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to set status condition: %w", err)
		}
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}

	// No changes to apply
	return ctrl.Result{}, nil
}

// updateImageIfNeeded updates the StatefulSet image if it differs from the Ingestor spec.
func (r *IngestorReconciler) updateImageIfNeeded(sts *appsv1.StatefulSet, ingestor *adxmonv1.Ingestor) bool {
	if len(sts.Spec.Template.Spec.Containers) == 1 {
		if sts.Spec.Template.Spec.Containers[0].Image != ingestor.Spec.Image {
			logger.Infof("Updating image for Ingestor %s/%s from %s to %s", ingestor.Namespace, ingestor.Name, sts.Spec.Template.Spec.Containers[0].Image, ingestor.Spec.Image)
			sts.Spec.Template.Spec.Containers[0].Image = ingestor.Spec.Image
			return true
		}
	}
	return false
}

// updateReplicasIfNeeded updates the StatefulSet replicas if it differs from the Ingestor spec.
func (r *IngestorReconciler) updateReplicasIfNeeded(sts *appsv1.StatefulSet, ingestor *adxmonv1.Ingestor) bool {
	if sts.Spec.Replicas != nil && *sts.Spec.Replicas != ingestor.Spec.Replicas {
		logger.Infof("Updating replicas for Ingestor %s/%s from %d to %d", ingestor.Namespace, ingestor.Name, *sts.Spec.Replicas, ingestor.Spec.Replicas)
		*sts.Spec.Replicas = ingestor.Spec.Replicas
		return true
	}
	return false
}

// logExposeExternally logs if ExposeExternally is set (feature not implemented).
func (r *IngestorReconciler) logExposeExternally(ingestor *adxmonv1.Ingestor) {
	if ingestor.Spec.ExposeExternally {
		logger.Infof("ExposeExternally is set to true for Ingestor %s/%s, but not implemented", ingestor.Namespace, ingestor.Name)
	}
}

// handleADXClusterSelectorChange checks for selector changes and updates container args if needed.
func (r *IngestorReconciler) handleADXClusterSelectorChange(ctx context.Context, sts *appsv1.StatefulSet, ingestor *adxmonv1.Ingestor, stored *adxmonv1.IngestorSpec) (bool, error) {
	if stored == nil {
		// If there's no stored spec, we can't compare, assume no change needed based on selector diff
		return false, nil
	}
	storedSel, err := json.Marshal(stored.ADXClusterSelector)
	if err != nil {
		return false, fmt.Errorf("failed to marshal stored ADXClusterSelector: %w", err)
	}
	currentSel, err := json.Marshal(ingestor.Spec.ADXClusterSelector)
	if err != nil {
		return false, fmt.Errorf("failed to marshal current ADXClusterSelector: %w", err)
	}
	if string(storedSel) == string(currentSel) {
		// Selector hasn't changed
		return false, nil
	}
	logger.Infof("ADXClusterSelector changed for Ingestor %s/%s: stored=%s, current=%s", ingestor.Namespace, ingestor.Name, string(storedSel), string(currentSel))

	_, data, err := r.templateData(ctx, ingestor)
	if err != nil {
		// If we fail to get template data (e.g., cluster not ready), report error but don't mark as changed yet.
		// The reconciliation should requeue and try again later.
		return false, fmt.Errorf("failed to get template data for selector change: %w", err)
	}

	// Filter existing args, keeping only those not related to kusto endpoints
	currentArgs := sts.Spec.Template.Spec.Containers[0].Args
	newArgs := make([]string, 0, len(currentArgs)) // Pre-allocate capacity
	for _, arg := range currentArgs {
		if !strings.HasPrefix(arg, "--metrics-kusto-endpoints=") && !strings.HasPrefix(arg, "--logs-kusto-endpoints=") {
			newArgs = append(newArgs, arg)
		}
	}

	// Append new endpoint args based on the current selector
	for _, cluster := range data.MetricsClusters {
		newArgs = append(newArgs, fmt.Sprintf("--metrics-kusto-endpoints=%s", cluster))
	}
	for _, cluster := range data.LogsClusters {
		newArgs = append(newArgs, fmt.Sprintf("--logs-kusto-endpoints=%s", cluster))
	}

	// Check if the arguments actually changed before assigning back
	// Sort slices before comparing to ensure order doesn't matter
	currentArgsSorted := slices.Clone(currentArgs)
	newArgsSorted := slices.Clone(newArgs)
	slices.Sort(currentArgsSorted)
	slices.Sort(newArgsSorted)

	if slices.Equal(currentArgsSorted, newArgsSorted) {
		// No actual change in arguments after filtering and adding new ones based on the new selector
		logger.Infof("ADXClusterSelector changed for Ingestor %s/%s, but resulting args are the same.", ingestor.Namespace, ingestor.Name)
		return false, nil
	}

	logger.Infof("Updating args for Ingestor %s/%s due to ADXClusterSelector change.", ingestor.Namespace, ingestor.Name)
	sts.Spec.Template.Spec.Containers[0].Args = newArgs // Assign the final list of args

	return true, nil
}

func (r *IngestorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.waitForReadyReason = ReasonWaitForReady

	// Define the mapping function for ADXCluster changes to enqueue Ingestor reconciliations
	mapFn := func(ctx context.Context, obj client.Object) []reconcile.Request {
		cluster, ok := obj.(*adxmonv1.ADXCluster)
		if !ok {
			logger.Errorf("EventHandler received non-ADXCluster object: %T", obj)
			return nil
		}

		ingestorList := &adxmonv1.IngestorList{}
		// List Ingestors only in the namespace of the changed ADXCluster
		if err := r.Client.List(ctx, ingestorList, client.InNamespace(cluster.Namespace)); err != nil {
			logger.Errorf("Failed to list Ingestors in namespace %s while handling ADXCluster %s/%s event: %v", cluster.Namespace, cluster.Namespace, cluster.Name, err)
			return nil
		}

		requests := []reconcile.Request{}
		for _, ingestor := range ingestorList.Items {
			// Skip if Ingestor is being deleted
			if !ingestor.DeletionTimestamp.IsZero() {
				continue
			}
			// Check if the Ingestor's selector matches the ADXCluster's labels
			if ingestor.Spec.ADXClusterSelector == nil {
				// If selector is nil, it selects nothing.
				continue
			}
			selector, err := metav1.LabelSelectorAsSelector(ingestor.Spec.ADXClusterSelector)
			if err != nil {
				logger.Errorf("Failed to parse selector for Ingestor %s/%s: %v", ingestor.Namespace, ingestor.Name, err)
				continue // Skip this ingestor if selector is invalid
			}

			if selector.Matches(labels.Set(cluster.GetLabels())) {
				// If the selector matches, enqueue a reconcile request for this Ingestor
				requests = append(requests, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      ingestor.Name,
						Namespace: ingestor.Namespace,
					},
				})
				logger.Infof("Enqueuing reconcile request for Ingestor %s/%s due to change in ADXCluster %s/%s", ingestor.Namespace, ingestor.Name, cluster.Namespace, cluster.Name)
			}
		}
		return requests
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&adxmonv1.Ingestor{}).
		Owns(&appsv1.StatefulSet{}).
		// Add Watches for ADXCluster changes
		Watches(
			&adxmonv1.ADXCluster{},
			handler.EnqueueRequestsFromMapFunc(mapFn),
		).
		Complete(r)
}

func (r *IngestorReconciler) CreateIngestor(ctx context.Context, ingestor *adxmonv1.Ingestor) (ctrl.Result, error) {
	r.applyDefaults(ingestor)
	if err := ingestor.Spec.StoreAppliedProvisioningState(); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to store applied provisioning state: %w", err)
	}
	if err := r.Update(ctx, ingestor); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update ingestor: %w", err)
	}

	// Install CRDs
	if err := r.installCrds(ctx); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to install CRDs: %w", err)
	}
	if err := r.setCondition(ctx, ingestor, ReasonCRDsInstalled, "CRDs installed successfully", metav1.ConditionUnknown); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to set status condition: %w", err)
	}

	// Render the ingestor manifest
	tmplBytes, err := ingestorCrdsFS.ReadFile("manifests/ingestor.yaml")
	if err != nil {
		// This is a terminal condition because a retry will not help.
		if err := r.setCondition(ctx, ingestor, ReasonTemplateError, "Failed to read ingestor template", metav1.ConditionFalse); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil // No need to retry
	}
	tmpl, err := template.New("ingestor").Parse(string(tmplBytes))
	if err != nil {
		// This is a terminal condition because a retry will not help.
		if err := r.setCondition(ctx, ingestor, ReasonTemplateError, "Failed to parse ingestor template", metav1.ConditionFalse); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil // No need to retry
	}

	ready, data, err := r.templateData(ctx, ingestor)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get template data: %w", err)
	}
	if !ready {
		if err := r.setCondition(ctx, ingestor, ReasonNotReady, "ADXCluster not ready", metav1.ConditionUnknown); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
	}

	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, data); err != nil {
		// This is a terminal condition because a retry will not help.
		if err := r.setCondition(ctx, ingestor, ReasonTemplateError, "Failed to render ingestor template", metav1.ConditionFalse); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil // No need to retry
	}

	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(rendered.Bytes()), 4096)
	for {
		if ctx.Err() != nil {
			return ctrl.Result{}, ctx.Err()
		}
		obj := &unstructured.Unstructured{}
		err := decoder.Decode(obj)
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			continue
		}
		if obj.Object == nil || obj.GetKind() == "" {
			logger.Debugf("Skipping empty or invalid YAML document in ingestor manifest")
			continue
		}
		// Set the owner reference, this enables garbage collection for the ingestor
		// and ensures that the ingestor is deleted when the owner is deleted.
		// --> Only set owner reference if the object is namespace-scoped.
		if obj.GetNamespace() != "" {
			if err := controllerutil.SetControllerReference(ingestor, obj, r.Scheme); err != nil {
				// Check if the error is specifically about cluster-scoped resources having namespace-scoped owners
				// This might happen if the object's namespace is empty but it's not truly cluster-scoped according to the scheme? Unlikely but safer check.
				if strings.Contains(err.Error(), "cluster-scoped resource must not have a namespace-scoped owner") {
					logger.Warnf("Skipping owner reference for potentially cluster-scoped resource %s/%s", obj.GetKind(), obj.GetName())
				} else {
					return ctrl.Result{}, fmt.Errorf("failed to set owner reference for %s %s: %w", obj.GetKind(), obj.GetName(), err)
				}
			}
		} else {
			logger.Infof("Skipping owner reference for cluster-scoped resource %s/%s", obj.GetKind(), obj.GetName())
		}

		if err := r.Create(ctx, obj); err != nil && !errors.IsAlreadyExists(err) {
			return ctrl.Result{}, fmt.Errorf("failed to create %s %s: %w", obj.GetKind(), obj.GetName(), err)
		}
	}

	if err := r.setCondition(ctx, ingestor, r.waitForReadyReason, "Ingestor manifests installing", metav1.ConditionTrue); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to set status condition: %w", err)
	}
	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

func (s *IngestorReconciler) applyDefaults(ingestor *adxmonv1.Ingestor) {
	if ingestor.Spec.Replicas == 0 {
		ingestor.Spec.Replicas = 1
	}
	if ingestor.Spec.Image == "" {
		ingestor.Spec.Image = "ghcr.io/azure/adx-mon/ingestor:latest"
	}
}

type ingestorTemplateData struct {
	Image           string
	MetricsClusters []string
	LogsClusters    []string
	Namespace       string
}

func (r *IngestorReconciler) templateData(ctx context.Context, ingestor *adxmonv1.Ingestor) (clustersReady bool, data *ingestorTemplateData, err error) {
	// ADXClusterSelector is required - nil selector means nothing is selected
	if ingestor.Spec.ADXClusterSelector == nil {
		return false, nil, fmt.Errorf("ADXClusterSelector is required")
	}

	// List ADXClusters matching the selector
	selector, err := metav1.LabelSelectorAsSelector(ingestor.Spec.ADXClusterSelector)
	if err != nil {
		return false, nil, fmt.Errorf("failed to convert label selector: %w", err)
	}

	var adxClusterList adxmonv1.ADXClusterList
	listOpts := []client.ListOption{client.MatchingLabelsSelector{Selector: selector}}
	if err := r.Client.List(ctx, &adxClusterList, listOpts...); err != nil {
		return false, nil, fmt.Errorf("failed to list ADXClusters: %w", err)
	}

	var metricsClusters []string
	var logsClusters []string
	for _, cluster := range adxClusterList.Items {
		// wait for the cluster to be ready
		if !meta.IsStatusConditionTrue(cluster.Status.Conditions, adxmonv1.ADXClusterConditionOwner) {
			// Cluster is not ready yet, but this is not an error
			return false, nil, nil
		}

		endpoint := resolvedClusterEndpoint(&cluster)
		if endpoint == "" {
			continue // Skip this cluster entirely if no endpoint
		}

		for _, db := range cluster.Spec.Databases {
			entry := fmt.Sprintf("%s=%s", db.DatabaseName, endpoint)
			switch db.TelemetryType {
			case adxmonv1.DatabaseTelemetryMetrics:
				metricsClusters = append(metricsClusters, entry)
			case adxmonv1.DatabaseTelemetryLogs:
				logsClusters = append(logsClusters, entry)
			default:
				// Traces and other telemetry types are not currently supported by the ingestor
			}
		}
	}

	data = &ingestorTemplateData{
		Image:           ingestor.Spec.Image,
		MetricsClusters: metricsClusters,
		LogsClusters:    logsClusters,
		Namespace:       ingestor.Namespace,
	}
	return true, data, nil
}

func (r *IngestorReconciler) setCondition(ctx context.Context, ingestor *adxmonv1.Ingestor, reason, message string, status metav1.ConditionStatus) error {
	condition := metav1.Condition{
		Type:               adxmonv1.IngestorConditionOwner,
		Status:             status,
		ObservedGeneration: ingestor.GetGeneration(),
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}
	if meta.SetStatusCondition(&ingestor.Status.Conditions, condition) {
		if err := r.Status().Update(ctx, ingestor); err != nil {
			return fmt.Errorf("failed to update status: %w", err)
		}
	}
	return nil
}

func (r *IngestorReconciler) installCrds(ctx context.Context) error {
	// Install Ingestor related CRDs from ingestorCrdsFS under manifests/crds
	entries, err := ingestorCrdsFS.ReadDir("manifests/crds")
	if err != nil {
		return fmt.Errorf("failed to read CRD directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		crdBytes, err := ingestorCrdsFS.ReadFile("manifests/crds/" + entry.Name())
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
