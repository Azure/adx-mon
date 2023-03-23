package engine

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Azure/adx-mon/alert"
	"github.com/Azure/adx-mon/alerter/rules"
	"github.com/Azure/adx-mon/logger"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
	kustovalues "github.com/Azure/azure-kusto-go/kusto/data/value"
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

	// resconsider this later or documetn why we do it https://go.dev/blog/context-and-structs
	//"Contexts should not be stored inside a struct type, but instead passed to each function that needs it."
	ctx context.Context

	wg      sync.WaitGroup
	closeFn context.CancelFunc

	mu      sync.RWMutex
	workers map[string]*worker
}

// TODO make AlertAddr   string part of alertcli
func NewExecutor(alert AlertCli, alertAddr string, kustoClient Client, ruleStore ruleStore) *Executor {
	return &Executor{
		alertCli:    alert,
		alertAddr:   alertAddr,
		kustoClient: kustoClient,
		ruleStore:   ruleStore,
		workers:     make(map[string]*worker),
	}
}

func (e *Executor) Open(ctx context.Context) error {
	e.ctx, e.closeFn = context.WithCancel(ctx)
	logger.Info("Begin executing %d queries", len(e.ruleStore.Rules()))

	e.syncWorkers()
	go e.periodicSync()
	return nil
}

func (e *Executor) workerKey(rule *rules.Rule) string {
	return fmt.Sprintf("%s/%s", rule.Namespace, rule.Name)
}

func (e *Executor) newWorker(rule *rules.Rule) *worker {
	ctx, cancel := context.WithCancel(e.ctx)
	return &worker{
		ctx:         ctx,
		cancel:      cancel,
		rule:        rule,
		kustoClient: e.kustoClient,
		HandlerFn:   e.HandlerFn,
	}
}

func (e *Executor) Close() error {
	e.closeFn()
	e.wg.Wait()
	return nil
}

// HandlerFn converts rows of a query to Alerts.
func (e *Executor) HandlerFn(endpoint string, rule rules.Rule, row *table.Row) error {
	res := Notification{
		Severity:     math.MinInt64,
		CustomFields: map[string]string{},
	}

	query := rule.Query

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
				metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name).Set(0)
				return err
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
		metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name).Set(0)
		return err
	}

	for k, v := range rule.Parameters {
		switch vv := v.(type) {
		case string:
			query = strings.Replace(query, k, fmt.Sprintf("\"%s\"", vv), -1)
		default:
			metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name).Set(0)
			return fmt.Errorf("unimplemented query type: %v", vv)
		}
	}

	url, err := kustoDeepLink(query)
	if err != nil {
		metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name).Set(0)
		return fmt.Errorf("failed to create kusto deep link: %w", err)
	}

	if !strings.HasSuffix(endpoint, "/") {
		endpoint = endpoint + "/"
	}

	// Setup the Kusto query deep links
	link := "Execute in "
	link += fmt.Sprintf(`<a href="%s%s?query=%s">[Web]</a> `, endpoint, rule.Database, url)
	link += fmt.Sprintf(`<a href="%s%s?query=%s&web=0">[Desktop]</a>`, endpoint, rule.Database, url)

	summary := fmt.Sprintf("%s<br/><br/>%s</br><pre>%s</pre>", res.Summary, link, query)
	summary = strings.TrimSpace(summary)

	if res.CorrelationID != "" && !strings.HasPrefix(res.CorrelationID, fmt.Sprintf("%s/%s://", rule.Namespace, rule.Name)) {
		res.CorrelationID = fmt.Sprintf("%s/%s://%s", rule.Namespace, rule.Name, res.CorrelationID)
	}

	destination := rule.Destination
	// The recipient query results field is deprecated.
	if destination == "" {
		logger.Warn("Recipient query results field is deprecated. Please use the destination field in the rule instead for %s/%s.", rule.Namespace, rule.Name)
		destination = res.Recipient
	}

	a := alert.Alert{
		Destination:   destination,
		Title:         res.Title,
		Summary:       summary,
		Description:   res.Description,
		Severity:      int(res.Severity),
		Source:        fmt.Sprintf("%s/%s", rule.Namespace, rule.Name),
		CorrelationID: res.CorrelationID,
		CustomFields:  res.CustomFields,
	}

	addr := fmt.Sprintf("%s/alerts", e.alertAddr)
	logger.Debug("Sending alert %s %v", addr, a)

	if err := e.alertCli.Create(context.Background(), addr, a); err != nil {
		logger.Error("Failed to create Notification: %s\n", err)
		metrics.NotificationUnhealthy.WithLabelValues(rule.Namespace, rule.Name).Set(1)
		return nil
	}
	metrics.NotificationUnhealthy.WithLabelValues(rule.Namespace, rule.Name).Set(0)

	//log.Infof("Created Notification %s - %s", resp.IncidentId, res.Title)

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
	e.ctx, e.closeFn = context.WithCancel(ctx) //todo move way from using context on struct
	for _, r := range e.ruleStore.Rules() {
		worker := e.newWorker(r)
		worker.ExecuteQuery(ctx)
	}
}

func (e *Executor) syncWorkers() {
	// Track the query Ids that are still definied as CRs, so we can determine which ones were deleted.
	liveQueries := make(map[string]struct{})
	for _, r := range e.ruleStore.Rules() {
		id := e.workerKey(r)
		liveQueries[id] = struct{}{}
		worker, ok := e.workers[id]
		if !ok {
			logger.Info("Starting new worker for %s", id)
			worker := e.newWorker(r)
			e.workers[id] = worker
			go worker.Run()
			continue
		}

		// Rule has not changed, leave the existing working running
		if worker.rule.Version == r.Version {
			continue
		}

		if worker.rule.Version != r.Version {
			logger.Info("Rule %s has changed, restarting worker", id)
			worker.Close()
			delete(e.workers, id)
			worker := e.newWorker(r)
			e.workers[id] = worker
			go worker.Run()
		}
	}

	// Shutdown any workers that no longer exist
	for id := range e.workers {
		if _, ok := liveQueries[id]; !ok {
			logger.Info("Shutting down worker for %s", id)
			e.workers[id].Close()
			delete(e.workers, id)
		}
	}
}

func (e *Executor) periodicSync() {
	ticker := time.NewTicker(10 * time.Second)
	for {
		select {
		case <-ticker.C:
			e.syncWorkers()
		case <-e.ctx.Done():
			return
		}
	}
}

func kustoDeepLink(q string) (string, error) {
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	if _, err := w.Write([]byte(q)); err != nil {
		return "", err
	}

	if err := w.Flush(); err != nil {
		return "", err
	}

	if err := w.Close(); err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(b.Bytes()), nil
}
