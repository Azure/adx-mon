package engine

import (
	"log/slog"
	"time"

	"github.com/Azure/adx-mon/alerter/rules"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
)

type alertRuleEvaluation struct {
	rule          *rules.Rule
	executionTime time.Time
	startTime     time.Time
	duration      time.Duration
	finished      bool

	outcome         string
	rows            int
	alertsGenerated int
}

func newAlertRuleEvaluation(rule *rules.Rule) *alertRuleEvaluation {
	now := time.Now()
	return &alertRuleEvaluation{
		rule:          rule,
		executionTime: now.UTC(),
		startTime:     now,
		outcome:       evaluationOutcomeSuccess,
	}
}

func (e *alertRuleEvaluation) finish() {
	duration := e.elapsed()
	metrics.AlertRuleEvaluationDurationSeconds.Observe(duration.Seconds())
	metrics.AlertRuleEvaluationsTotal.WithLabelValues(e.outcome).Inc()
	metrics.AlertsGeneratedTotal.Add(float64(e.alertsGenerated))
	logger.Info("AlertRule evaluation completed",
		slog.String("namespace", e.rule.Namespace),
		slog.String("name", e.rule.Name),
		slog.String("outcome", e.outcome),
		slog.Float64("duration_seconds", duration.Seconds()),
		slog.Int("rows", e.rows),
		slog.Int("alerts_generated", e.alertsGenerated),
	)
}

func (e *alertRuleEvaluation) elapsed() time.Duration {
	if !e.finished {
		e.duration = time.Since(e.startTime)
		e.finished = true
	}
	return e.duration
}
