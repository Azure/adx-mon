package engine

import (
	"context"
	"sync"
	"time"

	"github.com/Azure/adx-mon/alerter/queue"
	"github.com/Azure/adx-mon/alerter/rules"
	"github.com/Azure/adx-mon/logger"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
)

type worker struct {
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	rule        *rules.Rule
	kustoClient Client
	HandlerFn   func(endpoint string, rule rules.Rule, row *table.Row) error
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

	start := time.Now()
	logger.Info("Executing %s/%s on %s/%s", e.rule.Namespace, e.rule.Name, e.kustoClient.Endpoint(e.rule.Database), e.rule.Database)
	if err := e.kustoClient.Query(ctx, *e.rule, e.HandlerFn); err != nil {
		logger.Error("Failed to execute query=%s.%s on %s/%s: %s", e.rule.Namespace, e.rule.Name, e.kustoClient.Endpoint(e.rule.Database), e.rule.Database, err)
		metrics.QueryHealth.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(0)
		return
	}

	metrics.QueryHealth.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(1)
	logger.Info("Completed %s/%s in %s", e.rule.Namespace, e.rule.Name, time.Since(start))
}

func (e *worker) Close() {
	e.cancel()
	e.wg.Wait() //waiting so that we don't leak go routines/quueue.worker? Could rely on caller to cancel?
}
