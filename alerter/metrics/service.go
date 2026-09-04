package metrics

import (
	"context"
	"fmt"
	"time"

	adxmetrics "github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	srv "github.com/Azure/adx-mon/pkg/service"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

const statusLogInterval = 5 * time.Minute

type Service interface {
	srv.Component
}

type service struct {
	closeFn  context.CancelFunc
	gatherer prometheus.Gatherer
	previous snapshot
}

type snapshot struct {
	evaluations     map[string]float64
	alertsGenerated float64
	durationCount   uint64
	durationSum     float64
}

func NewService() Service {
	return &service{
		gatherer: prometheus.DefaultGatherer,
		previous: snapshot{evaluations: map[string]float64{}},
	}
}

func (s *service) Open(ctx context.Context) error {
	ctx, s.closeFn = context.WithCancel(ctx)
	go s.collect(ctx)
	return nil
}

func (s *service) Close() error {
	s.closeFn()
	return nil
}

func (s *service) collect(ctx context.Context) {
	ticker := time.NewTicker(statusLogInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			current, err := gatherSnapshot(s.gatherer)
			if err != nil {
				logger.Errorf("Failed to gather alerter metrics: %s", err)
				continue
			}
			delta := current.delta(s.previous)
			logger.Infof("Status PeriodSeconds=%d Evaluations=%d Successes=%d SetupErrors=%d UserErrors=%d "+
				"ServiceErrors=%d NotificationThrottled=%d AlertsGenerated=%d AverageDurationSeconds=%0.2f",
				int(statusLogInterval.Seconds()), uint64(delta.totalEvaluations()),
				uint64(delta.evaluations["success"]), uint64(delta.evaluations["setup_error"]),
				uint64(delta.evaluations["user_error"]), uint64(delta.evaluations["service_error"]),
				uint64(delta.evaluations["notification_throttled"]), uint64(delta.alertsGenerated),
				delta.averageDurationSeconds())
			s.previous = current
		}
	}
}

func gatherSnapshot(gatherer prometheus.Gatherer) (snapshot, error) {
	metricFamilies, err := gatherer.Gather()
	if err != nil {
		return snapshot{}, err
	}

	result := snapshot{evaluations: map[string]float64{}}
	for _, family := range metricFamilies {
		switch family.GetName() {
		case prometheus.BuildFQName(adxmetrics.Namespace, "alerter", "alert_rule_evaluations_total"):
			for _, metric := range family.Metric {
				result.evaluations[labelValue(metric, "outcome")] += metric.GetCounter().GetValue()
			}
		case prometheus.BuildFQName(adxmetrics.Namespace, "alerter", "alerts_generated_total"):
			for _, metric := range family.Metric {
				result.alertsGenerated += metric.GetCounter().GetValue()
			}
		case prometheus.BuildFQName(adxmetrics.Namespace, "alerter", "alert_rule_evaluation_duration_seconds"):
			for _, metric := range family.Metric {
				result.durationCount += metric.GetHistogram().GetSampleCount()
				result.durationSum += metric.GetHistogram().GetSampleSum()
			}
		}
	}
	return result, nil
}

func labelValue(metric *dto.Metric, name string) string {
	for _, label := range metric.Label {
		if label.GetName() == name {
			return label.GetValue()
		}
	}
	return ""
}

func (s snapshot) delta(previous snapshot) snapshot {
	result := snapshot{
		evaluations:     map[string]float64{},
		alertsGenerated: nonNegativeDelta(s.alertsGenerated, previous.alertsGenerated),
		durationCount:   uint64(nonNegativeDelta(float64(s.durationCount), float64(previous.durationCount))),
		durationSum:     nonNegativeDelta(s.durationSum, previous.durationSum),
	}
	for outcome, current := range s.evaluations {
		result.evaluations[outcome] = nonNegativeDelta(current, previous.evaluations[outcome])
	}
	return result
}

func nonNegativeDelta(current, previous float64) float64 {
	if current < previous {
		return current
	}
	return current - previous
}

func (s snapshot) totalEvaluations() float64 {
	var total float64
	for _, count := range s.evaluations {
		total += count
	}
	return total
}

func (s snapshot) averageDurationSeconds() float64 {
	if s.durationCount == 0 {
		return 0
	}
	return s.durationSum / float64(s.durationCount)
}

var _ = fmt.Sprintf
