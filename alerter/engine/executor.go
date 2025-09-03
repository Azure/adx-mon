package engine

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Azure/adx-mon/alerter/alert"
	"github.com/Azure/adx-mon/alerter/rules"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
	kustovalues "github.com/Azure/azure-kusto-go/kusto/data/value"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ruleStore interface {
	Rules() []*rules.Rule
}

type AlertCli interface {
	Create(ctx context.Context, endpoint string, alert alert.Alert) error
}

type Executor struct {
	alertCli    AlertCli
	alertAddr   string
	kustoClient Client
	ruleStore   ruleStore
	region      string
	ctrlCli     client.Client

	// tags are access by the worker concurrently outside a mutex.  This is safe because
	// the map is never modified after creation.
	tags map[string]string

	wg      sync.WaitGroup
	closeFn context.CancelFunc

	mu      sync.RWMutex
	workers map[string]*worker
}

type ExecutorOpts struct {
	AlertCli    AlertCli
	AlertAddr   string
	KustoClient Client
	RuleStore   ruleStore
	Region      string
	Tags        map[string]string
	CtrlCli     client.Client
}

// TODO make AlertAddr string part of alertcli
func NewExecutor(opts ExecutorOpts) *Executor {
	return &Executor{
		alertCli:    opts.AlertCli,
		alertAddr:   opts.AlertAddr,
		kustoClient: opts.KustoClient,
		ruleStore:   opts.RuleStore,
		region:      opts.Region,
		tags:        opts.Tags,
		ctrlCli:     opts.CtrlCli,
		workers:     make(map[string]*worker),
	}
}

func (e *Executor) Open(ctx context.Context) error {
	ctx, e.closeFn = context.WithCancel(ctx)
	logger.Infof("Begin executing %d queries", len(e.ruleStore.Rules()))

	e.syncWorkers(ctx)
	go e.periodicSync(ctx)
	return nil
}

func (e *Executor) workerKey(rule *rules.Rule) string {
	return fmt.Sprintf("%s/%s", rule.Namespace, rule.Name)
}

func (e *Executor) newWorker(rule *rules.Rule) *worker {
	return NewWorker(rule, e.region, e.tags, e.kustoClient, e.alertCli, fmt.Sprintf("%s/alerts", e.alertAddr), e.HandlerFn, e.ctrlCli)
}

func (e *Executor) Close() error {
	e.closeFn()
	e.wg.Wait()
	return nil
}

// HandlerFn converts rows of a query to Alerts.
func (e *Executor) HandlerFn(ctx context.Context, endpoint string, qc *QueryContext, row *table.Row) error {
	res := Notification{
		Severity:     math.MinInt64,
		CustomFields: map[string]string{},
	}

	columns := row.ColumnNames()
	for i, value := range row.Values {
		switch strings.ToLower(columns[i]) {
		case "title":
			res.Title = value.String()
		case "description":
			res.Description = value.String()
		case "severity":
			v, err := e.asInt64(value)
			if err != nil {
				return &NotificationValidationError{err.Error()}
			}
			res.Severity = v
		case "recipient":
			res.Recipient = value.String()
		case "summary":
			res.Summary = value.String()
		case "correlationid":
			res.CorrelationID = value.String()
		default:
			res.CustomFields[columns[i]] = value.String()
		}
	}

	if err := res.Validate(); err != nil {
		return err
	}

	summary, err := KustoQueryLinks(res.Summary, qc.Query, endpoint, qc.Rule.Database)
	if err != nil {
		metrics.QueryHealth.WithLabelValues(qc.Rule.Namespace, qc.Rule.Name).Set(0)
		return fmt.Errorf("failed to create kusto deep link: %w", err)
	}

	if res.CorrelationID != "" && !strings.HasPrefix(res.CorrelationID, fmt.Sprintf("%s/%s://", qc.Rule.Namespace, qc.Rule.Name)) {
		res.CorrelationID = fmt.Sprintf("%s/%s://%s", qc.Rule.Namespace, qc.Rule.Name, res.CorrelationID)
	}

	destination := qc.Rule.Destination
	// The recipient query results field is deprecated.
	if destination == "" {
		logger.Warnf("Recipient query results field is deprecated. Please use the destination field in the rule instead for %s/%s.", qc.Rule.Namespace, qc.Rule.Name)
		destination = res.Recipient
	}

	a := alert.Alert{
		Destination:   destination,
		Title:         res.Title,
		Summary:       summary,
		Description:   res.Description,
		Severity:      int(res.Severity),
		Source:        fmt.Sprintf("%s/%s", qc.Rule.Namespace, qc.Rule.Name),
		CorrelationID: res.CorrelationID,
		CustomFields:  res.CustomFields,
	}

	addr := fmt.Sprintf("%s/alerts", e.alertAddr)
	logger.Debugf("Sending alert %s %v", addr, a)

	if err := e.alertCli.Create(context.Background(), addr, a); err != nil {
		if errors.Is(err, alert.ErrTooManyRequests) {
			logger.Errorf("Failed to create Notification due to throttling: %s/%s", qc.Rule.Namespace, qc.Rule.Name)
			// We are throttled. Bail out of this loop so we stop trying to send notifications that will just be throttled.
			return err
		}

		logger.Errorf("Failed to create Notification: %s\n", err)
		metrics.NotificationUnhealthy.WithLabelValues(qc.Rule.Namespace, qc.Rule.Name).Set(1)
		return nil
	}
	metrics.NotificationUnhealthy.WithLabelValues(qc.Rule.Namespace, qc.Rule.Name).Set(0)

	return nil
}

