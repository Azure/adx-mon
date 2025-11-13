package operator

import (
	bytes "bytes"
	context "context"
	embed "embed"
	"encoding/json"
	fmt "fmt"
	"slices"
	"strings"
	texttemplate "text/template"
	time "time"

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

//go:embed manifests/crds/alertrules_crd.yaml manifests/alerter.yaml
var alerterFS embed.FS

// AlerterReconciler reconciles Alerter CRDs and manages the Alerter deployment
// and AlertRule CRD installation.
type AlerterReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	waitForReadyReason string
}

func (r *AlerterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	alerter := &adxmonv1.Alerter{}
	if err := r.Get(ctx, req.NamespacedName, alerter); err != nil {
		return r.ReconcileComponent(ctx, req)
	}

	if expr := alerter.Spec.CriteriaExpression; expr != "" {
		labels := getOperatorClusterLabels()
		ok, err := celutil.EvaluateCriteriaExpression(labels, expr)
		if err != nil {
			logger.Errorf("Alerter %s/%s criteriaExpression error: %v", req.Namespace, req.Name, err)
			// Expression errors are terminal until the CRD is updated, so we just set status and exit.
			c := metav1.Condition{Type: adxmonv1.AlerterConditionOwner, Status: metav1.ConditionFalse, Reason: "CriteriaExpressionError", Message: err.Error(), ObservedGeneration: alerter.GetGeneration(), LastTransitionTime: metav1.Now()}
			if meta.SetStatusCondition(&alerter.Status.Conditions, c) {
				_ = r.Status().Update(ctx, alerter)
			}
			return ctrl.Result{}, nil
		}
		if !ok {
			c := metav1.Condition{Type: adxmonv1.AlerterConditionOwner, Status: metav1.ConditionFalse, Reason: "CriteriaExpressionFalse", Message: "criteriaExpression evaluated to false; skipping", ObservedGeneration: alerter.GetGeneration(), LastTransitionTime: metav1.Now()}
			if meta.SetStatusCondition(&alerter.Status.Conditions, c) {
				_ = r.Status().Update(ctx, alerter)
			}
			return ctrl.Result{}, nil
		}
	}

	if !alerter.DeletionTimestamp.IsZero() {
		logger.Infof("Alerter %s/%s is being deleted, skipping reconciliation", alerter.Namespace, alerter.Name)
		return ctrl.Result{}, nil
	}

	condition := meta.FindStatusCondition(alerter.Status.Conditions, adxmonv1.AlerterConditionOwner)
	switch {
	case condition == nil:
		return r.CreateAlerter(ctx, alerter)
	case condition.Reason == r.waitForReadyReason:
		return r.IsReady(ctx, alerter)
	case condition.Status == metav1.ConditionUnknown:
		return r.CreateAlerter(ctx, alerter)
	case condition.ObservedGeneration != alerter.GetGeneration():
		return r.CreateAlerter(ctx, alerter)
	}
	return ctrl.Result{}, nil
}

func (r *AlerterReconciler) ReconcileComponent(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var deployment appsv1.Deployment
	if err := r.Get(ctx, req.NamespacedName, &deployment); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	alerter := &adxmonv1.Alerter{}
	if err := r.Get(ctx, req.NamespacedName, alerter); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	stored, err := alerter.Spec.LoadAppliedProvisioningState()
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to load applied provisioning state: %w", err)
	}

	var update bool

	// Update image if needed
	if r.updateImageIfNeeded(&deployment, alerter) {
		update = true
	}

	// Handle ADXClusterSelector changes and update args if needed
	changed, err := r.handleADXClusterSelectorChange(ctx, &deployment, alerter, stored)
	if err != nil {
		return ctrl.Result{}, err
	}
	if changed {
		update = true
	}

	// Apply updates if any
	if update {
		if err := r.Update(ctx, &deployment); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update Deployment: %w", err)
		}
		if err := r.setCondition(ctx, alerter, r.waitForReadyReason, "Alerter manifest updating...", metav1.ConditionUnknown); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to set status condition: %w", err)
		}
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}

	// No changes to apply
	return ctrl.Result{}, nil
}

