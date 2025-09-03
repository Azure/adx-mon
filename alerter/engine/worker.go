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
	alertrulev1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	kerrors "github.com/Azure/azure-kusto-go/kusto/data/errors"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/google/cel-go/cel"
)

const (
	maxQueryTime = 5 * time.Minute
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
	HandlerFn       func(ctx context.Context, endpoint string, qc *QueryContext, row *table.Row) error
	ctrlCli         client.Client
	alertsGenerated int // Track alerts generated in current execution

	// CEL related cached compilation
	criteriaCELCompiled bool
	criteriaCELEnv      *cel.Env
	criteriaCELProgram  cel.Program
	criteriaCELErr      error
}

// BuildCELEnvFromTags constructs a CEL environment declaring each tag key as a string variable.
// Tag keys must already be lowercase.
func BuildCELEnvFromTags(tags map[string]string) (*cel.Env, error) {
	var opts []cel.EnvOption
	for k := range tags {
		opts = append(opts, cel.Variable(k, cel.StringType))
	}
	return cel.NewEnv(opts...)
}

// NewWorker creates a worker instance pre-building the CEL environment from tags.
// The CEL expression itself is compiled lazily on first ExecuteQuery call.
func NewWorker(rule *rules.Rule, region string, tags map[string]string, kustoClient Client, alertCli interface{ Create(ctx context.Context, endpoint string, alert alert.Alert) error }, alertAddr string, handlerFn func(ctx context.Context, endpoint string, qc *QueryContext, row *table.Row) error, ctrlCli client.Client) *worker {
	// Lowercase tags for case-insensitive matching and consistent CEL variable naming.
	lowered := make(map[string]string, len(tags))
	for k, v := range tags {
		lowered[strings.ToLower(k)] = strings.ToLower(v)
	}
	// Build CEL environment (ignore error here; stored for lazy compilation logging later)
	env, err := BuildCELEnvFromTags(lowered)
	w := &worker{
		rule:        rule,
		Region:      region,
		tags:        lowered,
		kustoClient: kustoClient,
		AlertCli:    alertCli,
		AlertAddr:   alertAddr,
		HandlerFn:   handlerFn,
		ctrlCli:     ctrlCli,
		criteriaCELEnv: env,
	}
	if err != nil {
		w.criteriaCELErr = fmt.Errorf("failed to build CEL env: %w", err)
		logger.Errorf("%v", w.criteriaCELErr)
	}
	return w
}

func (e *worker) Run(ctx context.Context) {
	e.wg.Add(1)

	e.mu.Lock()
	ctx, e.cancel = context.WithCancel(ctx)
	e.mu.Unlock()

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
		return time.Now().Add(-time.Second) // Immediate execution
	}

	// Calculate next execution time based on last execution + interval
	lastQueryTime := e.rule.LastQueryTime
	nextQueryTime := lastQueryTime.Add(e.rule.Interval)

	return nextQueryTime
}

