package operator

import (
	bytes "bytes"
	context "context"
	embed "embed"
	fmt "fmt"
	"slices"
	"strings"
	texttemplate "text/template"
	time "time"

	"github.com/Azure/adx-mon/alerter/alert"
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
	"k8s.io/apimachinery/pkg/util/yaml"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	defaultAlerterImage                         = "ghcr.io/azure/adx-mon/alerter:latest"
	desiredAlerterTerminationGracePeriodSeconds = int64(60)
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

func (r *AlerterReconciler) waitReason() string {
	if r.waitForReadyReason != "" {
		return r.waitForReadyReason
	}
	return "WaitForReady"
}

func (r *AlerterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	alerter := &adxmonv1.Alerter{}
	if err := r.Get(ctx, req.NamespacedName, alerter); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
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

	if _, err := resolvedNotificationEndpoints(alerter.Spec); err != nil {
		_ = r.setCondition(ctx, alerter, "InvalidSpec", err.Error(), metav1.ConditionFalse)
		return ctrl.Result{}, nil
	}

	condition := meta.FindStatusCondition(alerter.Status.Conditions, adxmonv1.AlerterConditionOwner)
	switch {
	case condition == nil:
		return r.CreateAlerter(ctx, alerter)
	case condition.Status == metav1.ConditionUnknown:
		return r.CreateAlerter(ctx, alerter)
	case condition.ObservedGeneration != alerter.GetGeneration():
		return r.CreateAlerter(ctx, alerter)
	}

	updated, exists, err := r.reconcileDeployment(ctx, alerter)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !exists {
		return r.CreateAlerter(ctx, alerter)
	}
	if updated {
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}
	if condition.Reason == r.waitReason() {
		return r.IsReady(ctx, alerter)
	}
	return ctrl.Result{}, nil
}

// reconcileDeployment directly compares and updates image, --alerter-address=, and --kusto-endpoint=
// on Deployment/alerter without mutating Alerter.Spec or calling StoreAppliedProvisioningState.
func (r *AlerterReconciler) reconcileDeployment(ctx context.Context, alerter *adxmonv1.Alerter) (updated bool, exists bool, err error) {
	var deployment appsv1.Deployment
	if err := r.Get(ctx, client.ObjectKey{Namespace: alerter.GetNamespace(), Name: "alerter"}, &deployment); err != nil {
		if errors.IsNotFound(err) {
			return false, false, nil
		}
		return false, false, err
	}

	ready, data, err := r.templateData(ctx, alerter)
	if err != nil {
		return false, true, fmt.Errorf("failed to get template data: %w", err)
	}
	if !ready {
		_ = r.setCondition(ctx, alerter, "NotReady", "ADXCluster not ready", metav1.ConditionUnknown)
		return true, true, nil
	}

	if !applyDesiredDeploymentSpec(&deployment, alerter, data) {
		return false, true, nil
	}

	if err := r.Update(ctx, &deployment); err != nil {
		return false, true, fmt.Errorf("failed to update Deployment: %w", err)
	}
	if err := r.setCondition(ctx, alerter, r.waitReason(), "Alerter manifest updating...", metav1.ConditionUnknown); err != nil {
		return false, true, fmt.Errorf("failed to set status condition: %w", err)
	}
	return true, true, nil
}

func applyDesiredDeploymentSpec(deployment *appsv1.Deployment, alerter *adxmonv1.Alerter, data *alerterTemplateData) bool {
	if len(deployment.Spec.Template.Spec.Containers) == 0 {
		return false
	}

	var changed bool
	container := &deployment.Spec.Template.Spec.Containers[0]
	if container.Image != data.Image {
		logger.Infof("Updating image for alerter %s/%s from %s to %s", alerter.Namespace, alerter.Name, container.Image, data.Image)
		container.Image = data.Image
		changed = true
	}

	desiredArgs := desiredAlerterArgs(container.Args, data.AlerterEndpoints, data.KustoEndpoints)
	if !slices.Equal(container.Args, desiredArgs) {
		logger.Infof("Updating args for alerter %s/%s", alerter.Namespace, alerter.Name)
		container.Args = desiredArgs
		changed = true
	}

	if deployment.Spec.Template.Spec.TerminationGracePeriodSeconds == nil ||
		*deployment.Spec.Template.Spec.TerminationGracePeriodSeconds != desiredAlerterTerminationGracePeriodSeconds {
		logger.Infof("Updating terminationGracePeriodSeconds for alerter %s/%s to %d", alerter.Namespace, alerter.Name, desiredAlerterTerminationGracePeriodSeconds)
		deployment.Spec.Template.Spec.TerminationGracePeriodSeconds = new(desiredAlerterTerminationGracePeriodSeconds)
		changed = true
	}

	return changed
}

func desiredAlerterArgs(currentArgs, alerterEndpoints, kustoEndpoints []string) []string {
	preserved := make([]string, 0, len(currentArgs))
	for _, arg := range currentArgs {
		if strings.HasPrefix(arg, "--alerter-address=") || strings.HasPrefix(arg, "--kusto-endpoint=") {
			continue
		}
		preserved = append(preserved, arg)
	}

	desired := make([]string, 0, len(alerterEndpoints)+len(preserved)+len(kustoEndpoints))
	for _, ep := range alerterEndpoints {
		desired = append(desired, fmt.Sprintf("--alerter-address=%s", ep))
	}
	desired = append(desired, preserved...)
	for _, cluster := range kustoEndpoints {
		desired = append(desired, fmt.Sprintf("--kusto-endpoint=%s", cluster))
	}
	return desired
}

func (r *AlerterReconciler) IsReady(ctx context.Context, alerter *adxmonv1.Alerter) (ctrl.Result, error) {
	var deploy appsv1.Deployment
	if err := r.Get(ctx, client.ObjectKey{Namespace: alerter.GetNamespace(), Name: "alerter"}, &deploy); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{RequeueAfter: time.Minute}, nil
		}
		return ctrl.Result{}, err
	}
	if deploy.Spec.Replicas != nil && deploy.Status.ReadyReplicas == *deploy.Spec.Replicas {
		if err := r.setCondition(ctx, alerter, "Ready", "Alerter deployment is ready", metav1.ConditionTrue); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}
	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

