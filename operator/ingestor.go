package operator

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"text/template"
	"time"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/celutil"
	"github.com/Azure/adx-mon/pkg/logger"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
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

const (
	ingestorSecurityFinalizer = "ingestor.adx-mon.azure.com/security-cleanup"
	ingestorManagedLabelKey   = "adx-mon.azure.com/managed-by"
	ingestorManagedLabelValue = "ingestor-operator"
	ingestorPodRoleName       = "adx-mon-ingestor-pods"
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

	if !ingestor.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.cleanupIngestorSecurity(ctx, ingestor)
	}

	if err := r.reconcileIngestorSecurity(ctx, ingestor); err != nil {
		_ = r.setCondition(ctx, ingestor, "SecurityMigrationFailed", err.Error(), metav1.ConditionFalse)
		return ctrl.Result{}, fmt.Errorf("failed to reconcile ingestor security controls: %w", err)
	}
	if condition := meta.FindStatusCondition(ingestor.Status.Conditions, adxmonv1.IngestorConditionOwner); condition != nil && condition.Reason == "SecurityMigrationFailed" {
		if err := r.setCondition(ctx, ingestor, "SecurityMigrationComplete", "Ingestor security controls are reconciled", metav1.ConditionTrue); err != nil {
			return ctrl.Result{}, err
		}
	}

	if expr := ingestor.Spec.CriteriaExpression; expr != "" {
		labels := getOperatorClusterLabels()
		ok, err := celutil.EvaluateCriteriaExpression(labels, expr)
		if err != nil {
			logger.Errorf("Ingestor %s/%s criteriaExpression error: %v", req.Namespace, req.Name, err)
			// Expression errors are terminal until the CRD changes; set status and exit without requeue.
			c := metav1.Condition{Type: adxmonv1.IngestorConditionOwner, Status: metav1.ConditionFalse, Reason: "CriteriaExpressionError", Message: err.Error(), ObservedGeneration: ingestor.GetGeneration(), LastTransitionTime: metav1.Now()}
			if meta.SetStatusCondition(&ingestor.Status.Conditions, c) {
				_ = r.Status().Update(ctx, ingestor)
			}
			return ctrl.Result{}, nil
		}
		if !ok {
			c := metav1.Condition{Type: adxmonv1.IngestorConditionOwner, Status: metav1.ConditionFalse, Reason: "CriteriaExpressionFalse", Message: "criteriaExpression evaluated to false; skipping", ObservedGeneration: ingestor.GetGeneration(), LastTransitionTime: metav1.Now()}
			if meta.SetStatusCondition(&ingestor.Status.Conditions, c) {
				_ = r.Status().Update(ctx, ingestor)
			}
			return ctrl.Result{}, nil
		}
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

func (r *IngestorReconciler) reconcileIngestorSecurity(ctx context.Context, ingestor *adxmonv1.Ingestor) error {
	namespace := ingestor.Namespace
	clusterRoleName := namespace + ":ingestor"

	// Do not provision resources for a new Ingestor that may be excluded by its criteria.
	// Existing installations always have either this ClusterRole or the StatefulSet.
	existingClusterRole := &rbacv1.ClusterRole{}
	if err := r.Get(ctx, client.ObjectKey{Name: clusterRoleName}, existingClusterRole); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		existingStatefulSet := &appsv1.StatefulSet{}
		if err := r.Get(ctx, client.ObjectKey{Name: "ingestor", Namespace: namespace}, existingStatefulSet); err != nil {
			if errors.IsNotFound(err) {
				return nil
			}
			return err
		}
	}

	// Revoke the vulnerable cluster-wide core API permissions before any additive
	// migration step that could fail.
	clusterRole := desiredIngestorClusterRole(namespace)
	if err := r.reconcileClusterRole(ctx, clusterRole); err != nil {
		return err
	}

	if !controllerutil.ContainsFinalizer(ingestor, ingestorSecurityFinalizer) {
		controllerutil.AddFinalizer(ingestor, ingestorSecurityFinalizer)
		if err := r.Update(ctx, ingestor); err != nil {
			return err
		}
	}

	clusterRoleBinding := desiredIngestorClusterRoleBinding(namespace)
	if err := r.reconcileClusterRoleBinding(ctx, clusterRoleBinding); err != nil {
		return err
	}

	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: ingestorPodRoleName, Namespace: namespace},
		Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{""},
			Resources: []string{"pods"},
			Verbs:     []string{"get", "list", "watch", "patch"},
		}},
	}
	if err := controllerutil.SetControllerReference(ingestor, role, r.Scheme); err != nil {
		return err
	}
	if err := r.reconcileRole(ctx, role); err != nil {
		return err
	}

	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: ingestorPodRoleName, Namespace: namespace},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     ingestorPodRoleName,
		},
		Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: "ingestor", Namespace: namespace}},
	}
	if err := controllerutil.SetControllerReference(ingestor, roleBinding, r.Scheme); err != nil {
		return err
	}
	if err := r.reconcileRoleBinding(ctx, roleBinding); err != nil {
		return err
	}

	return r.reconcileIngestorTokenProjection(ctx, namespace)
}

