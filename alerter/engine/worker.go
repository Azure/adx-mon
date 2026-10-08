package engine

import (
	"context"
	"sync"
	"time"

	"github.com/Azure/adx-mon/alerter/alert"
	"github.com/Azure/adx-mon/alerter/queue"
	"github.com/Azure/adx-mon/alerter/rules"
	"github.com/Azure/adx-mon/pkg/logger"
	azquery "github.com/Azure/azure-kusto-go/azkustodata/query"
	"k8s.io/utils/clock"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type worker struct {
	mu     sync.Mutex
	cancel context.CancelFunc

	wg          sync.WaitGroup
	rule        *rules.Rule
	region      string
	kustoClient Client
	alertAddr   string
	alertCli    interface {
		Create(ctx context.Context, endpoint string, alert alert.Alert) error
	}
	handlerFn  func(ctx context.Context, endpoint string, qc *QueryContext, row azquery.Row) error
	querySlots chan struct{}
	ctrlCli    client.Client
	queryTime  time.Duration
	clock      clock.Clock

	// criteria/expression evaluation cached at construction
	matchAllowed bool
	matchErr     error
}

// WorkerConfig groups parameters for constructing a worker.
type WorkerConfig struct {
	Rule                 *rules.Rule
	Region               string
	Tags                 map[string]string
	KustoClient          Client
	MaxConcurrentQueries int
	AlertClient          interface {
		Create(ctx context.Context, endpoint string, alert alert.Alert) error
	}
	AlertAddr        string
	HandlerFn        func(ctx context.Context, endpoint string, qc *QueryContext, row azquery.Row) error
	CtrlClient       client.Client
	sharedQuerySlots chan struct{}
	Clock            clock.Clock
}

// NewWorker creates a worker and performs one-time match evaluation.
func NewWorker(cfg *WorkerConfig) *worker {
	if cfg == nil || cfg.Rule == nil {
		return nil
	}

	querySlots := cfg.sharedQuerySlots
	if querySlots == nil {
		querySlots = queue.New(cfg.MaxConcurrentQueries)
	}
	workerClock := cfg.Clock
	if workerClock == nil {
		workerClock = clock.RealClock{}
	}
	w := &worker{
		rule:        cfg.Rule,
		region:      cfg.Region,
		kustoClient: cfg.KustoClient,
		alertCli:    cfg.AlertClient,
		alertAddr:   cfg.AlertAddr,
		handlerFn:   cfg.HandlerFn,
		querySlots:  querySlots,
		ctrlCli:     cfg.CtrlClient,
		queryTime:   maxQueryTime,
		clock:       workerClock,
	}
	allowed, err := cfg.Rule.Matches(cfg.Tags)
	w.matchAllowed = allowed
	w.matchErr = err
	if err != nil {
		logger.Errorf("Worker initialization match error for %s/%s: %v", cfg.Rule.Namespace, cfg.Rule.Name, err)
	} else if !allowed {
		logger.Infof("Worker %s/%s disabled (criteria/expression not matched) at initialization", cfg.Rule.Namespace, cfg.Rule.Name)
	}
	return w
}

func (e *worker) Run(ctx context.Context) {
	e.wg.Add(1)

	e.mu.Lock()
	ctx, e.cancel = context.WithCancel(ctx)
	e.mu.Unlock()

	// Best-effort: update criteria condition reflecting cached match evaluation
	e.updateAlertRuleCriteriaCondition(ctx)

	go func() {
		defer e.wg.Done()

		// Calculate the next execution time based on last execution
		nextQueryTime := e.calculateNextQueryTime()

		logger.Infof("Creating query executor for %s/%s in %s executing every %s, next execution at %s",
			e.rule.Namespace, e.rule.Name, e.rule.Database, e.rule.Interval.String(), nextQueryTime.Format(time.RFC3339))

		// If we should execute immediately (e.g., first time or overdue), do so
		if nextQueryTime.Before(time.Now()) {
			e.ExecuteQuery(ctx)
		} else {
			// Wait until the calculated next execution time before starting the ticker
			waitDuration := time.Until(nextQueryTime)

			select {
			case <-ctx.Done():
				return
			case <-time.After(waitDuration):
				e.ExecuteQuery(ctx)
			}
		}

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
	}()
}

// calculateNextQueryTime determines when the next execution should occur
// based on the last execution time from the AlertRule status
func (e *worker) calculateNextQueryTime() time.Time {
	// If no last query time, this is the first execution
	if e.rule.LastQueryTime.IsZero() {
		return e.clock.Now().Add(-time.Second) // Immediate execution
	}

	// Calculate next execution time based on last execution + interval
	lastQueryTime := e.rule.LastQueryTime
	nextQueryTime := lastQueryTime.Add(e.rule.Interval)

	return nextQueryTime
}

func (e *worker) Close() {
	e.mu.Lock()
	cancelFn := e.cancel
	e.mu.Unlock()
	if cancelFn != nil {
		cancelFn()
	}

	e.wg.Wait()
}