func (r *AlerterReconciler) CreateAlerter(ctx context.Context, alerter *adxmonv1.Alerter) (ctrl.Result, error) {
	if _, err := resolvedNotificationEndpoints(alerter.Spec); err != nil {
		_ = r.setCondition(ctx, alerter, "InvalidSpec", err.Error(), metav1.ConditionFalse)
		return ctrl.Result{}, nil
	}

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
		if err := r.Create(ctx, obj); err != nil {
			if !errors.IsAlreadyExists(err) {
				return ctrl.Result{}, fmt.Errorf("failed to create %s %s: %w", obj.GetKind(), obj.GetName(), err)
			}
			if obj.GetKind() == "Deployment" && obj.GetName() == "alerter" {
				var existingDeploy appsv1.Deployment
				if err := r.Get(ctx, client.ObjectKey{Namespace: alerter.GetNamespace(), Name: "alerter"}, &existingDeploy); err != nil {
					return ctrl.Result{}, fmt.Errorf("failed to get existing Deployment alerter: %w", err)
				}
				if applyDesiredDeploymentSpec(&existingDeploy, alerter, data) {
					if err := r.Update(ctx, &existingDeploy); err != nil {
						return ctrl.Result{}, fmt.Errorf("failed to update Deployment alerter: %w", err)
					}
				}
			}
		}
	}

	if err := r.setCondition(ctx, alerter, r.waitReason(), "Alerter manifests installing", metav1.ConditionTrue); err != nil {
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

func desiredAlerterImage(alerter *adxmonv1.Alerter) string {
	if alerter.Spec.Image != "" {
		return alerter.Spec.Image
	}
	return defaultAlerterImage
}

func resolvedNotificationEndpoints(spec adxmonv1.AlerterSpec) ([]string, error) {
	if spec.NotificationEndpoint != "" && len(spec.NotificationEndpoints) > 0 {
		return nil, fmt.Errorf("exactly one of notificationEndpoint or notificationEndpoints must be specified")
	}

	var raw []string
	switch {
	case len(spec.NotificationEndpoints) > 0:
		raw = spec.NotificationEndpoints
	case spec.NotificationEndpoint != "":
		raw = []string{spec.NotificationEndpoint}
	default:
		return nil, nil
	}

	seen := make(map[string]struct{}, len(raw))
	deduped := make([]string, 0, len(raw))
	for _, entry := range raw {
		ep, err := alert.ParseBaseEndpoint(entry)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[ep.URL]; exists {
			continue
		}
		seen[ep.URL] = struct{}{}
		deduped = append(deduped, strings.TrimSpace(entry))
	}
	return deduped, nil
}

type alerterTemplateData struct {
	Image            string
	AlerterEndpoints []string
	KustoEndpoints   []string
	MSIID            string
	Namespace        string
}

func (r *AlerterReconciler) templateData(ctx context.Context, alerter *adxmonv1.Alerter) (clustersReady bool, data *alerterTemplateData, err error) {
	alerterEndpoints, err := resolvedNotificationEndpoints(alerter.Spec)
	if err != nil {
		return false, nil, err
	}

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
	slices.Sort(kustoEndpoints)
	data = &alerterTemplateData{
		Image:            desiredAlerterImage(alerter),
		AlerterEndpoints: alerterEndpoints,
		KustoEndpoints:   kustoEndpoints,
		Namespace:        alerter.Namespace,
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
					Name:      alerter.Name,
					Namespace: alerter.Namespace,
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