func desiredIngestorClusterRole(namespace string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: namespace + ":ingestor", Labels: map[string]string{ingestorManagedLabelKey: ingestorManagedLabelValue}},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{"adx-mon.azure.com"}, Resources: []string{"functions"}, Verbs: []string{"get", "list", "update"}},
			{APIGroups: []string{"adx-mon.azure.com"}, Resources: []string{"managementcommands", "summaryrules"}, Verbs: []string{"get", "list"}},
			{APIGroups: []string{"adx-mon.azure.com"}, Resources: []string{"functions/status", "managementcommands/status", "summaryrules/status"}, Verbs: []string{"update"}},
			{APIGroups: []string{"adx-mon.azure.com"}, Resources: []string{"functions/finalizers"}, Verbs: []string{"update"}},
		},
	}
}

func desiredIngestorClusterRoleBinding(namespace string) *rbacv1.ClusterRoleBinding {
	name := namespace + ":ingestor"
	return &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{ingestorManagedLabelKey: ingestorManagedLabelValue}},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: name},
		Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "ingestor", Namespace: namespace}},
	}
}

func (r *IngestorReconciler) reconcileRole(ctx context.Context, desired *rbacv1.Role) error {
	existing := &rbacv1.Role{}
	key := client.ObjectKeyFromObject(desired)
	if err := r.Get(ctx, key, existing); err != nil {
		if errors.IsNotFound(err) {
			return r.Create(ctx, desired)
		}
		return err
	}
	if reflect.DeepEqual(existing.Rules, desired.Rules) && reflect.DeepEqual(existing.OwnerReferences, desired.OwnerReferences) {
		return nil
	}
	existing.Rules = desired.Rules
	existing.OwnerReferences = desired.OwnerReferences
	return r.Update(ctx, existing)
}

func (r *IngestorReconciler) reconcileRoleBinding(ctx context.Context, desired *rbacv1.RoleBinding) error {
	existing := &rbacv1.RoleBinding{}
	key := client.ObjectKeyFromObject(desired)
	if err := r.Get(ctx, key, existing); err != nil {
		if errors.IsNotFound(err) {
			return r.Create(ctx, desired)
		}
		return err
	}
	if !reflect.DeepEqual(existing.RoleRef, desired.RoleRef) {
		if err := r.Delete(ctx, existing); err != nil {
			return err
		}
		return r.Create(ctx, desired)
	}
	if reflect.DeepEqual(existing.Subjects, desired.Subjects) && reflect.DeepEqual(existing.OwnerReferences, desired.OwnerReferences) {
		return nil
	}
	existing.Subjects = desired.Subjects
	existing.OwnerReferences = desired.OwnerReferences
	return r.Update(ctx, existing)
}

func (r *IngestorReconciler) reconcileClusterRole(ctx context.Context, desired *rbacv1.ClusterRole) error {
	existing := &rbacv1.ClusterRole{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(desired), existing); err != nil {
		if errors.IsNotFound(err) {
			return r.Create(ctx, desired)
		}
		return err
	}
	if reflect.DeepEqual(existing.Rules, desired.Rules) && reflect.DeepEqual(existing.Labels, desired.Labels) {
		return nil
	}
	existing.Rules = desired.Rules
	existing.Labels = desired.Labels
	return r.Update(ctx, existing)
}

func (r *IngestorReconciler) reconcileClusterRoleBinding(ctx context.Context, desired *rbacv1.ClusterRoleBinding) error {
	existing := &rbacv1.ClusterRoleBinding{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(desired), existing); err != nil {
		if errors.IsNotFound(err) {
			return r.Create(ctx, desired)
		}
		return err
	}
	if !reflect.DeepEqual(existing.RoleRef, desired.RoleRef) {
		if err := r.Delete(ctx, existing); err != nil {
			return err
		}
		return r.Create(ctx, desired)
	}
	if reflect.DeepEqual(existing.Subjects, desired.Subjects) && reflect.DeepEqual(existing.Labels, desired.Labels) {
		return nil
	}
	existing.Subjects = desired.Subjects
	existing.Labels = desired.Labels
	return r.Update(ctx, existing)
}

