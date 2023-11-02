package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Azure/adx-mon/alerter/alert"
	"github.com/Azure/adx-mon/alerter/queue"
	"github.com/Azure/adx-mon/alerter/rules"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	kerrors "github.com/Azure/azure-kusto-go/kusto/data/errors"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
)

type worker struct {
	mu     sync.Mutex
	cancel context.CancelFunc

	wg          sync.WaitGroup
	rule        *rules.Rule
	Region      string
	tags        map[string]string
	kustoClient Client
	AlertAddr   string
	AlertCli    interface {
		Create(ctx context.Context, endpoint string, alert alert.Alert) error
	}
	HandlerFn func(ctx context.Context, endpoint string, qc *QueryContext, row *table.Row) error
}

func (e *worker) Run(ctx context.Context) {
	e.wg.Add(1)
	defer e.wg.Done()

	e.mu.Lock()
	ctx, e.cancel = context.WithCancel(ctx)
	e.mu.Unlock()

	logger.Infof("Creating query executor for %s/%s in %s executing every %s",
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
	// Check if the rule is enabled for this instance by matching any of the alert criteria tags.
	var matched bool
	for k, v := range e.rule.Criteria {
		lowerKey := strings.ToLower(k)
		if vv, ok := e.tags[lowerKey]; ok {
			for _, value := range v {
				if strings.ToLower(vv) == strings.ToLower(value) {
					matched = true
					break
				}
			}
		}
		if matched {
			break
		}
	}

	// If tags are specified, but none of them matched, skip the query
	if len(e.rule.Criteria) > 0 && !matched {
		logger.Infof("Skipping %s/%s on %s/%s because none of the tags matched: %v", e.rule.Namespace, e.rule.Name, e.kustoClient.Endpoint(e.rule.Database), e.rule.Database, e.tags)
		return
	}

	// Try to acquire a worker slot
	queue.Workers <- struct{}{}

	// Release the worker slot
	defer func() { <-queue.Workers }()

	start := time.Now().UTC()
	queryContext, err := NewQueryContext(e.rule, start, e.Region)
	if err != nil {
		logger.Errorf("Failed to wrap query=%s/%s on %s/%s: %s", e.rule.Namespace, e.rule.Name, e.kustoClient.Endpoint(e.rule.Database), e.rule.Database, err)
		return
	}

	logger.Infof("Executing %s/%s on %s/%s", e.rule.Namespace, e.rule.Name, e.kustoClient.Endpoint(e.rule.Database), e.rule.Database)
	err, rows := e.kustoClient.Query(ctx, queryContext, e.HandlerFn)
	if err != nil {
		// This failed because we sent too many notifications.
		if errors.Is(err, alert.ErrTooManyRequests) {
			err := e.AlertCli.Create(ctx, e.AlertAddr, alert.Alert{
				Destination:   e.rule.Destination,
				Title:         fmt.Sprintf("Alert %s/%s has too many notifications", e.rule.Namespace, e.rule.Name),
				Summary:       "This alert has been throttled by ICM due to too many notifications.  Please reduce the number of notifications for this alert.",
				Severity:      3,
				Source:        fmt.Sprintf("notification-failure/%s/%s", e.rule.Namespace, e.rule.Name),
				CorrelationID: fmt.Sprintf("notification-failure/%s/%s", e.rule.Namespace, e.rule.Name),
			})
			if err != nil {
				logger.Errorf("Failed to send alert for throttled notification for %s/%s: %s", e.rule.Namespace, e.rule.Name, err)
			}
			return
		}

		// This failed because the query failed.
		logger.Errorf("Failed to execute query=%s/%s on %s/%s: %s", e.rule.Namespace, e.rule.Name, e.kustoClient.Endpoint(e.rule.Database), e.rule.Database, err)

		if !isUserError(err) {
			metrics.QueryHealth.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(0)
			return
		}

		summary, err := KustoQueryLinks(fmt.Sprintf("This query is failing to execute:<br/><br/><pre>%s</pre><br/><br/>", err.Error()), queryContext.Query, e.kustoClient.Endpoint(e.rule.Database), e.rule.Database)
		if err != nil {
			logger.Errorf("Failed to send failure alert for %s/%s: %s", e.rule.Namespace, e.rule.Name, err)
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
			logger.Errorf("Failed to send failure alert for %s/%s: %s", e.rule.Namespace, e.rule.Name, err)
			// Only set the notification as failed if we are not able to send a failure alert directly.
			metrics.NotificationUnhealthy.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(1)
			return
		}
		// Query failed due to user error, so return the query to healthy.
		metrics.QueryHealth.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(1)
		return
	}

	metrics.QueryHealth.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(1)
	logger.Infof("Completed %s/%s in %s", e.rule.Namespace, e.rule.Name, time.Since(start))
	logger.Infof("Query for %s/%s completed with %d entries found", e.rule.Namespace, e.rule.Name, rows)
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

func isUserError(err error) bool {
	if err == nil {
		return false
	}

	// User specified a database in their CRD that adx-mon does not have configured.
	var unknownDB *UnknownDBError
	if errors.As(err, &unknownDB) {
		return true
	}

	// User's query results are missing a required column, or they are the wrong type.
	var validationErr *NotificationValidationError
	if errors.As(err, &validationErr) {
		return true
	}

	// Look to see if a kusto query error is specific to how the query was defined and not due to problems with adx-mon itself.
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
