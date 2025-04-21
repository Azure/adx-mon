package operator

import (
	"bytes"
	"context"
	"fmt"
	"text/template"
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

// ResourceHandler contains configuration for handling Kubernetes resources
type ResourceHandler struct {
	// Name of the resource (e.g., "collector", "ingestor", "alerter")
	Name string
	// The condition type to use (e.g., adxmonv1.CollectorClusterConditionOwner)
	ConditionType string
	// Path to the manifest template (e.g., "manifests/collector.yaml")
	ManifestPath string
	// TemplateData builds template data from operator spec
	TemplateData func(*adxmonv1.Operator) interface{}
	// Type of resource being managed (StatefulSet, DaemonSet, Deployment)
	ResourceKind string
	// Function to check if resource is ready
	IsReady func(obj client.Object) bool
	// Function to update resource spec if needed, returns true if updated
	UpdateSpec func(obj client.Object, operator *adxmonv1.Operator) bool
	// Function to update operator with resource information after successful creation/update
	UpdateOperator func(obj client.Object, operator *adxmonv1.Operator) bool
}

// HandleEvent is the main entry point for handling resource events
func (h *ResourceHandler) HandleEvent(ctx context.Context, r *Reconciler, obj client.Object, operator *adxmonv1.Operator) (ctrl.Result, error) {
	if operator != nil {
		if h.shouldInstall(operator) {
			return h.ensureResource(ctx, r, operator)
		}
		return h.ensureInstalled(ctx, r, operator)
	}

	return h.ensureSpec(ctx, r, obj)
}

func (h *ResourceHandler) shouldInstall(operator *adxmonv1.Operator) bool {
	condition := meta.FindStatusCondition(operator.Status.Conditions, h.ConditionType)
	return condition == nil ||
		condition.Reason == string(adxmonv1.OperatorServiceReasonNotInstalled) ||
		condition.Reason == "" ||
		condition.ObservedGeneration != operator.GetGeneration()
}

func (h *ResourceHandler) setCondition(ctx context.Context, r *Reconciler, operator *adxmonv1.Operator, reason adxmonv1.OperatorServiceReason, err error, isTerminal bool) error {
	cond := metav1.Condition{
		Type:               h.ConditionType,
		Status:             metav1.ConditionUnknown,
		Reason:             string(reason),
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: operator.GetGeneration(),
	}

	switch {
	case err != nil && isTerminal:
		cond.Status = metav1.ConditionFalse
		cond.Reason = string(adxmonv1.OperatorServiceTerminalError)
		cond.Message = err.Error()
	case err != nil:
		cond.Status = metav1.ConditionUnknown
		cond.Reason = string(adxmonv1.OperatorServiceReasonInstalling)
		cond.Message = err.Error()
	case reason == adxmonv1.OperatorServiceReasonInstalled:
		cond.Status = metav1.ConditionTrue
		cond.Reason = string(adxmonv1.OperatorServiceReasonInstalled)
	}

	if !meta.SetStatusCondition(&operator.Status.Conditions, cond) {
		return nil
	}

	if err := r.Status().Update(ctx, operator); err != nil {
		logger.Errorf("Failed to update %s condition: %v", h.Name, err)
		return err
	}
	return nil
}

func (h *ResourceHandler) ensureSpec(ctx context.Context, r *Reconciler, obj client.Object) (ctrl.Result, error) {
	var operator adxmonv1.Operator
	err := r.Get(ctx, client.ObjectKey{
		Name:      obj.GetNamespace(),
		Namespace: obj.GetNamespace(),
	}, &operator)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get Operator CRD: %w", err)
	}

	if updated := h.UpdateSpec(obj, &operator); updated {
		err := r.Update(ctx, obj)
		if err != nil {
			err = fmt.Errorf("failed to update %s: %w", h.ResourceKind, err)
			return ctrl.Result{}, h.setCondition(ctx, r, &operator, adxmonv1.OperatorServiceReasonDrifted, err, false)
		}
		return ctrl.Result{RequeueAfter: time.Minute}, h.setCondition(ctx, r, &operator, adxmonv1.OperatorServiceReasonInstalled, nil, false)
	}

	return ctrl.Result{Requeue: true}, nil
}