func (e *Executor) asInt64(value kustovalues.Kusto) (int64, error) {
	switch t := value.(type) {
	case kustovalues.Long:
		return t.Value, nil
	case kustovalues.Real:
		return int64(t.Value), nil
	case kustovalues.String:
		v, err := strconv.ParseInt(t.Value, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("failed to convert severity to int: %w", err)
		}
		return v, nil
	case kustovalues.Int:
		return int64(t.Value), nil
	case kustovalues.Decimal:
		v, err := strconv.ParseFloat(t.Value, 64)
		if err != nil {
			return 0, fmt.Errorf("failed to convert severity to int: %w", err)
		}
		return int64(v), nil
	default:
		return 0, fmt.Errorf("failed to convert severity to int: %s", value.String())
	}
}

func (e *Executor) RunOnce(ctx context.Context) {
	ctx, e.closeFn = context.WithCancel(ctx)
	for _, r := range e.ruleStore.Rules() {
		worker := e.newWorker(r)
		worker.ExecuteQuery(ctx)
	}
}

// syncWorkers ensures that the workers are running for the current set of rules.  If any new rules
// are added, or existing rules are updated, a new worker will be started.  If any rules are deleted,
// the worker will be stopped. This function is called periodically by the executor.
func (e *Executor) syncWorkers(ctx context.Context) {
	// Track the query Ids that are still definied as CRs, so we can determine which ones were deleted.
	liveQueries := make(map[string]struct{})
	for _, r := range e.ruleStore.Rules() {
		id := e.workerKey(r)
		liveQueries[id] = struct{}{}
		w, ok := e.workers[id]
		if !ok {
			logger.Infof("Starting new worker for %s", id)
			worker := e.newWorker(r)
			worker.Run(ctx)
			e.workers[id] = worker
			continue
		}

		// Rule has not changed, leave the existing working running
		if w.rule.Version == r.Version {
			continue
		}

		logger.Infof("Rule %s has changed, restarting worker", id)
		w.Close()
		delete(e.workers, id)
		w = e.newWorker(r)
		e.workers[id] = w
		w.Run(ctx)
	}

	// Shutdown any workers that no longer exist
	for id := range e.workers {
		if _, ok := liveQueries[id]; !ok {
			logger.Infof("Shutting down worker for %s", id)
			e.workers[id].Close()
			delete(e.workers, id)
		}
	}
}

// periodicSync will periodically sync the workers with the current set of rules.
func (e *Executor) periodicSync(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	for {
		select {
		case <-ticker.C:
			e.syncWorkers(ctx)
		case <-ctx.Done():
			return
		}
	}
}