// handleADXClusterSelectorChange checks for selector changes and updates container args if needed.
func (r *AlerterReconciler) handleADXClusterSelectorChange(ctx context.Context, deployment *appsv1.Deployment, alerter *adxmonv1.Alerter, stored *adxmonv1.AlerterSpec) (bool, error) {
	if stored == nil {
		// If there's no stored spec, we can't compare, assume no change needed based on selector diff
		return false, nil
	}
	storedSel, _ := json.Marshal(stored.ADXClusterSelector)
	currentSel, _ := json.Marshal(alerter.Spec.ADXClusterSelector)
	if string(storedSel) == string(currentSel) {
		// Selector hasn't changed
		return false, nil
	}
	logger.Infof("ADXClusterSelector changed for alerter %s/%s: stored=%s, current=%s", alerter.Namespace, alerter.Name, string(storedSel), string(currentSel))

	_, data, err := r.templateData(ctx, alerter)
	if err != nil {
		// If we fail to get template data (e.g., cluster not ready), report error but don't mark as changed yet.
		// The reconciliation should requeue and try again later.
		return false, fmt.Errorf("failed to get template data for selector change: %w", err)
	}

	// Filter existing args, keeping only those not related to kusto endpoints
	currentArgs := deployment.Spec.Template.Spec.Containers[0].Args
	newArgs := make([]string, 0, len(currentArgs)) // Pre-allocate capacity
	for _, arg := range currentArgs {
		if !strings.HasPrefix(arg, "--kusto-endpoint=") {
			newArgs = append(newArgs, arg)
		}
	}

	// Append new endpoint args based on the current selector
	for _, cluster := range data.KustoEndpoints {
		newArgs = append(newArgs, fmt.Sprintf("--kusto-endpoint=%s", cluster))
	}

	// Check if the arguments actually changed before assigning back
	// Sort slices before comparing to ensure order doesn't matter
	currentArgsSorted := slices.Clone(currentArgs)
	newArgsSorted := slices.Clone(newArgs)
	slices.Sort(currentArgsSorted)
	slices.Sort(newArgsSorted)

	if slices.Equal(currentArgsSorted, newArgsSorted) {
		// No actual change in arguments after filtering and adding new ones based on the new selector
		logger.Infof("ADXClusterSelector changed for alerter %s/%s, but resulting args are the same.", alerter.Namespace, alerter.Name)
		return false, nil
	}

	logger.Infof("Updating args for alerter %s/%s due to ADXClusterSelector change.", alerter.Namespace, alerter.Name)
	deployment.Spec.Template.Spec.Containers[0].Args = newArgs // Assign the final list of args

	return true, nil
}

// updateImageIfNeeded updates the Deployment image if it differs from the Alerter spec.
func (r *AlerterReconciler) updateImageIfNeeded(deployment *appsv1.Deployment, alerter *adxmonv1.Alerter) bool {
	if len(deployment.Spec.Template.Spec.Containers) == 1 {
		if deployment.Spec.Template.Spec.Containers[0].Image != alerter.Spec.Image {
			logger.Infof("Updating image for alerter %s/%s from %s to %s", alerter.Namespace, alerter.Name, deployment.Spec.Template.Spec.Containers[0].Image, alerter.Spec.Image)
			deployment.Spec.Template.Spec.Containers[0].Image = alerter.Spec.Image
			return true
		}
	}
	return false
}

func (r *AlerterReconciler) IsReady(ctx context.Context, alerter *adxmonv1.Alerter) (ctrl.Result, error) {
	var deploy appsv1.Deployment
	if err := r.Get(ctx, client.ObjectKey{Namespace: alerter.GetNamespace(), Name: "alerter"}, &deploy); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{RequeueAfter: time.Minute}, nil
		}
		return ctrl.Result{}, err
	}
	if deploy.Status.ReadyReplicas == *deploy.Spec.Replicas {
		if err := r.setCondition(ctx, alerter, "Ready", "Alerter deployment is ready", metav1.ConditionTrue); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}
	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