func (r *IngestorReconciler) cleanupIngestorSecurity(ctx context.Context, ingestor *adxmonv1.Ingestor) error {
	if !controllerutil.ContainsFinalizer(ingestor, ingestorSecurityFinalizer) {
		return nil
	}
	name := ingestor.Namespace + ":ingestor"
	for _, obj := range []client.Object{
		&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: name}},
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: name}},
	} {
		if err := r.Get(ctx, client.ObjectKey{Name: name}, obj); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return err
		}
		if obj.GetLabels()[ingestorManagedLabelKey] != ingestorManagedLabelValue {
			continue
		}
		if err := r.Delete(ctx, obj); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}
	controllerutil.RemoveFinalizer(ingestor, ingestorSecurityFinalizer)
	return r.Update(ctx, ingestor)
}

func (r *IngestorReconciler) reconcileIngestorTokenProjection(ctx context.Context, namespace string) error {
	sts := &appsv1.StatefulSet{}
	if err := r.Get(ctx, client.ObjectKey{Name: "ingestor", Namespace: namespace}, sts); err != nil {
		return client.IgnoreNotFound(err)
	}

	desiredVolume := ingestorKubeAPIAccessVolume()
	changed := sts.Spec.Template.Spec.AutomountServiceAccountToken == nil || *sts.Spec.Template.Spec.AutomountServiceAccountToken
	sts.Spec.Template.Spec.AutomountServiceAccountToken = boolPtr(false)

	volumes := make([]corev1.Volume, 0, len(sts.Spec.Template.Spec.Volumes)+1)
	for _, volume := range sts.Spec.Template.Spec.Volumes {
		if volume.Name != desiredVolume.Name {
			volumes = append(volumes, volume)
		}
	}
	volumes = append(volumes, desiredVolume)
	if !reflect.DeepEqual(sts.Spec.Template.Spec.Volumes, volumes) {
		sts.Spec.Template.Spec.Volumes = volumes
		changed = true
	}

	for i := range sts.Spec.Template.Spec.Containers {
		if sts.Spec.Template.Spec.Containers[i].Name != "ingestor" {
			continue
		}
		mounts := make([]corev1.VolumeMount, 0, len(sts.Spec.Template.Spec.Containers[i].VolumeMounts)+1)
		for _, mount := range sts.Spec.Template.Spec.Containers[i].VolumeMounts {
			if mount.Name != "kube-api-access" && mount.MountPath != "/var/run/secrets/kubernetes.io/serviceaccount" {
				mounts = append(mounts, mount)
			}
		}
		mounts = append(mounts, corev1.VolumeMount{Name: "kube-api-access", MountPath: "/var/run/secrets/kubernetes.io/serviceaccount", ReadOnly: true})
		if !reflect.DeepEqual(sts.Spec.Template.Spec.Containers[i].VolumeMounts, mounts) {
			sts.Spec.Template.Spec.Containers[i].VolumeMounts = mounts
			changed = true
		}
	}

	if changed {
		if err := r.Update(ctx, sts); err != nil {
			return err
		}
	}
	if sts.Spec.UpdateStrategy.Type == appsv1.OnDeleteStatefulSetStrategyType {
		return fmt.Errorf("StatefulSet uses OnDelete update strategy; delete existing ingestor pods to complete the projected-token migration")
	}
	if !changed && sts.Status.UpdateRevision != "" && sts.Status.CurrentRevision != sts.Status.UpdateRevision {
		return fmt.Errorf("StatefulSet rollout is still replacing pods that used automatically mounted credentials")
	}
	return nil
}

func ingestorKubeAPIAccessVolume() corev1.Volume {
	defaultMode := int32(420)
	expirationSeconds := int64(3600)
	return corev1.Volume{
		Name: "kube-api-access",
		VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{
			DefaultMode: &defaultMode,
			Sources: []corev1.VolumeProjection{
				{ServiceAccountToken: &corev1.ServiceAccountTokenProjection{ExpirationSeconds: &expirationSeconds, Path: "token"}},
				{ConfigMap: &corev1.ConfigMapProjection{
					LocalObjectReference: corev1.LocalObjectReference{Name: "kube-root-ca.crt"},
					Items:                []corev1.KeyToPath{{Key: "ca.crt", Path: "ca.crt"}},
				}},
				{DownwardAPI: &corev1.DownwardAPIProjection{Items: []corev1.DownwardAPIVolumeFile{{
					Path:     "namespace",
					FieldRef: &corev1.ObjectFieldSelector{APIVersion: "v1", FieldPath: "metadata.namespace"},
				}}}},
			},
		}},
	}
}