func (e *worker) ExecuteQuery(ctx context.Context) {
	// Check if the rule is enabled for this instance by matching any of the alert criteria tags.
	var criteriaMatched bool
	// Existing map-based criteria (OR semantics across map entries)
	if len(e.rule.Criteria) == 0 {
		criteriaMatched = true
	} else {
		for k, v := range e.rule.Criteria {
			lowerKey := strings.ToLower(k)
			if vv, ok := e.tags[lowerKey]; ok {
				for _, value := range v {
					if strings.EqualFold(vv, value) {
						criteriaMatched = true
						break
					}
				}
			}
			if criteriaMatched {
				break
			}
		}
	}

	// CEL expression evaluation (lazy compile of expression only; env built at worker construction).
	var expressionMatched bool
	if e.rule.CriteriaExpression != "" {
		if !e.criteriaCELCompiled { // Compile expression once per worker.
			if e.criteriaCELEnv == nil {
				e.criteriaCELErr = fmt.Errorf("CEL environment not initialized for %s/%s", e.rule.Namespace, e.rule.Name)
				logger.Errorf("%v", e.criteriaCELErr)
				e.criteriaCELCompiled = true
			} else {
				ast, iss := e.criteriaCELEnv.Parse(e.rule.CriteriaExpression)
				if iss.Err() != nil {
					e.criteriaCELErr = iss.Err()
				} else {
					astChecked, iss2 := e.criteriaCELEnv.Check(ast)
					if iss2.Err() != nil {
						e.criteriaCELErr = iss2.Err()
					} else {
						e.criteriaCELProgram, e.criteriaCELErr = e.criteriaCELEnv.Program(astChecked)
					}
				}
				if e.criteriaCELErr != nil {
					logger.Errorf("Failed to compile CEL criteria expression for %s/%s: %v", e.rule.Namespace, e.rule.Name, e.criteriaCELErr)
				}
				e.criteriaCELCompiled = true
			}
		}
		if e.criteriaCELErr == nil {
			activation := map[string]interface{}{}
			for k, v := range e.tags { // tags already lowered
				activation[k] = v
			}
			out, _, err := e.criteriaCELProgram.Eval(activation)
			if err != nil {
				logger.Errorf("CEL evaluation error for %s/%s: %v", e.rule.Namespace, e.rule.Name, err)
			} else if b, ok := out.Value().(bool); ok && b {
				expressionMatched = true
			}
		}
	}

	// Execution decision: map criteria OR expression (if either true, proceed). If neither defined treat as allowed.
	if !(criteriaMatched || expressionMatched) {
		logger.Infof("Skipping %s/%s on %s/%s because criteria/expression did not match. tags=%v expr=%q", e.rule.Namespace, e.rule.Name, e.kustoClient.Endpoint(e.rule.Database), e.rule.Database, e.tags, e.rule.CriteriaExpression)
		return
	}

	// Try to acquire a worker slot
	queue.Workers <- struct{}{}

	// Release the worker slot
	defer func() { <-queue.Workers }()

	ctx, cancel := context.WithTimeout(ctx, maxQueryTime)
	defer cancel()

	endTime := time.Now().UTC()

	// Reset alerts counter for this execution
	e.alertsGenerated = 0

	queryContext, err := NewQueryContext(e.rule, endTime, e.Region)
	if err != nil {
		logger.Errorf("Failed to wrap query=%s/%s on %s/%s: %s", e.rule.Namespace, e.rule.Name, e.kustoClient.Endpoint(e.rule.Database), e.rule.Database, err)
		e.updateAlertRuleStatus(ctx, endTime, 0, "Error", fmt.Sprintf("Failed to wrap query: %v", err))
		return
	}

	logger.Infof("Executing %s/%s on %s/%s", e.rule.Namespace, e.rule.Name, e.kustoClient.Endpoint(e.rule.Database), e.rule.Database)

	// Create a wrapper handler that tracks alerts generated
	wrappedHandler := func(ctx context.Context, endpoint string, qc *QueryContext, row *table.Row) error {
		err := e.HandlerFn(ctx, endpoint, qc, row)
		if err == nil {
			// If HandlerFn succeeded, it means an alert was generated
			e.alertsGenerated++
		}
		return err
	}

	err, rows := e.kustoClient.Query(ctx, queryContext, wrappedHandler)
	if err != nil {
		// This failed because we sent too many notifications.
		if errors.Is(err, alert.ErrTooManyRequests) {
			err := e.AlertCli.Create(ctx, e.AlertAddr, alert.Alert{
				Destination:   e.rule.Destination,
				Title:         fmt.Sprintf("Alert %s/%s has too many notifications in %s", e.rule.Namespace, e.rule.Name, e.Region),
				Summary:       "This alert has been throttled by ICM due to too many notifications.  Please reduce the number of notifications for this alert.",
				Severity:      3,
				Source:        fmt.Sprintf("notification-failure/%s/%s", e.rule.Namespace, e.rule.Name),
				CorrelationID: fmt.Sprintf("notification-failure/%s/%s", e.rule.Namespace, e.rule.Name),
			})
			if err != nil {
				logger.Errorf("Failed to send alert for throttled notification for %s/%s: %s", e.rule.Namespace, e.rule.Name, err)
			}
			e.updateAlertRuleStatus(ctx, endTime, 0, "Throttled", "Too many notifications sent")
			return
		}

		// This failed because the query failed.
		logger.Errorf("Failed to execute query=%s/%s on %s/%s: %s", e.rule.Namespace, e.rule.Name, e.kustoClient.Endpoint(e.rule.Database), e.rule.Database, err)

		if !isUserError(err) {
			metrics.QueryHealth.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(0)
			e.updateAlertRuleStatus(ctx, endTime, 0, "Error", fmt.Sprintf("Query execution failed: %v", err))
			return
		}

		// Store the original query error before it gets overwritten
		originalQueryErr := err

		summary, err := KustoQueryLinks(fmt.Sprintf("This query is failing to execute:<br/><br/><pre>%s</pre><br/><br/>", originalQueryErr.Error()), queryContext.Query, e.kustoClient.Endpoint(e.rule.Database), e.rule.Database)
		if err != nil {
			logger.Errorf("Failed to send failure alert for %s/%s: %s", e.rule.Namespace, e.rule.Name, err)
			metrics.NotificationUnhealthy.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(1)
			e.updateAlertRuleStatus(ctx, endTime, 0, "Error", fmt.Sprintf("Query failed and unable to create failure alert: %v", originalQueryErr))
			return
		}

		endpointBaseName, _ := strings.CutPrefix(e.kustoClient.Endpoint(e.rule.Database), "https://")
		err = e.AlertCli.Create(ctx, e.AlertAddr, alert.Alert{
			Destination:   e.rule.Destination,
			Title:         fmt.Sprintf("Alert %s/%s has query errors on %s", e.rule.Namespace, e.rule.Name, e.kustoClient.Endpoint(e.rule.Database)),
			Summary:       summary,
			Severity:      3,
			Source:        fmt.Sprintf("%s/%s", e.rule.Namespace, e.rule.Name),
			CorrelationID: fmt.Sprintf("alert-failure/%s/%s/%s", endpointBaseName, e.rule.Namespace, e.rule.Name),
		})

		if err != nil {
			logger.Errorf("Failed to send failure alert for %s/%s/%s: %s", endpointBaseName, e.rule.Namespace, e.rule.Name, err)
			// Only set the notification as failed if we are not able to send a failure alert directly.
			metrics.NotificationUnhealthy.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(1)
			e.updateAlertRuleStatus(ctx, endTime, 0, "Error", fmt.Sprintf("Query failed and unable to send failure alert: %v", originalQueryErr))
			return
		} else {
			metrics.NotificationUnhealthy.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(0)
		}
		// Query failed due to user error, so return the query to healthy.
		metrics.QueryHealth.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(1)
		e.updateAlertRuleStatus(ctx, endTime, 0, "Error", fmt.Sprintf("Query failed with user error: %v", originalQueryErr))
		return
	}

	metrics.QueryHealth.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(1)
	metrics.QueriesRunTotal.WithLabelValues().Inc()
	logger.Infof("Completed %s/%s in %s", e.rule.Namespace, e.rule.Name, time.Since(endTime))
	logger.Infof("Query for %s/%s completed with %d entries found", e.rule.Namespace, e.rule.Name, rows)

	// Update AlertRule status with execution information
	e.updateAlertRuleStatus(ctx, endTime, e.alertsGenerated, "Success", "")
}

