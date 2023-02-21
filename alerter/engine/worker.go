package engine

import (
	"context"
	"github.com/Azure/adx-mon/alerter/queue"
	"github.com/Azure/adx-mon/alerter/rules"
	"github.com/Azure/adx-mon/logger"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
	"sync"
	"time"
)

type worker struct {
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	rule        *rules.Rule
	kustoClient Client
	ICMHandler  func(endpoint string, rule rules.Rule, row *table.Row) error
}

func (e *worker) Run() {
	e.wg.Add(1)
	defer e.wg.Done()

	logger.Info("Creating query executor for %s/%s in %s executing every %s",
		e.rule.Namespace, e.rule.Name, e.rule.Database, e.rule.Interval.String())

	// do-while
	if err := e.kustoClient.Query(e.ctx, *e.rule, e.ICMHandler); err != nil {
		logger.Error("Failed to execute query=%s/%s: %s", e.rule.Namespace, e.rule.Name, err)
		metrics.QueryHealth.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(0)
	}
	ticker := time.NewTicker(e.rule.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			// Try to acquire a worker slot
			queue.Workers <- struct{}{}

			start := time.Now()
			logger.Info("Executing %s/%s", e.rule.Namespace, e.rule.Name)
			if err := e.kustoClient.Query(e.ctx, *e.rule, e.ICMHandler); err != nil {
				logger.Error("Failed to execute query=%s.%s: %s", e.rule.Namespace, e.rule.Name, err)
				metrics.QueryHealth.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(0)
			} else {
				metrics.QueryHealth.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(1)
				logger.Info("Completed %s/%s in %s", e.rule.Namespace, e.rule.Name, time.Since(start))
			}

			// Release the worker slot
			<-queue.Workers
		}
	}
}

func (e *worker) Close() {
	e.cancel()
	e.wg.Wait()
}
