package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Labels are bounded parser names and reasons, never stream IDs or paths.
var LogsCollectorPartialMessagesDropped = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: Namespace,
	Name:      "collector_partial_messages_dropped_total",
	Help:      "Incomplete log messages discarded for size, idle timeout, or assembler capacity; oversized messages are dropped through the final fragment.",
}, []string{"source", "reason"})
