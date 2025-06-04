package operator

import (
	"bytes"
	"context"
	"embed"
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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

//go:embed manifests/collector.yaml
var collectorManifestFS embed.FS

type CollectorReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	waitForReadyReason string
}

func (r *CollectorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	collector := &adxmonv1.Collector{}
	if err := r.Get(ctx, req.NamespacedName, collector); err != nil {
		return r.ReconcileComponent(ctx, req)
	}

	if !collector.DeletionTimestamp.IsZero() {
		logger.Infof("Collector %s/%s is being deleted, skipping reconciliation", collector.Namespace, collector.Name)
		return ctrl.Result{}, nil
	}

	condition := meta.FindStatusCondition(collector.Status.Conditions, adxmonv1.CollectorConditionOwner)
	switch {
	case condition == nil:
		// First time reconciliation
		return r.CreateCollector(ctx, collector)

	case condition.Reason == r.waitForReadyReason:
		// Collector is installing, check if it's ready
		return r.IsReady(ctx, collector)

	case condition.Status == metav1.ConditionUnknown:
		// Retry installation of collector manifests
		return r.CreateCollector(ctx, collector)

	case condition.ObservedGeneration != collector.GetGeneration():
		// CRD has been updated, re-render the collector manifests
		return r.CreateCollector(ctx, collector)
	}

	return ctrl.Result{}, nil
}

func (r *CollectorReconciler) IsReady(ctx context.Context, collector *adxmonv1.Collector) (ctrl.Result, error) {
	var ds appsv1.DaemonSet
	if err := r.Get(ctx, client.ObjectKey{Namespace: collector.GetNamespace(), Name: "collector"}, &ds); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{RequeueAfter: time.Minute}, nil
		}
		return ctrl.Result{}, err
	}

	if ds.Status.NumberReady == ds.Status.DesiredNumberScheduled {
		if err := r.setCondition(ctx, collector, "Ready", "All collector replicas are ready", metav1.ConditionTrue); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

func (r *CollectorReconciler) ReconcileComponent(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var ds appsv1.DaemonSet
	if err := r.Get(ctx, req.NamespacedName, &ds); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Fetch the Collector CRD
	collector := &adxmonv1.Collector{}
	if err := r.Get(ctx, req.NamespacedName, collector); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	var update bool

	// Update image if needed
	if r.updateImageIfNeeded(&ds, collector) {
		update = true
	}

	if update {
		if err := r.Update(ctx, &ds); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update DaemonSet: %w", err)
		}
		logger.Infof("Updated collector DaemonSet %s/%s", ds.Namespace, ds.Name)
	}

	return ctrl.Result{}, nil
}

func (r *CollectorReconciler) updateImageIfNeeded(ds *appsv1.DaemonSet, collector *adxmonv1.Collector) bool {
	r.applyDefaults(collector)
	
	currentImage := ds.Spec.Template.Spec.Containers[0].Image
	if currentImage != collector.Spec.Image {
		logger.Infof("Updating image for collector %s/%s from %s to %s",
			collector.Namespace, collector.Name, currentImage, collector.Spec.Image)
		ds.Spec.Template.Spec.Containers[0].Image = collector.Spec.Image
		return true
	}
	return false
}

func (r *CollectorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.waitForReadyReason = "WaitForReady"

	return ctrl.NewControllerManagedBy(mgr).
		For(&adxmonv1.Collector{}).
		Owns(&appsv1.DaemonSet{}).
		Complete(r)
}

func (r *CollectorReconciler) CreateCollector(ctx context.Context, collector *adxmonv1.Collector) (ctrl.Result, error) {
	r.applyDefaults(collector)
	if err := r.Update(ctx, collector); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update collector: %w", err)
	}

	// Render the collector manifest
	tmplBytes, err := collectorManifestFS.ReadFile("manifests/collector.yaml")
	if err != nil {
		// This is a terminal condition because a retry will not help.
		if err := r.setCondition(ctx, collector, "TemplateError", "Failed to read collector template", metav1.ConditionFalse); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil // No need to retry
	}
	tmpl, err := template.New("collector").Parse(string(tmplBytes))
	if err != nil {
		// This is a terminal condition because a retry will not help.
		if err := r.setCondition(ctx, collector, "TemplateError", "Failed to parse collector template", metav1.ConditionFalse); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil // No need to retry
	}

	data := r.templateData(collector)

	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, data); err != nil {
		// This is a terminal condition because a retry will not help.
		if err := r.setCondition(ctx, collector, "TemplateError", "Failed to render collector template", metav1.ConditionFalse); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil // No need to retry
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
		// Set the owner reference, this enables garbage collection for the collector
		// and ensures that the collector is deleted when the owner is deleted.
		// --> Only set owner reference if the object is namespace-scoped.
		if obj.GetNamespace() != "" {
			if err := controllerutil.SetControllerReference(collector, obj, r.Scheme); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to set controller reference: %w", err)
			}
		} else {
			logger.Infof("Skipping owner reference for cluster-scoped resource %s/%s", obj.GetKind(), obj.GetName())
		}

		if err := r.Create(ctx, obj); err != nil && !errors.IsAlreadyExists(err) {
			return ctrl.Result{}, fmt.Errorf("failed to create %s %s: %w", obj.GetKind(), obj.GetName(), err)
		}
	}

	if err := r.setCondition(ctx, collector, r.waitForReadyReason, "Collector manifests installing", metav1.ConditionTrue); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to set status condition: %w", err)
	}
	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

func (r *CollectorReconciler) applyDefaults(collector *adxmonv1.Collector) {
	if collector.Spec.Image == "" {
		collector.Spec.Image = "ghcr.io/azure/adx-mon/collector:latest"
	}
	if collector.Spec.IngestorEndpoint == "" {
		// Auto-configure to use ingestor service in the same namespace
		collector.Spec.IngestorEndpoint = fmt.Sprintf("https://ingestor.%s.svc.cluster.local", collector.Namespace)
	}
}

type collectorTemplateData struct {
	Image           string
	IngestorEndpoint string
	Namespace       string
}

func (r *CollectorReconciler) templateData(collector *adxmonv1.Collector) *collectorTemplateData {
	return &collectorTemplateData{
		Image:           collector.Spec.Image,
		IngestorEndpoint: collector.Spec.IngestorEndpoint,
		Namespace:       collector.Namespace,
	}
}

func (r *CollectorReconciler) setCondition(ctx context.Context, collector *adxmonv1.Collector, reason, message string, status metav1.ConditionStatus) error {
	condition := metav1.Condition{
		Type:               adxmonv1.CollectorConditionOwner,
		Status:             status,
		ObservedGeneration: collector.GetGeneration(),
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}
	if meta.SetStatusCondition(&collector.Status.Conditions, condition) {
		if err := r.Status().Update(ctx, collector); err != nil {
			return fmt.Errorf("failed to update status: %w", err)
		}
	}
	return nil
}