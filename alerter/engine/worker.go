package engine

import (
	"context"
	"errors"
	"fmt"
	"github.com/Azure/adx-mon/alert"
	"strings"
	"sync"
	"time"

	"github.com/Azure/adx-mon/alerter/queue"
	"github.com/Azure/adx-mon/alerter/rules"
	"github.com/Azure/adx-mon/logger"
	"github.com/Azure/adx-mon/metrics"
	kerrors "github.com/Azure/azure-kusto-go/kusto/data/errors"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
)

type worker struct {
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	rule        *rules.Rule
	Region      string
	kustoClient Client
	AlertAddr   string
	AlertCli    interface {
		Create(ctx context.Context, endpoint string, alert alert.Alert) error
	}
	HandlerFn func(ctx context.Context, endpoint string, qc *QueryContext, row *table.Row) error
}

func (e *worker) Run(ctx context.Context) {
	ctx, e.cancel = context.WithCancel(ctx)
	e.wg.Add(1)
	defer e.wg.Done()

	logger.Info("Creating query executor for %s/%s in %s executing every %s",
		e.rule.Namespace, e.rule.Name, e.rule.Database, e.rule.Interval.String())

	// do-while
	e.ExecuteQuery(ctx)

	ticker := time.NewTicker(e.rule.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.ExecuteQuery(ctx)
		}
	}
}

func (e *worker) ExecuteQuery(ctx context.Context) {
	// Try to acquire a worker slot
	queue.Workers <- struct{}{}

	// Release the worker slot
	defer func() { <-queue.Workers }()

	start := time.Now().UTC()
	queryContext, err := NewQueryContext(e.rule, start, e.Region)
	if err != nil {
		logger.Error("Failed to wrap query=%s/%s on %s/%s: %s", e.rule.Namespace, e.rule.Name, e.kustoClient.Endpoint(e.rule.Database), e.rule.Database, err)
		return
	}

	logger.Info("Executing %s/%s on %s/%s", e.rule.Namespace, e.rule.Name, e.kustoClient.Endpoint(e.rule.Database), e.rule.Database)
	if err := e.kustoClient.Query(ctx, queryContext, e.HandlerFn); err != nil {
		logger.Error("Failed to execute query=%s/%s on %s/%s: %s", e.rule.Namespace, e.rule.Name, e.kustoClient.Endpoint(e.rule.Database), e.rule.Database, err)

		if !isUserError(err) {
			metrics.QueryHealth.WithLabelValues(e.rule.Namespace, e.rule.Name).Inc()
			return
		}

		summary, err := KustoQueryLinks(fmt.Sprintf("This query is failing to execute:<br/><br/><pre>%s</pre><br/><br/>", err.Error()), queryContext.Query, e.kustoClient.Endpoint(e.rule.Database), e.rule.Database)
		if err != nil {
			logger.Error("Failed to send failure alert for %s/%s: %s", e.rule.Namespace, e.rule.Name, err)
			metrics.NotificationUnhealthy.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(1)
			return
		}

		if err := e.AlertCli.Create(ctx, e.AlertAddr, alert.Alert{
			Destination:   e.rule.Destination,
			Title:         fmt.Sprintf("Alert %s/%s has query errors on %s", e.rule.Namespace, e.rule.Name, e.kustoClient.Endpoint(e.rule.Database)),
			Summary:       summary,
			Severity:      3,
			Source:        fmt.Sprintf("%s/%s", e.rule.Namespace, e.rule.Name),
			CorrelationID: fmt.Sprintf("alert-failure/%s/%s", e.rule.Namespace, e.rule.Name),
		}); err != nil {
			logger.Error("Failed to send failure alert for %s/%s: %s", e.rule.Namespace, e.rule.Name, err)
			// Only set the query as failed if we are not able to send a failure alert directly.
			metrics.NotificationUnhealthy.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(1)
			return
		}
		metrics.QueryHealth.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(0)
		return
	}

	metrics.QueryHealth.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(1)
	logger.Info("Completed %s/%s in %s", e.rule.Namespace, e.rule.Name, time.Since(start))
}

func (e *worker) Close() {
	if e.cancel != nil {
		e.cancel()
	}
	e.wg.Wait()
}

func isUserError(err error) bool {
	if err == nil {
		return false
	}

	var kerr *kerrors.HttpError
	if errors.As(err, &kerr) {
		if kerr.Kind == kerrors.KClientArgs {
			return true
		}
		lowerErr := strings.ToLower(kerr.Error())
		if strings.Contains(lowerErr, "sem0001") || strings.Contains(lowerErr, "semantic error") || strings.Contains(lowerErr, "request is invalid") {
			return true
		}
	}

	return false
}
