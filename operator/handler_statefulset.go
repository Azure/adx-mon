package operator

import (
	"bytes"
	"context"
	"fmt"
	"text/template"
	"time"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	msgInstallingIngestor = "Installing ingestor"
	msgInstalledIngestor  = "Ingestor installed"
)

func handleStatefulSetEvent(ctx context.Context, r *Reconciler, sts *appsv1.StatefulSet, operator *adxmonv1.Operator) (ctrl.Result, error) {
	if operator != nil {
		if shouldInstallIngestor(operator) {
			c := metav1.Condition{
				Type:               adxmonv1.IngestorClusterConditionOwner,
				Status:             metav1.ConditionTrue,
				Reason:             PhaseEnsureIngestor.String(),
				Message:            msgInstallingIngestor,
				LastTransitionTime: metav1.NewTime(time.Now()),
			}
			if err := EnsureIngestor(ctx, r, operator); err != nil {
				c.Status = metav1.ConditionFalse
				c.Message = err.Error()
			}
			_ = updateCondition(r, ctx, operator, c)
			return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, nil
		}
		// Ingestor is already installed, check if it is running
		result, err := EnsureIngestorInstalled(ctx, r, operator)
		if err == nil {
			// If the Ingestor is installed, update the condition to true
			c := metav1.Condition{
				Type:               adxmonv1.IngestorClusterConditionOwner,
				Status:             metav1.ConditionTrue,
				Reason:             PhaseEnsureIngestor.String(),
				Message:            msgInstalledIngestor,
				LastTransitionTime: metav1.NewTime(time.Now()),
			}
			_ = updateCondition(r, ctx, operator, c)
			return result, nil
		}
		return result, err
	}

	// TODO handle sts drift
	return ctrl.Result{}, nil
}

func shouldInstallIngestor(operator *adxmonv1.Operator) bool {
	condition := meta.FindStatusCondition(operator.Status.Conditions, adxmonv1.IngestorClusterConditionOwner)
	return condition != nil && condition.Status == metav1.ConditionUnknown && condition.Message != msgInstallingIngestor
}

func EnsureIngestorInstalled(ctx context.Context, r *Reconciler, operator *adxmonv1.Operator) (ctrl.Result, error) {
	// Check if the Ingestor is installed by looking for the StatefulSet
	var sts appsv1.StatefulSet
	err := r.Get(ctx, client.ObjectKey{
		Name:      "ingestor",
		Namespace: operator.Namespace,
	}, &sts)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			// If the StatefulSet is not found, it means the Ingestor is not installed
			return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, nil
		}
		return ctrl.Result{}, err
	}

	// If the StatefulSet is found, we need to check if all its replicas are running
	if sts.Status.ReadyReplicas != *sts.Spec.Replicas {
		return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, nil
	}

	return ctrl.Result{}, nil
}

func EnsureIngestor(ctx context.Context, r *Reconciler, operator *adxmonv1.Operator) error {
	// Check if the Ingestor is installed by looking for the StatefulSet
	var sts appsv1.StatefulSet
	err := r.Get(ctx, client.ObjectKey{
		Name:      "ingestor",
		Namespace: operator.Namespace,
	}, &sts)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			// If the StatefulSet is not found, it means the Ingestor is not installed

			// Populate IngestorSS from operator
			var ingestorSS IngestorSS
			ingestorSS.FromOperator(operator)

			// Render the template from embedded FS
			tmplBytes, err := manifestsFS.ReadFile("manifests/ingestor.yaml")
			if err != nil {
				return fmt.Errorf("failed to read embedded ingestor.yaml: %w", err)
			}
			tmpl, err := template.New("ingestor").Parse(string(tmplBytes))
			if err != nil {
				return fmt.Errorf("failed to parse ingestor.yaml template: %w", err)
			}
			var rendered bytes.Buffer
			if err := tmpl.Execute(&rendered, ingestorSS); err != nil {
				return fmt.Errorf("failed to render ingestor.yaml: %w", err)
			}

			// Decode and apply each manifest in the YAML
			decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(rendered.Bytes()), 4096)
			for {
				obj := &unstructured.Unstructured{}
				err := decoder.Decode(obj)
				if err != nil {
					if err.Error() == "EOF" {
						break
					}
					return fmt.Errorf("failed to decode manifest: %w", err)
				}
				if obj.Object == nil || obj.GetKind() == "" {
					continue
				}
				if err := r.Create(ctx, obj); err != nil && !errors.IsAlreadyExists(err) {
					return fmt.Errorf("failed to create %s: %w", obj.GetKind(), err)
				}
			}
			return nil
		}
		return err
	}

	// If the StatefulSet is found, we need to check if all its replicas are running
	if sts.Status.ReadyReplicas != *sts.Spec.Replicas {
		return fmt.Errorf("not all ingestor replicas are running: ready=%d, desired=%d", sts.Status.ReadyReplicas, *sts.Spec.Replicas)
	}

	return nil
}

type IngestorSS struct {
	Image           string
	LogsClusters    []string
	MetricsClusters []string
	TracesClusters  []string
}

func (i *IngestorSS) FromOperator(operator *adxmonv1.Operator) {
	if operator.Spec.Ingestor == nil {
		i.Image = "ghcr.io/azure/adx-mon/ingestor:latest"
	} else if operator.Spec.Ingestor.Image != "" {
		i.Image = operator.Spec.Ingestor.Image
	}

	if operator.Spec.ADX == nil {
		return
	}

	for _, cluster := range operator.Spec.ADX.Clusters {
		for _, db := range cluster.Databases {
			switch db.TelemetryType {
			case adxmonv1.DatabaseTelemetryLogs:
				i.LogsClusters = append(i.LogsClusters, fmt.Sprintf("%s=%s", db.Name, cluster.Endpoint))
			case adxmonv1.DatabaseTelemetryMetrics:
				i.MetricsClusters = append(i.MetricsClusters, fmt.Sprintf("%s=%s", db.Name, cluster.Endpoint))
			case adxmonv1.DatabaseTelemetryTraces:
				// TODO not yet supported
			}
		}
	}
}