// updateAlertRuleStatus updates the AlertRule status with the execution information
func (e *worker) updateAlertRuleStatus(ctx context.Context, executionTime time.Time, alertsGenerated int, status, message string) {
	// Skip status update if we don't have a Kubernetes client
	if e.ctrlCli == nil {
		return
	}

	// Create a context for the status update using the passed-in context as parent
	updateCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Get the current AlertRule
	alertRule := &alertrulev1.AlertRule{}
	err := e.ctrlCli.Get(updateCtx, types.NamespacedName{
		Namespace: e.rule.Namespace,
		Name:      e.rule.Name,
	}, alertRule)
	if err != nil {
		logger.Errorf("Failed to get AlertRule %s/%s for status update: %v", e.rule.Namespace, e.rule.Name, err)
		return
	}

	// Update the status fields using existing fields
	executionTimeMeta := metav1.NewTime(executionTime)
	alertRule.Status.LastQueryTime = executionTimeMeta
	if alertsGenerated > 0 {
		alertRule.Status.LastAlertTime = executionTimeMeta
	}
	alertRule.Status.Status = status
	alertRule.Status.Message = message

	// Update the AlertRule status
	err = e.ctrlCli.Status().Update(updateCtx, alertRule)
	if err != nil {
		logger.Errorf("Failed to update AlertRule %s/%s status: %v", e.rule.Namespace, e.rule.Name, err)
		return
	}

	logger.Debugf("Updated AlertRule %s/%s status: LastQueryTime=%v, LastAlertTime=%v, Status=%s",
		e.rule.Namespace, e.rule.Name, executionTime, alertRule.Status.LastAlertTime, status)
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