func boolPtr(value bool) *bool {
	return &value
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
		if err := r.setCondition(ctx, ingestor, "Ready", "All ingestor replicas are ready", metav1.ConditionTrue); err != nil {
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
	storedSel, _ := json.Marshal(stored.ADXClusterSelector)
	currentSel, _ := json.Marshal(ingestor.Spec.ADXClusterSelector)
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
	r.waitForReadyReason = "WaitForReady"

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

				if err := r.setCondition(ctx, &ingestor, "ADXClusterChanged", fmt.Sprintf("ADXCluster %s/%s changed", cluster.Namespace, cluster.Name), metav1.ConditionUnknown); err != nil {
					logger.Errorf("Failed to set condition for Ingestor %s/%s: %v", ingestor.Namespace, ingestor.Name, err)
				}
			}
		}
		return requests
	}
	securityMapFn := func(ctx context.Context, obj client.Object) []reconcile.Request {
		name := obj.GetName()
		if !strings.HasSuffix(name, ":ingestor") {
			return nil
		}
		separator := strings.LastIndex(name, ":ingestor")
		if separator <= 0 {
			return nil
		}
		namespace := name[:separator]
		var ingestors adxmonv1.IngestorList
		if err := r.List(ctx, &ingestors, client.InNamespace(namespace)); err != nil {
			logger.Errorf("Failed to list Ingestors in namespace %s for RBAC drift event: %v", namespace, err)
			return nil
		}
		requests := make([]reconcile.Request, 0, len(ingestors.Items))
		for _, ingestor := range ingestors.Items {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&ingestor)})
		}
		return requests
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&adxmonv1.Ingestor{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		// Add Watches for ADXCluster changes
		Watches(
			&adxmonv1.ADXCluster{},
			handler.EnqueueRequestsFromMapFunc(mapFn),
		).
		Watches(&rbacv1.ClusterRole{}, handler.EnqueueRequestsFromMapFunc(securityMapFn)).
		Watches(&rbacv1.ClusterRoleBinding{}, handler.EnqueueRequestsFromMapFunc(securityMapFn)).
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
	if err := r.setCondition(ctx, ingestor, "CRDsInstalled", "CRDs installed successfully", metav1.ConditionUnknown); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to set status condition: %w", err)
	}

	// Render the ingestor manifest
	tmplBytes, err := ingestorCrdsFS.ReadFile("manifests/ingestor.yaml")
	if err != nil {
		// This is a terminal condition because a retry will not help.
		if err := r.setCondition(ctx, ingestor, "TemplateError", "Failed to read ingestor template", metav1.ConditionFalse); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil // No need to retry
	}
	tmpl, err := template.New("ingestor").Parse(string(tmplBytes))
	if err != nil {
		// This is a terminal condition because a retry will not help.
		if err := r.setCondition(ctx, ingestor, "TemplateError", "Failed to parse ingestor template", metav1.ConditionFalse); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil // No need to retry
	}

	ready, data, err := r.templateData(ctx, ingestor)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get template data: %w", err)
	}
	if !ready {
		if err := r.setCondition(ctx, ingestor, "NotReady", "ADXCluster not ready", metav1.ConditionUnknown); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
	}

	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, data); err != nil {
		// This is a terminal condition because a retry will not help.
		if err := r.setCondition(ctx, ingestor, "TemplateError", "Failed to render ingestor template", metav1.ConditionFalse); err != nil {
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
	// List ADXClusters matching the selector
	selector, err := metav1.LabelSelectorAsSelector(ingestor.Spec.ADXClusterSelector)
	if err != nil {
		return false, nil, fmt.Errorf("failed to convert label selector: %w", err)
	}

	var adxClusterList adxmonv1.ADXClusterList
	listOpts := []client.ListOption{}
	if ingestor.Spec.ADXClusterSelector != nil {
		listOpts = append(listOpts, client.MatchingLabelsSelector{Selector: selector})
	}
	if err := r.Client.List(ctx, &adxClusterList, listOpts...); err != nil {
		return false, nil, fmt.Errorf("failed to list ADXClusters: %w", err)
	}

	var metricsClusters []string
	var logsClusters []string
	for _, cluster := range adxClusterList.Items {
		// wait for the cluster to be ready
		if !meta.IsStatusConditionTrue(cluster.Status.Conditions, adxmonv1.ADXClusterConditionOwner) {
			// Cluster is not ready
			return false, nil, fmt.Errorf("ADXCluster is not ready")
		}

		endpoint := resolvedClusterEndpoint(&cluster)

		for _, db := range cluster.Spec.Databases {
			if db.TelemetryType == adxmonv1.DatabaseTelemetryMetrics {
				if endpoint != "" {
					metricsClusters = append(metricsClusters, fmt.Sprintf("%s=%s", db.DatabaseName, endpoint))
				}
			}
			if db.TelemetryType == adxmonv1.DatabaseTelemetryLogs {
				if endpoint != "" {
					logsClusters = append(logsClusters, fmt.Sprintf("%s=%s", db.DatabaseName, endpoint))
				}
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