func (h *ResourceHandler) ensureInstalled(ctx context.Context, r *Reconciler, operator *adxmonv1.Operator) (ctrl.Result, error) {
	obj := &unstructured.Unstructured{}
	obj.SetKind(h.ResourceKind)
	obj.SetAPIVersion("apps/v1")

	err := r.Get(ctx, client.ObjectKey{
		Name:      h.Name,
		Namespace: operator.Namespace,
	}, obj)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			return ctrl.Result{}, h.setCondition(ctx, r, operator, adxmonv1.OperatorServiceReasonNotInstalled, nil, false)
		}
		return ctrl.Result{}, err
	}

	if updated := h.UpdateSpec(obj, operator); updated {
		err := r.Update(ctx, obj)
		if err != nil {
			return ctrl.Result{}, h.setCondition(ctx, r, operator, adxmonv1.OperatorServiceReasonDrifted, err, false)
		}
		return ctrl.Result{RequeueAfter: time.Minute}, h.setCondition(ctx, r, operator, adxmonv1.OperatorServiceReasonInstalled, nil, false)
	}

	if !h.IsReady(obj) {
		return ctrl.Result{RequeueAfter: time.Minute}, h.setCondition(ctx, r, operator, adxmonv1.OperatorServiceReasonInstalling, nil, false)
	}

	// Update operator with resource information if needed
	if h.UpdateOperator != nil && h.UpdateOperator(obj, operator) {
		if err := r.Update(ctx, operator); err != nil {
			logger.Errorf("Failed to update operator with %s information: %v", h.Name, err)
			return ctrl.Result{}, err
		}
		// Get a fresh copy of the operator
		updatedOperator := &adxmonv1.Operator{}
		if err := r.Get(ctx, client.ObjectKey{Name: operator.Name, Namespace: operator.Namespace}, updatedOperator); err != nil {
			logger.Errorf("Failed to get fresh copy of operator: %v", err)
			return ctrl.Result{}, err
		}
		// Update status with new operator
		return ctrl.Result{Requeue: true}, h.setCondition(ctx, r, updatedOperator, adxmonv1.OperatorServiceReasonInstalled, nil, false)
	}

	return ctrl.Result{Requeue: true}, h.setCondition(ctx, r, operator, adxmonv1.OperatorServiceReasonInstalled, nil, false)
}

func (h *ResourceHandler) ensureResource(ctx context.Context, r *Reconciler, operator *adxmonv1.Operator) (ctrl.Result, error) {
	obj := &unstructured.Unstructured{}
	obj.SetKind(h.ResourceKind)
	obj.SetAPIVersion("apps/v1")

	err := r.Get(ctx, client.ObjectKey{
		Name:      h.Name,
		Namespace: operator.Namespace,
	}, obj)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			templateData := h.TemplateData(operator)

			tmplBytes, err := manifestsFS.ReadFile(h.ManifestPath)
			if err != nil {
				err = fmt.Errorf("failed to read %s template: %w", h.Name, err)
				return ctrl.Result{}, h.setCondition(ctx, r, operator, adxmonv1.OperatorServiceReasonInstalling, err, true)
			}

			tmpl, err := template.New(h.Name).Parse(string(tmplBytes))
			if err != nil {
				err = fmt.Errorf("failed to parse %s template: %w", h.Name, err)
				return ctrl.Result{}, h.setCondition(ctx, r, operator, adxmonv1.OperatorServiceReasonInstalling, err, true)
			}

			var rendered bytes.Buffer
			if err := tmpl.Execute(&rendered, templateData); err != nil {
				err = fmt.Errorf("failed to render %s: %w", h.Name, err)
				return ctrl.Result{}, h.setCondition(ctx, r, operator, adxmonv1.OperatorServiceReasonInstalling, err, true)
			}

			decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(rendered.Bytes()), 4096)
			for {
				obj := &unstructured.Unstructured{}
				err := decoder.Decode(obj)
				if err != nil {
					if err.Error() == "EOF" {
						break
					}
					err = fmt.Errorf("failed to decode manifest: %w", err)
					return ctrl.Result{}, h.setCondition(ctx, r, operator, adxmonv1.OperatorServiceReasonInstalling, err, true)
				}
				if obj.Object == nil || obj.GetKind() == "" {
					continue
				}
				if err := r.Create(ctx, obj); err != nil && !errors.IsAlreadyExists(err) {
					err = fmt.Errorf("failed to create %s: %w", obj.GetKind(), err)
					return ctrl.Result{}, h.setCondition(ctx, r, operator, adxmonv1.OperatorServiceReasonInstalling, err, false)
				}
			}
			return ctrl.Result{RequeueAfter: time.Minute}, h.setCondition(ctx, r, operator, adxmonv1.OperatorServiceReasonInstalling, nil, false)
		}
		return ctrl.Result{}, err
	}

	if !h.IsReady(obj) {
		return ctrl.Result{RequeueAfter: time.Minute}, h.setCondition(ctx, r, operator, adxmonv1.OperatorServiceReasonInstalling, nil, false)
	}

	return ctrl.Result{Requeue: true}, h.setCondition(ctx, r, operator, adxmonv1.OperatorServiceReasonInstalled, nil, false)
}

// Helper functions to check readiness for different resource types
func StatefulSetIsReady(obj client.Object) bool {
	sts := &appsv1.StatefulSet{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.(*unstructured.Unstructured).Object, sts); err != nil {
		return false
	}
	return sts.Status.ReadyReplicas == *sts.Spec.Replicas
}

func DaemonSetIsReady(obj client.Object) bool {
	ds := &appsv1.DaemonSet{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.(*unstructured.Unstructured).Object, ds); err != nil {
		return false
	}
	return ds.Status.NumberReady == ds.Status.DesiredNumberScheduled
}

func DeploymentIsReady(obj client.Object) bool {
	deploy := &appsv1.Deployment{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.(*unstructured.Unstructured).Object, deploy); err != nil {
		return false
	}
	return deploy.Status.ReadyReplicas == *deploy.Spec.Replicas
}