func (r *AlerterReconciler) CreateAlerter(ctx context.Context, alerter *adxmonv1.Alerter) (ctrl.Result, error) {
	r.applyDefaults(alerter)
	if err := r.installAlertRuleCRD(ctx); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to install AlertRule CRD: %w", err)
	}
	if err := r.setCondition(ctx, alerter, "CRDsInstalled", "AlertRule CRD installed successfully", metav1.ConditionUnknown); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to set status condition: %w", err)
	}

	tmplBytes, err := alerterFS.ReadFile("manifests/alerter.yaml")
	if err != nil {
		_ = r.setCondition(ctx, alerter, "TemplateError", "Failed to read alerter template", metav1.ConditionFalse)
		return ctrl.Result{}, nil
	}
	tmpl, err := texttemplate.New("alerter").Parse(string(tmplBytes))
	if err != nil {
		_ = r.setCondition(ctx, alerter, "TemplateError", "Failed to parse alerter template", metav1.ConditionFalse)
		return ctrl.Result{}, nil
	}

	ready, data, err := r.templateData(ctx, alerter)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get template data: %w", err)
	}
	if !ready {
		_ = r.setCondition(ctx, alerter, "NotReady", "ADXCluster not ready", metav1.ConditionUnknown)
		return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
	}

	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, data); err != nil {
		_ = r.setCondition(ctx, alerter, "TemplateError", "Failed to render alerter template", metav1.ConditionFalse)
		return ctrl.Result{}, nil
	}

	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(rendered.Bytes()), 4096)
	for {
		obj := &unstructured.Unstructured{}
		err := decoder.Decode(obj)
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			continue
		}
		if obj.Object == nil || obj.GetKind() == "" {
			continue
		}
		if obj.GetNamespace() != "" {
			if err := controllerutil.SetControllerReference(alerter, obj, r.Scheme); err != nil {
				if err.Error() != "cluster-scoped resource must not have a namespace-scoped owner" {
					return ctrl.Result{}, fmt.Errorf("failed to set owner reference for %s %s: %w", obj.GetKind(), obj.GetName(), err)
				}
			}
		}
		if err := r.Create(ctx, obj); err != nil && !errors.IsAlreadyExists(err) {
			return ctrl.Result{}, fmt.Errorf("failed to create %s %s: %w", obj.GetKind(), obj.GetName(), err)
		}
	}

	if err := r.setCondition(ctx, alerter, r.waitForReadyReason, "Alerter manifests installing", metav1.ConditionTrue); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to set status condition: %w", err)
	}
	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

func (r *AlerterReconciler) installAlertRuleCRD(ctx context.Context) error {
	crdBytes, err := alerterFS.ReadFile("manifests/crds/alertrules_crd.yaml")
	if err != nil {
		return fmt.Errorf("failed to read AlertRule CRD: %w", err)
	}
	obj := &unstructured.Unstructured{}
	if err := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(crdBytes), 4096).Decode(obj); err != nil {
		return fmt.Errorf("failed to unmarshal AlertRule CRD: %w", err)
	}
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(obj.GroupVersionKind())
	err = r.Client.Get(ctx, client.ObjectKey{Name: obj.GetName()}, existing)
	if err != nil {
		if errors.IsNotFound(err) {
			if err := r.Client.Create(ctx, obj); err != nil {
				return fmt.Errorf("failed to create AlertRule CRD %s: %w", obj.GetName(), err)
			}
			logger.Infof("Created AlertRule CRD %s", obj.GetName())
		} else {
			return fmt.Errorf("failed to get existing AlertRule CRD %s: %w", obj.GetName(), err)
		}
	} else {
		obj.SetResourceVersion(existing.GetResourceVersion())
		if err := r.Client.Update(ctx, obj); err != nil {
			return fmt.Errorf("failed to update AlertRule CRD %s: %w", obj.GetName(), err)
		}
		logger.Infof("Updated AlertRule CRD %s", obj.GetName())
	}
	return nil
}

func (r *AlerterReconciler) applyDefaults(alerter *adxmonv1.Alerter) {
	if alerter.Spec.Image == "" {
		alerter.Spec.Image = "ghcr.io/azure/adx-mon/alerter:latest"
	}
}

type alerterTemplateData struct {
	Image           string
	AlerterEndpoint string
	KustoEndpoints  []string
	MSIID           string
	Namespace       string
}

