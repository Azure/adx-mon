package operator

import (
	"bytes"
	"context"
	"embed"
	"time"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	ctrl "sigs.k8s.io/controller-runtime"
)

//go:embed manifests/*.yaml manifests/**/*.yaml
var manifestsFS embed.FS

// updateCondition updates the Operator's status condition for the current phase.
func updateCondition(r *Reconciler, ctx context.Context, operator *adxmonv1.Operator, condition metav1.Condition) error {

	shouldUpdate := meta.SetStatusCondition(&operator.Status.Conditions, condition)

	if condition.Status == metav1.ConditionTrue {

		next := advancePhase(condition)
		if meta.SetStatusCondition(&operator.Status.Conditions, next) {
			shouldUpdate = true
		}
	}

	if !shouldUpdate {
		return nil
	}

	return r.Status().Update(ctx, operator)
}

func handleOperatorEvent(ctx context.Context, r *Reconciler, operator *adxmonv1.Operator) (ctrl.Result, error) {
	// Determine current phase from status
	phase := currentPhase(operator)
	if phase == PhaseDone {
		condition := meta.FindStatusCondition(operator.Status.Conditions, adxmonv1.OperatorCommandConditionOwner)
		if condition != nil &&
			condition.Status == metav1.ConditionTrue &&
			condition.ObservedGeneration == operator.GetGeneration() {
			// If the operator is done, we can return
			return ctrl.Result{}, nil
		}

		// If the operator is not done, probably CRD update, reset evaluation
		phase = PhaseUnknown
	}

	switch phase {
	case PhaseEnsureCrds:
		if err := InstallCrds(ctx, r); err != nil {
			return ctrl.Result{}, err
		}
		c := metav1.Condition{
			Type:               adxmonv1.ADXClusterConditionOwner,
			Status:             metav1.ConditionUnknown,
			Reason:             PhaseEnsureKusto.String(),
			Message:            "Ensuring ADX Cluster",
			LastTransitionTime: metav1.NewTime(time.Now()),
		}
		if meta.SetStatusCondition(&operator.Status.Conditions, c) {
			err := r.Status().Update(ctx, operator)
			return ctrl.Result{Requeue: true}, err
		}
		return ctrl.Result{Requeue: true}, nil

	case PhaseEnsureKusto:
		return handleAdxEvent(ctx, r, operator)

	case PhaseEnsureIngestor:
		return handleStatefulSetEvent(ctx, r, nil, operator)

	case PhaseEnsureCollector:
		// TODO: Ensure Collector DaemonSet

		c := metav1.Condition{
			Type:               adxmonv1.CollectorClusterConditionOwner,
			Status:             metav1.ConditionTrue,
			Reason:             PhaseEnsureCollector.String(),
			Message:            "Ensuring Collector DaemonSet",
			LastTransitionTime: metav1.NewTime(time.Now()),
		}
		_ = updateCondition(r, ctx, operator, c)
		return ctrl.Result{Requeue: true}, nil

	case PhaseEnsureAlerter:
		// TODO: Ensure Alerter Deployment

		c := metav1.Condition{
			Type:               adxmonv1.AlerterClusterConditionOwner,
			Status:             metav1.ConditionTrue,
			Reason:             PhaseEnsureAlerter.String(),
			Message:            "Ensuring Alerter Deployment",
			LastTransitionTime: metav1.NewTime(time.Now()),
		}
		_ = updateCondition(r, ctx, operator, c)
		return ctrl.Result{Requeue: true}, nil

	default:
		// Start from the beginning if unknown

		c := metav1.Condition{
			Type:               adxmonv1.OperatorCommandConditionOwner,
			Status:             metav1.ConditionUnknown,
			Reason:             PhaseUnknown.String(),
			Message:            "Starting reconciliation",
			LastTransitionTime: metav1.NewTime(time.Now()),
		}
		_ = updateCondition(r, ctx, operator, c)
		return ctrl.Result{Requeue: true}, nil
	}
}

func clustersAreDone(operator *adxmonv1.Operator) bool {
	condition := meta.FindStatusCondition(operator.Status.Conditions, adxmonv1.ADXClusterConditionOwner)
	if condition != nil &&
		condition.Status == metav1.ConditionTrue &&
		condition.ObservedGeneration == operator.GetGeneration() {
		// If the condition is true, we can assume the cluster is done
		return true
	}

	// If a Kusto cluster already has an endpoint, we denote the cluster as "existing".
	var allExisting bool
	if operator.Spec.ADX != nil {
		for _, cluster := range operator.Spec.ADX.Clusters {
			if cluster.Endpoint == "" {
				return false
			}
			allExisting = true
		}
	}
	if allExisting {
		return true
	}

	// TODO: Check the ASO owned Kusto CRD status

	// If the cluster is not existing, we check if the owner condition is true.
	if condition == nil {
		return false
	}
	if condition.Status != metav1.ConditionTrue {
		return false
	}
	return true
}

func InstallCrds(ctx context.Context, r *Reconciler) error {
	// Install each CRD from manifestsFS under manifests/crds
	entries, err := manifestsFS.ReadDir("manifests/crds")
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		crdBytes, err := manifestsFS.ReadFile("manifests/crds/" + entry.Name())
		if err != nil {
			return err
		}

		// Unmarshal YAML to unstructured.Unstructured
		obj := &unstructured.Unstructured{}
		if err := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(crdBytes), 4096).Decode(obj); err != nil {
			return err
		}

		// Create or update the CRD
		err = r.Client.Create(ctx, obj)
		if err != nil && !errors.IsAlreadyExists(err) {
			return err
		}
	}
	return nil
}
