package operator

import (
	"slices"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Operator phases
type OperatorPhase int

const (
	PhaseEnsureCrds OperatorPhase = iota
	PhaseEnsureKusto
	PhaseEnsureIngestor
	PhaseEnsureCollector
	PhaseEnsureAlerter
	PhaseDone
	PhaseUnknown
)

func (p OperatorPhase) String() string {
	switch p {
	case PhaseEnsureCrds:
		return "EnsureCrds"
	case PhaseEnsureKusto:
		return "EnsureKusto"
	case PhaseEnsureIngestor:
		return "EnsureIngestor"
	case PhaseEnsureCollector:
		return "EnsureCollector"
	case PhaseEnsureAlerter:
		return "EnsureAlerter"
	case PhaseDone:
		return "Done"
	default:
		return "Unknown"
	}
}

func Phase(p string) OperatorPhase {
	switch p {
	case "EnsureCrds":
		return PhaseEnsureCrds
	case "EnsureKusto":
		return PhaseEnsureKusto
	case "EnsureIngestor":
		return PhaseEnsureIngestor
	case "EnsureCollector":
		return PhaseEnsureCollector
	case "EnsureAlerter":
		return PhaseEnsureAlerter
	case "Done":
		return PhaseDone
	default:
		return PhaseUnknown
	}
}

func currentPhase(operator *adxmonv1.Operator) OperatorPhase {
	if len(operator.Status.Conditions) == 0 {
		return PhaseEnsureCrds
	}

	var incomplete []OperatorPhase
	for _, condition := range operator.Status.Conditions {
		if condition.Status != metav1.ConditionTrue {
			incomplete = append(incomplete, Phase(condition.Reason))
		}
	}

	// Sort by phase priority (lowest iota value first)
	slices.Sort(incomplete)

	if len(incomplete) > 0 {
		return incomplete[0]
	}
	return PhaseDone
}

func advancePhase(condition metav1.Condition) metav1.Condition {
	var phase metav1.Condition
	switch condition.Reason {
	case PhaseEnsureCrds.String():
		phase.Type = adxmonv1.ADXClusterConditionOwner
		phase.Status = metav1.ConditionUnknown
		phase.Reason = PhaseEnsureKusto.String()
		phase.Message = "Ensuring Kusto Cluster"
	case PhaseEnsureKusto.String():
		phase.Type = adxmonv1.IngestorClusterConditionOwner
		phase.Status = metav1.ConditionUnknown
		phase.Reason = PhaseEnsureIngestor.String()
		phase.Message = "Ensuring Ingestor StatefulSet"
	case PhaseEnsureIngestor.String():
		phase.Type = adxmonv1.CollectorClusterConditionOwner
		phase.Status = metav1.ConditionUnknown
		phase.Reason = PhaseEnsureCollector.String()
		phase.Message = "Ensuring Collector DaemonSet"
	case PhaseEnsureCollector.String():
		phase.Type = adxmonv1.AlerterClusterConditionOwner
		phase.Status = metav1.ConditionUnknown
		phase.Reason = PhaseEnsureAlerter.String()
		phase.Message = "Ensuring Alerter Deployment"
	case PhaseEnsureAlerter.String():
		phase.Type = adxmonv1.OperatorCommandConditionOwner
		phase.Status = metav1.ConditionTrue
		phase.Reason = PhaseDone.String()
		phase.Message = "All components are ready"
	default:
		phase.Type = adxmonv1.OperatorCommandConditionOwner
		phase.Status = metav1.ConditionUnknown
		phase.Reason = PhaseEnsureCrds.String()
		phase.Message = "Starting reconciliation"
	}

	return phase
}
