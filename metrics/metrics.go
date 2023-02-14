package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	Namespace = "adxmon"

	// Ingestor metrics
	SamplesReceived = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "ingestor",
		Name:      "samples_received_total",
		Help:      "Counter of samples received from an ingestor instance",
	}, []string{"node"})

	SamplesStored = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "ingestor",
		Name:      "samples_stored_total",
		Help:      "Counter of samples stored for an ingestor instance",
	}, []string{"node"})

	// Alerting metrics
	AlertQueryFailures = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "alerter",
		Name:      "query_failures_total",
		Help:      "Number of alert query failures",
	}, []string{"namespace", "name"})

	AlertQuerySuccess = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "alerter",
		Name:      "query_success_total",
		Help:      "Number of alert query successfully executed",
	}, []string{"namespace", "name"})

	NotificationFailures = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "alerter",
		Name:      "notification_failures_total",
		Help:      "Number of alert notification failures",
	}, []string{"namespace", "name"})

	NotificationSuccess = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "alerter",
		Name:      "notification_success_total",
		Help:      "Number of alert notification successfully created",
	}, []string{"namespace", "name"})
)
