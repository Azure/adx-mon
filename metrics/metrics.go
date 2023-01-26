package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	Namespace       = "adxmon"
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
)
