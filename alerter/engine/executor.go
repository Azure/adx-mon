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
	"github.com/Azure/adx-mon/alerter/queue"
	"github.com/Azure/adx-mon/alerter/rules"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	azquery "github.com/Azure/azure-kusto-go/azkustodata/query"
	aztypes "github.com/Azure/azure-kusto-go/azkustodata/types"
	azvalue "github.com/Azure/azure-kusto-go/azkustodata/value"
	"github.com/shopspring/decimal"
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

	querySlots chan struct{}

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
	Concurrency int
	CtrlCli     client.Client
}

// reservedNotificationColumns is the lowercase set of query result columns that
// adx-mon consumes as alert fields. Duplicate casings of these columns are rejected
// before row conversion so lint and execution do not silently pick one value.
var reservedNotificationColumns = map[string]struct{}{
	"title":         {},
	"description":   {},
	"severity":      {},
	"recipient":     {},
	"summary":       {},
	"correlationid": {},
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
		querySlots:  queue.New(opts.Concurrency),
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
	return NewWorker(&WorkerConfig{
		Rule:             rule,
		Region:           e.region,
		Tags:             e.tags,
		KustoClient:      e.kustoClient,
		AlertClient:      e.alertCli,
		AlertAddr:        fmt.Sprintf("%s/alerts", e.alertAddr),
		HandlerFn:        e.HandlerFn,
		CtrlClient:       e.ctrlCli,
		sharedQuerySlots: e.querySlots,
	})
}

func (e *Executor) Close() error {
	e.closeFn()
	e.wg.Wait()
	return nil
}

// HandlerFn converts rows of a query to Alerts.
func (e *Executor) HandlerFn(ctx context.Context, endpoint string, qc *QueryContext, row azquery.Row) error {
	res, err := ParseAlertResult(qc, row)
	if err != nil {
		return err
	}

	summary, err := KustoQueryLinks(res.Summary, qc.Query, endpoint, qc.Rule.Database)
	if err != nil {
		metrics.QueryHealth.WithLabelValues(qc.Rule.Namespace, qc.Rule.Name).Set(0)
		return fmt.Errorf("failed to create kusto deep link: %w", err)
	}

	a := alert.Alert{
		Destination:   res.Destination,
		Title:         res.Title,
		Summary:       summary,
		Description:   res.Description,
		Severity:      clampInt64ToInt(res.Severity),
		Source:        res.Source,
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

// ParseAlertResult converts and validates a query row without performing delivery enrichment or I/O.
func ParseAlertResult(qc *QueryContext, row azquery.Row) (AlertResult, error) {
	if qc == nil {
		return AlertResult{}, fmt.Errorf("query context must not be nil")
	}
	if qc.Rule == nil {
		return AlertResult{}, fmt.Errorf("query context rule must not be nil")
	}

	notification := Notification{
		Severity:     math.MinInt64,
		CustomFields: map[string]string{},
	}

	columns := row.Columns()
	values := row.Values()
	if len(columns) != len(values) {
		return AlertResult{}, &NotificationValidationError{fmt.Sprintf("query result row has %d columns and %d values", len(columns), len(values))}
	}

	columnNames := make([]string, 0, len(columns))
	for _, column := range columns {
		columnNames = append(columnNames, column.Name())
	}
	if err := validateNotificationColumns(columnNames); err != nil {
		return AlertResult{}, err
	}

	for i, value := range values {
		if value == nil {
			return AlertResult{}, &NotificationValidationError{fmt.Sprintf("query result column %q has nil value", columnNames[i])}
		}

		switch strings.ToLower(columnNames[i]) {
		case "title":
			notification.Title = value.String()
		case "description":
			notification.Description = value.String()
		case "severity":
			v, err := asInt64(value)
			if err != nil {
				return AlertResult{}, &NotificationValidationError{err.Error()}
			}
			notification.Severity = v
		case "recipient":
			notification.Recipient = value.String()
		case "summary":
			notification.Summary = value.String()
		case "correlationid":
			notification.CorrelationID = value.String()
		default:
			notification.CustomFields[columnNames[i]] = value.String()
		}
	}

	if err := notification.Validate(); err != nil {
		return AlertResult{}, err
	}

	source := fmt.Sprintf("%s/%s", qc.Rule.Namespace, qc.Rule.Name)
	correlationID := notification.CorrelationID
	if correlationID != "" && !strings.HasPrefix(correlationID, source+"://") {
		correlationID = fmt.Sprintf("%s://%s", source, correlationID)
	}

	destination := qc.Rule.Destination
	if destination == "" {
		// The recipient query results field is deprecated.
		logger.Warnf("Recipient query results field is deprecated. Please use the destination field in the rule instead for %s/%s.", qc.Rule.Namespace, qc.Rule.Name)
		destination = notification.Recipient
	}

	return AlertResult{
		Destination:   destination,
		Title:         notification.Title,
		Summary:       notification.Summary,
		Description:   notification.Description,
		Severity:      notification.Severity,
		Source:        source,
		CorrelationID: correlationID,
		CustomFields:  notification.CustomFields,
	}, nil
}

func clampInt64ToInt(v int64) int {
	if v > int64(math.MaxInt) {
		return math.MaxInt
	}
	if v < int64(math.MinInt) {
		return math.MinInt
	}
	return int(v)
}

func validateNotificationColumns(columns []string) error {
	seen := make(map[string]string)
	for _, column := range columns {
		key := strings.ToLower(column)
		_, ok := reservedNotificationColumns[key]
		if !ok {
			continue
		}

		if previous, ok := seen[key]; ok {
			return &NotificationValidationError{fmt.Sprintf("query results include multiple columns for reserved alert field %q: %s, %s", previous, previous, column)}
		}
		seen[key] = column
	}

	return nil
}

func asInt64(value azvalue.Kusto) (int64, error) {
	if value == nil {
		return 0, fmt.Errorf("failed to convert severity to int: <nil>")
	}
	switch value.GetType() {
	case aztypes.Long:
		v, ok := value.GetValue().(*int64)
		if !ok || v == nil {
			break
		}
		return *v, nil
	case aztypes.Real:
		v, ok := value.GetValue().(*float64)
		if !ok || v == nil {
			break
		}
		return int64(*v), nil
	case aztypes.String:
		v, err := strconv.ParseInt(value.String(), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("failed to convert severity to int: %w", err)
		}
		return v, nil
	case aztypes.Int:
		v, ok := value.GetValue().(*int32)
		if !ok || v == nil {
			break
		}
		return int64(*v), nil
	case aztypes.Decimal:
		v, ok := value.GetValue().(*decimal.Decimal)
		if !ok || v == nil {
			break
		}
		return v.IntPart(), nil
	}
	return 0, fmt.Errorf("failed to convert severity to int: %v", value.GetValue())
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
