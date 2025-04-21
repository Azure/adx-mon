package operator

import (
	"fmt"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var IngestorHandler = &ResourceHandler{
	Name:          "ingestor",
	ConditionType: adxmonv1.IngestorClusterConditionOwner,
	ManifestPath:  "manifests/ingestor.yaml",
	ResourceKind:  "StatefulSet",
	IsReady:       StatefulSetIsReady,
	TemplateData: func(operator *adxmonv1.Operator) any {
		image := "ghcr.io/azure/adx-mon/ingestor:latest"
		if operator.Spec.Ingestor != nil && operator.Spec.Ingestor.Image != "" {
			image = operator.Spec.Ingestor.Image
		}

		return struct {
			Image           string
			MetricsClusters []string
			LogsClusters    []string
		}{
			Image:           image,
			MetricsClusters: getMetricsEndpoints(operator.Spec.ADX),
			LogsClusters:    getLogsEndpoints(operator.Spec.ADX),
		}
	},
	UpdateSpec: func(obj client.Object, operator *adxmonv1.Operator) bool {
		// TODO image and replica changes made to the SS but not to the operator should be considered
		// drift to reconcile.
		return false
	},
	UpdateOperator: func(obj client.Object, operator *adxmonv1.Operator) bool {
		// TODO this is just cluster local, but if we create outside of the cluster we would need a proper Endpoint
		endpoint := fmt.Sprintf("https://%s.%s.svc.cluster.local", "ingestor", operator.Namespace)
		if operator.Spec.Collector == nil {
			operator.Spec.Collector = &adxmonv1.CollectorConfig{}
		}
		if operator.Spec.Collector.IngestorEndpoint != endpoint {
			operator.Spec.Collector.IngestorEndpoint = endpoint
			return true
		}
		return false
	},
}

var CollectorHandler = &ResourceHandler{
	Name:          "collector",
	ConditionType: adxmonv1.CollectorClusterConditionOwner,
	ManifestPath:  "manifests/collector.yaml",
	ResourceKind:  "DaemonSet",
	IsReady:       DaemonSetIsReady,
	TemplateData: func(operator *adxmonv1.Operator) any {
		// Get ingestor endpoint from operator config if specified, otherwise use in-cluster endpoint
		ingestorEndpoint := operator.Spec.Collector.IngestorEndpoint
		if ingestorEndpoint == "" && operator.Spec.Ingestor != nil {
			ingestorEndpoint = fmt.Sprintf("https://%s.%s.svc.cluster.local", "ingestor", operator.Namespace)
		}

		image := "ghcr.io/azure/adx-mon/collector:latest"
		if operator.Spec.Collector != nil && operator.Spec.Collector.Image != "" {
			image = operator.Spec.Collector.Image
		}
		return struct {
			Image            string
			IngestorEndpoint string
		}{
			Image:            image,
			IngestorEndpoint: ingestorEndpoint,
		}
	},
	UpdateSpec: func(obj client.Object, operator *adxmonv1.Operator) bool {
		// Add any spec update logic here if needed
		return false
	},
}

func getMetricsEndpoints(adx *adxmonv1.ADXConfig) []string {
	if adx == nil {
		return nil
	}
	var endpoints []string
	for _, cluster := range adx.Clusters {
		for _, db := range cluster.Databases {
			if db.TelemetryType == adxmonv1.DatabaseTelemetryMetrics {
				endpoints = append(endpoints, fmt.Sprintf("%s=%s", db.Name, cluster.Endpoint))
				break
			}
		}
	}
	return endpoints
}

func getLogsEndpoints(adx *adxmonv1.ADXConfig) []string {
	if adx == nil {
		return nil
	}
	var endpoints []string
	for _, cluster := range adx.Clusters {
		for _, db := range cluster.Databases {
			if db.TelemetryType == adxmonv1.DatabaseTelemetryLogs {
				endpoints = append(endpoints, fmt.Sprintf("%s=%s", db.Name, cluster.Endpoint))
				break
			}
		}
	}
	return endpoints
}