func (r *AlerterReconciler) templateData(ctx context.Context, alerter *adxmonv1.Alerter) (clustersReady bool, data *alerterTemplateData, err error) {
	selector, err := metav1.LabelSelectorAsSelector(alerter.Spec.ADXClusterSelector)
	if err != nil {
		return false, nil, fmt.Errorf("failed to convert label selector: %w", err)
	}
	var adxClusterList adxmonv1.ADXClusterList
	listOpts := []client.ListOption{}
	if alerter.Spec.ADXClusterSelector != nil {
		listOpts = append(listOpts, client.MatchingLabelsSelector{Selector: selector})
	}
	if err := r.Client.List(ctx, &adxClusterList, listOpts...); err != nil {
		return false, nil, fmt.Errorf("failed to list ADXClusters: %w", err)
	}
	var kustoEndpoints []string
	for _, cluster := range adxClusterList.Items {
		if !meta.IsStatusConditionTrue(cluster.Status.Conditions, adxmonv1.ADXClusterConditionOwner) {
			return false, nil, fmt.Errorf("ADXCluster is not ready")
		}
		if endpoint := resolvedClusterEndpoint(&cluster); endpoint != "" {
			kustoEndpoints = append(kustoEndpoints, endpoint)
		}
	}
	data = &alerterTemplateData{
		Image:           alerter.Spec.Image,
		AlerterEndpoint: alerter.Spec.NotificationEndpoint,
		KustoEndpoints:  kustoEndpoints,
		Namespace:       alerter.Namespace,
	}
	return true, data, nil
}

func (r *AlerterReconciler) setCondition(ctx context.Context, alerter *adxmonv1.Alerter, reason, message string, status metav1.ConditionStatus) error {
	condition := metav1.Condition{
		Type:               adxmonv1.AlerterConditionOwner,
		Status:             status,
		ObservedGeneration: alerter.GetGeneration(),
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}
	if meta.SetStatusCondition(&alerter.Status.Conditions, condition) {
		if err := r.Status().Update(ctx, alerter); err != nil {
			return fmt.Errorf("failed to update status: %w", err)
		}
	}
	return nil
}

func (r *AlerterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.waitForReadyReason = "WaitForReady"

	// Define the mapping function for ADXCluster changes to enqueue Alerter reconciliations
	mapFn := func(ctx context.Context, obj client.Object) []reconcile.Request {
		cluster, ok := obj.(*adxmonv1.ADXCluster)
		if !ok {
			logger.Errorf("EventHandler received non-ADXCluster object: %T", obj)
			return nil
		}

		alerterList := &adxmonv1.AlerterList{}
		// List Alerters only in the namespace of the changed ADXCluster
		if err := r.Client.List(ctx, alerterList, client.InNamespace(cluster.Namespace)); err != nil {
			logger.Errorf("Failed to list Alerters in namespace %s while handling ADXCluster %s/%s event: %v", cluster.Namespace, cluster.Namespace, cluster.Name, err)
			return nil
		}

		requests := []reconcile.Request{}
		for _, alerter := range alerterList.Items {
			// Skip if Alerter is being deleted
			if !alerter.DeletionTimestamp.IsZero() {
				continue
			}
			// Check if the Alerter's selector matches the ADXCluster's labels
			if alerter.Spec.ADXClusterSelector == nil {
				// If selector is nil, it selects nothing.
				continue
			}
			selector, err := metav1.LabelSelectorAsSelector(alerter.Spec.ADXClusterSelector)
			if err != nil {
				logger.Errorf("Failed to parse selector for Alerter %s/%s: %v", alerter.Namespace, alerter.Name, err)
				continue // Skip this Alerter if selector is invalid
			}

			if selector.Matches(labels.Set(cluster.GetLabels())) {
				// If the selector matches, enqueue a reconcile request for this Alerter
				requests = append(requests, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      alerter.Name,
						Namespace: alerter.Namespace,
					},
				})
				logger.Infof("Enqueuing reconcile request for Alerter %s/%s due to change in ADXCluster %s/%s", alerter.Namespace, alerter.Name, cluster.Namespace, cluster.Name)

				if err := r.setCondition(ctx, &alerter, "ADXClusterChanged", fmt.Sprintf("ADXCluster %s/%s changed", cluster.Namespace, cluster.Name), metav1.ConditionUnknown); err != nil {
					logger.Errorf("Failed to set condition for Alerter %s/%s: %v", alerter.Namespace, alerter.Name, err)
				}
			}
		}
		return requests
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&adxmonv1.Alerter{}).
		Owns(&appsv1.Deployment{}).
		// Add Watches for ADXCluster changes
		Watches(
			&adxmonv1.ADXCluster{},
			handler.EnqueueRequestsFromMapFunc(mapFn),
		).
		Complete(r)
}
