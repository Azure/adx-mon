package adx

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	v1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/ingestor/storage"
	"github.com/Azure/adx-mon/pkg/celutil"
	crdownership "github.com/Azure/adx-mon/pkg/crd/summaryrule"
	"github.com/Azure/adx-mon/pkg/kustoutil"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/data/errors"
	"github.com/Azure/azure-kusto-go/kusto/kql"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/clock"
)

type TableDetail struct {
	TableName       string  `kusto:"TableName"`
	HotExtentSize   float64 `kusto:"HotExtentSize"`
	TotalExtentSize float64 `kusto:"TotalExtentSize"`
	TotalExtents    int64   `kusto:"TotalExtents"`
	HotRowCount     int64   `kusto:"HotRowCount"`
	TotalRowCount   int64   `kusto:"TotalRowCount"`
}

type DropUnusedTablesTask struct {
	mu           sync.Mutex
	unusedTables map[string]int
	kustoCli     StatementExecutor
	database     string
}

type StatementExecutor interface {
	Database() string
	Endpoint() string
	Mgmt(ctx context.Context, query kusto.Statement, options ...kusto.MgmtOption) (*kusto.RowIterator, error)
}

func NewDropUnusedTablesTask(kustoCli StatementExecutor) *DropUnusedTablesTask {
	return &DropUnusedTablesTask{
		unusedTables: make(map[string]int),
		kustoCli:     kustoCli,
	}
}

func (t *DropUnusedTablesTask) Run(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	details, err := t.loadTableDetails(ctx)
	if err != nil {
		return fmt.Errorf("error loading table details: %w", err)
	}

	for _, v := range details {
		if v.TotalRowCount > 0 {
			delete(t.unusedTables, v.TableName)
		}

		if v.TotalRowCount == 0 {
			t.unusedTables[v.TableName]++
			logger.Infof("Marking table %s.%s as unused", t.database, v.TableName)
		}
	}

	for table, count := range t.unusedTables {
		if count > 2 {
			logger.Infof("DRYRUN Dropping unused table %s.%s", t.kustoCli.Database(), table)
			// stmt := kusto.NewStmt("", kusto.UnsafeStmt(unsafe.Stmt{Add: true, SuppressWarning: true})).UnsafeAdd(fmt.Sprintf(".drop table %s", table))
			// if _, err := t.kustoCli.Mgmt(ctx, stmt); err != nil {
			// 	return fmt.Errorf("error dropping table %s: %w", table, err)
			// }
			delete(t.unusedTables, table)
		}
	}
	return nil
}

func (t *DropUnusedTablesTask) loadTableDetails(ctx context.Context) ([]TableDetail, error) {
	stmt := kusto.NewStmt(".show tables details | project TableName, HotExtentSize, TotalExtentSize, TotalExtents, HotRowCount, TotalRowCount")
	rows, err := t.kustoCli.Mgmt(ctx, stmt)
	if err != nil {
		return nil, err
	}

	var tables []TableDetail
	for {
		row, err1, err2 := rows.NextRowOrError()
		if err2 == io.EOF {
			return tables, nil
		} else if err1 != nil {
			return tables, err1
		} else if err2 != nil {
			return tables, err2
		}

		var v TableDetail
		if err := row.ToStruct(&v); err != nil {
			return tables, err
		}
		tables = append(tables, v)
	}
}

type SyncFunctionsTask struct {
	store    storage.Functions
	kustoCli StatementExecutor
	// ClusterLabels carries the ingestor's cluster identity for criteriaExpression evaluation.
	ClusterLabels map[string]string
}

func NewSyncFunctionsTask(store storage.Functions, kustoCli StatementExecutor, clusterLabels map[string]string) *SyncFunctionsTask {
	return &SyncFunctionsTask{
		store:         store,
		kustoCli:      kustoCli,
		ClusterLabels: clusterLabels,
	}
}

func (t *SyncFunctionsTask) Run(ctx context.Context) error {
	functions, err := t.store.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list functions: %w", err)
	}
	for _, function := range functions {
		availableDB := t.kustoCli.Database()
		configuredDB := function.Spec.Database
		if configuredDB != v1.AllDatabases && !strings.EqualFold(configuredDB, availableDB) {
			function.SetDatabaseMatchCondition(false, configuredDB, availableDB)
			message := fmt.Sprintf("Function skipped due to database mismatch (configured %q, available %q)", configuredDB, availableDB)
			function.SetReconcileCondition(metav1.ConditionFalse, "DatabaseMismatchSkipped", message)
			function.Status.Status = v1.Failed
			function.Status.Error = ""
			function.Status.Reason = "DatabaseMismatch"
			function.Status.Message = message
			if err := t.store.UpdateStatus(ctx, function); err != nil {
				logger.Errorf("Failed to update function status for %s.%s after database mismatch: %v", configuredDB, function.Name, err)
			}
			continue
		}
		function.SetDatabaseMatchCondition(true, configuredDB, availableDB)

		expr := strings.TrimSpace(function.Spec.CriteriaExpression)
		if expr == "" {
			function.SetCriteriaMatchCondition(true, expr, nil, t.ClusterLabels)
		} else {
			ok, err := celutil.EvaluateCriteriaExpression(t.ClusterLabels, expr)
			if err != nil {
				err = fmt.Errorf("criteriaExpression evaluation failed: %w", err)
				logger.Errorf("Function %s/%s criteriaExpression error: %v", function.Namespace, function.Name, err)
				function.SetCriteriaMatchCondition(false, expr, err, t.ClusterLabels)
				function.SetReconcileCondition(metav1.ConditionFalse, "CriteriaEvaluationFailed", err.Error())
				if updErr := t.updateKQLFunctionStatus(ctx, function, v1.Failed, err); updErr != nil {
					logger.Errorf("Failed to update function status for %s.%s: %v", function.Spec.Database, function.Name, updErr)
				}
				continue
			}
			if !ok {
				function.SetCriteriaMatchCondition(false, expr, nil, t.ClusterLabels)
				message := fmt.Sprintf("Function skipped because criteria expression evaluated to false for cluster labels: %s", formatClusterLabelsForMessage(t.ClusterLabels))
				function.SetReconcileCondition(metav1.ConditionFalse, "CriteriaNotMatched", message)
				function.Status.Status = v1.Failed
				function.Status.Error = ""
				function.Status.Reason = "CriteriaNotMatched"
				function.Status.Message = message
				if err := t.store.UpdateStatus(ctx, function); err != nil {
					logger.Errorf("Failed to update function status for %s.%s after criteria mismatch: %v", function.Spec.Database, function.Name, err)
				}
				continue
			}
			function.SetCriteriaMatchCondition(true, expr, nil, t.ClusterLabels)
		}

		if !function.DeletionTimestamp.IsZero() {
			function.SetReconcileCondition(metav1.ConditionFalse, "FunctionDeleting", "Function deletion in progress")
			function.Status.Status = v1.Failed
			function.Status.Error = ""
			if err := t.store.UpdateStatus(ctx, function); err != nil {
				logger.Errorf("Failed to update function status for %s.%s prior to deletion: %v", function.Spec.Database, function.Name, err)
			}

			stmt := kql.New(".drop function ").AddUnsafe(function.Name).AddLiteral(" ifexists")
			if _, err := t.kustoCli.Mgmt(ctx, stmt); err != nil {
				parsed := kustoutil.ParseError(err)
				logger.Errorf("Failed to delete function %s.%s: %v", function.Spec.Database, function.Name, err)
				function.SetReconcileCondition(metav1.ConditionFalse, "FunctionDeletionFailed", parsed)
				function.Status.Status = v1.Failed
				function.Status.Error = parsed
				if err := t.store.UpdateStatus(ctx, function); err != nil {
					logger.Errorf("Failed to persist deletion failure for %s.%s: %v", function.Spec.Database, function.Name, err)
				}
				continue
			}

			function.SetReconcileCondition(metav1.ConditionTrue, "FunctionDeleted", fmt.Sprintf("Function successfully deleted from %s", availableDB))
			if err := t.updateKQLFunctionStatus(ctx, function, v1.Success, nil); err != nil {
				logger.Errorf("Failed to update success status following deletion for %s.%s: %v", function.Spec.Database, function.Name, err)
			}
			continue
		}

		if t.kustoCli.Endpoint() != function.Spec.AppliedEndpoint || function.Status.Status != v1.Success || function.GetGeneration() != function.Status.ObservedGeneration {
			stmt := kql.New(".execute database script with (ThrowOnErrors=true) <| ").AddUnsafe(function.Spec.Body)
			if _, err := t.kustoCli.Mgmt(ctx, stmt); err != nil {
				parsed := kustoutil.ParseError(err)
				if !errors.Retry(err) {
					logger.Errorf("Permanent failure to create function %s.%s: %v", function.Spec.Database, function.Name, err)
					function.SetReconcileCondition(metav1.ConditionFalse, "KustoExecutionFailed", parsed)
					if err = t.updateKQLFunctionStatus(ctx, function, v1.PermanentFailure, err); err != nil {
						logger.Errorf("Failed to update permanent failure status: %v", err)
					}
					continue
				}
				function.SetReconcileCondition(metav1.ConditionFalse, "KustoExecutionRetrying", parsed)
				if err := t.updateKQLFunctionStatus(ctx, function, v1.Failed, err); err != nil {
					logger.Errorf("Failed to persist transient failure for %s.%s: %v", function.Spec.Database, function.Name, err)
				}
				logger.Warnf("Transient failure to create function %s.%s: %v", function.Spec.Database, function.Name, err)
				continue
			}

			logger.Infof("Successfully created function %s.%s", function.Spec.Database, function.Name)
			if t.kustoCli.Endpoint() != function.Spec.AppliedEndpoint {
				function.Spec.AppliedEndpoint = t.kustoCli.Endpoint()
				if err := t.store.Update(ctx, function); err != nil {
					logger.Errorf("Failed to update function %s.%s: %v", function.Spec.Database, function.Name, err)
				}
			}

			function.SetReconcileCondition(metav1.ConditionTrue, "KustoExecutionSucceeded", fmt.Sprintf("Function created at %s", t.kustoCli.Endpoint()))
			if err := t.updateKQLFunctionStatus(ctx, function, v1.Success, nil); err != nil {
				logger.Errorf("Failed to update success status: %v", err)
			}
		}
	}

	return nil
}

func (t *SyncFunctionsTask) updateKQLFunctionStatus(ctx context.Context, fn *v1.Function, status v1.FunctionStatusEnum, err error) error {
	fn.Status.Status = status
	if err != nil {
		fn.Status.Error = kustoutil.ParseError(err)
	} else if fn.Status.Error != "" {
		fn.Status.Error = ""
	}
	if err := t.store.UpdateStatus(ctx, fn); err != nil {
		return fmt.Errorf("failed to update status for function %s.%s: %w", fn.Spec.Database, fn.Name, err)
	}
	return nil
}

type ManagementCommandTask struct {
	store    storage.CRDHandler
	kustoCli StatementExecutor
	// ClusterLabels mirrors the ingestor cluster identity for management-command gating.
	ClusterLabels map[string]string
}

func NewManagementCommandsTask(store storage.CRDHandler, kustoCli StatementExecutor, clusterLabels map[string]string) *ManagementCommandTask {
	return &ManagementCommandTask{
		store:         store,
		kustoCli:      kustoCli,
		ClusterLabels: clusterLabels,
	}
}

func (t *ManagementCommandTask) Run(ctx context.Context) error {
	managementCommands := &v1.ManagementCommandList{}
	if err := t.store.List(ctx, managementCommands, storage.FilterCompleted); err != nil {
		return fmt.Errorf("failed to list management commands: %w", err)
	}
	for _, command := range managementCommands.Items {
		// ManagementCommands database is optional as not all commands are scoped at the database level
		if command.Spec.Database != "" && command.Spec.Database != t.kustoCli.Database() {
			continue
		}

		if expr := command.Spec.CriteriaExpression; expr != "" {
			ok, err := celutil.EvaluateCriteriaExpression(t.ClusterLabels, expr)
			if err != nil {
				err = fmt.Errorf("criteriaExpression evaluation failed: %w", err)
				logger.Errorf("ManagementCommand %s/%s criteriaExpression error: %v", command.Namespace, command.Name, err)
				if updErr := t.store.UpdateStatus(ctx, &command, err); updErr != nil {
					logger.Errorf("Failed to update management command status for %s/%s: %v", command.Namespace, command.Name, updErr)
				}
				continue
			}
			if !ok {
				if logger.IsDebug() {
					logger.Debugf("Skipping management command %s/%s due to criteriaExpression evaluating to false", command.Namespace, command.Name)
				}
				continue
			}
		}

		var stmt *kql.Builder
		if command.Spec.Database == "" {
			stmt = kql.New(".execute cluster script with (ThrowOnErrors = true) <|").AddUnsafe(command.Spec.Body)
		} else {
			stmt = kql.New(".execute database script with (ThrowOnErrors = true) <|").AddUnsafe(command.Spec.Body)
		}
		if _, err := t.kustoCli.Mgmt(ctx, stmt); err != nil {
			logger.Errorf("Failed to execute management command %s.%s: %v", command.Spec.Database, command.Name, err)
			if err = t.store.UpdateStatus(ctx, &command, err); err != nil {
				logger.Errorf("Failed to update management command status: %v", err)
			}
		}

		logger.Infof("Successfully executed management command %s.%s", command.Spec.Database, command.Name)
		if err := t.store.UpdateStatus(ctx, &command, nil); err != nil {
			logger.Errorf("Failed to update success status: %v", err)
		}
	}

	return nil
}

type SummaryRuleTask struct {
	store         storage.CRDHandler
	kustoCli      StatementExecutor
	SubmitRule    func(ctx context.Context, rule v1.SummaryRule, startTime, endTime string) (string, error)
	ClusterLabels map[string]string
	Clock         clock.Clock
}

func NewSummaryRuleTask(store storage.CRDHandler, kustoCli StatementExecutor, clusterLabels map[string]string) *SummaryRuleTask {
	task := &SummaryRuleTask{
		store:         store,
		kustoCli:      kustoCli,
		ClusterLabels: clusterLabels,
		Clock:         clock.RealClock{},
	}
	// Set the default implementations
	task.SubmitRule = task.submitRule
	return task
}

type KustoAsyncOperationState string

const (
	// KustoAsyncOperationStateCompleted indicates that the async operation has completed successfully
	KustoAsyncOperationStateCompleted KustoAsyncOperationState = "Completed"
	// KustoAsyncOperationStateFailed indicates that the async operation has failed
	KustoAsyncOperationStateFailed KustoAsyncOperationState = "Failed"
	// KustoAsyncOperationStateInProgress indicates that the async operation is in progress
	KustoAsyncOperationStateInProgress KustoAsyncOperationState = "InProgress"
	// KustoAsyncOperationStateThrottled indicates that the async operation is being throttled
	KustoAsyncOperationStateThrottled KustoAsyncOperationState = "Throttled"
	// KustoAsyncOperationStateScheduled indicates that the async operation is scheduled
	KustoAsyncOperationStateScheduled KustoAsyncOperationState = "Scheduled"
)

// IsKustoAsyncOperationStateCompleted returns true if the async operation is completed
// (either succeeded or failed)
// This is used to determine if we should poll the async operation's status.
func IsKustoAsyncOperationStateCompleted(state string) bool {
	return state == string(KustoAsyncOperationStateCompleted) ||
		state == string(KustoAsyncOperationStateFailed)
}

func formatClusterLabelsForMessage(labels map[string]string) string {
	if len(labels) == 0 {
		return "(none)"
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, labels[k]))
	}
	return strings.Join(parts, ", ")
}

// matchesCriteria checks if the given criteria matches any of the cluster labels.
// It uses case-insensitive matching for both keys and values.
// Returns true if no criteria are specified (empty criteria means always match).
// Returns true if any criterion matches (OR logic between criteria).
func matchesCriteria(criteria map[string][]string, clusterLabels map[string]string) bool {
	// If no criteria are specified, always match
	if len(criteria) == 0 {
		return true
	}

	// Check if any criterion matches
	for k, v := range criteria {
		lowerKey := strings.ToLower(k)
		// Look for matching cluster label (case-insensitive key matching)
		for labelKey, labelValue := range clusterLabels {
			if strings.ToLower(labelKey) == lowerKey {
				for _, value := range v {
					if strings.ToLower(labelValue) == strings.ToLower(value) {
						return true // Found a match, return immediately
					}
				}
				break // We found the key, no need to check other label keys
			}
		}
	}

	return false // No criteria matched
}

// Run executes the SummaryRuleTask which manages summary rules and their associated
// Kusto async operations. It handles rule submission, operation tracking, and status updates.
func (t *SummaryRuleTask) Run(ctx context.Context) error {
	// Set a timeout to prevent hanging.
	// If the loop takes more than 5 minutes, something is wrong and we should just cancel and try again next iteration.
	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	summaryRules, err := t.initializeRun(timeoutCtx)
	if err != nil {
		return err
	}

	// Process each summary rule individually
	for _, rule := range summaryRules.Items {
		if !t.shouldProcessRule(rule) {
			t.logSummaryRule(&rule, true, nil)
			continue
		}

		// Handle rule execution logic (timing evaluation and submission)
		err := t.handleRuleExecution(timeoutCtx, &rule)

		// If the submission failed we intentionally skip backfill generation for this
		// cycle. Backfilling when the cluster is offline (or submissions are failing)
		// causes us to enqueue synthetic backlog windows that will immediately be
		// retried again next cycle, inflating the async operation list. The backlog
		// will be reconstructed on the first successful submission using the last
		// successful execution timestamp, so skipping here is safe and prevents
		// unbounded growth / test expectation mismatch.
		if err == nil {
			// Process any backlog operations for this rule (only when submission
			// succeeded this cycle).
			rule.BackfillAsyncOperations(t.Clock)
		}
		// NOTE: If we skipped BackfillAsyncOperations due to an error we still want
		// to attempt recovery of any pre-existing backlog operations as the
		// underlying outage may have been transient after submission; however, we
		// only attempt to process operations that already exist (trackAsyncOperations).

		// Process any outstanding async operations for this rule
		t.trackAsyncOperations(timeoutCtx, &rule)

		t.logSummaryRule(&rule, false, err)
		// Update the rule's primary status condition
		if err := t.updateSummaryRuleStatus(timeoutCtx, &rule, err); err != nil {
			logger.Errorf("Failed to update summary rule status: %v", err)
			// Not a lot we can do here, we'll end up just retrying next interval.
		}
	}

	return nil
}

func (t *SummaryRuleTask) initializeRun(ctx context.Context) (*v1.SummaryRuleList, error) {
	// Fetch all summary rules from storage
	summaryRules := &v1.SummaryRuleList{}
	if err := t.store.List(ctx, summaryRules); err != nil {
		return nil, fmt.Errorf("failed to list summary rules: %w", err)
	}

	return summaryRules, nil
}

func (t *SummaryRuleTask) shouldProcessRule(rule v1.SummaryRule) bool {
	// Ownership gating via shared helper: default (missing) goes to ingestor for backward compatibility.
	if !crdownership.IsOwnedBy(&rule, v1.SummaryRuleOwnerIngestor) {
		logger.Logger().Info("Ownership annotation not ingestor; skipping processing",
			"crd_name", fmt.Sprintf("%s/%s", rule.Namespace, rule.Name),
			"event", "control_handed_over",
		)
		return false
	}

	// Skip rules not belonging to the current database
	if rule.Spec.Database != t.kustoCli.Database() {
		if logger.IsDebug() {
			logger.Debugf("Skipping %s/%s on %s because it does not match the current database: %s", rule.Namespace, rule.Name, t.kustoCli.Database(), rule.Spec.Database)
		}
		return false
	}

	// Legacy criteria map (OR semantics). Must match if provided.
	if !matchesCriteria(rule.Spec.Criteria, t.ClusterLabels) {
		if logger.IsDebug() {
			logger.Debugf("Skipping %s/%s on %s because none of the criteria matched cluster labels: %v", rule.Namespace, rule.Name, rule.Spec.Database, t.ClusterLabels)
		}
		return false
	}

	// CEL criteriaExpression (AND semantics with criteria map) if provided.
	if rule.Spec.CriteriaExpression != "" {
		ok, err := celutil.EvaluateCriteriaExpression(t.ClusterLabels, rule.Spec.CriteriaExpression)
		if err != nil {
			logger.Errorf("Skipping %s/%s due to criteriaExpression error: %v", rule.Namespace, rule.Name, err)
			return false
		}
		if !ok {
			if logger.IsDebug() {
				logger.Debugf("Skipping %s/%s because criteriaExpression evaluated to false", rule.Namespace, rule.Name)
			}
			return false
		}
	}

	return true
}

// handleRuleExecution handles the execution logic for a single summary rule.
// It evaluates whether the rule should be submitted based on its interval and timing,
// and if so, submits the rule as an async operation to Kusto.
// Returns any submission error that occurred.
func (t *SummaryRuleTask) handleRuleExecution(ctx context.Context, rule *v1.SummaryRule) error {
	// Get the current condition of the rule
	cnd := rule.GetCondition()
	if cnd == nil {
		// For first-time execution, initialize the condition with a timestamp
		// that's one interval back from current time
		cnd = &metav1.Condition{
			LastTransitionTime: metav1.Time{Time: t.Clock.Now().Add(-rule.Spec.Interval.Duration)},
		}
	}

	// Calculate the next execution window based on the last successful execution
	windowStartTime, windowEndTime := rule.NextExecutionWindow(t.Clock)

	if rule.ShouldSubmitRule(t.Clock) {
		// Subtract 1 tick (100 nanoseconds, the smallest time unit supported by Kusto datetime)
		// from endTime for the query to avoid boundary issues while keeping the original
		// windowEndTime for status tracking. This allows users to use `between(_startTime .. _endTime)`
		// in their query without worrying about the boundary issue.
		queryEndTime := windowEndTime.Add(-kustoutil.OneTick)

		// Prepare a new async operation with calculated time range
		asyncOp := v1.AsyncOperation{
			StartTime: windowStartTime.Format(time.RFC3339Nano),
			EndTime:   queryEndTime.Format(time.RFC3339Nano),
		}
		operationId, err := t.SubmitRule(ctx, *rule, asyncOp.StartTime, asyncOp.EndTime)
		asyncOp.OperationId = operationId
		rule.SetAsyncOperation(asyncOp)
		rule.SetLastExecutionTime(windowEndTime)

		if err != nil {
			return fmt.Errorf("failed to submit rule %s.%s: %w", rule.Spec.Database, rule.Name, err)
		}
	}

	return nil
}

// trackAsyncOperations processes pending async operations for a SummaryRule.
func (t *SummaryRuleTask) trackAsyncOperations(ctx context.Context, rule *v1.SummaryRule) {
	operations := rule.GetAsyncOperations()
	for _, op := range operations {

		// Check for backlog operations, or those that failed to be submitted. We need to attempt
		// executing these so that we don't skip any windows.
		if op.OperationId == "" {
			t.processBacklogOperation(ctx, rule, op)
			continue
		}

		kustoOp, err := t.getOperation(ctx, op.OperationId)
		if err != nil {
			logger.Errorf("Failed to query operation %s: %v", op.OperationId, err)
			continue
		}
		if kustoOp == nil {
			// Operation not found - could be old or completed
			continue
		}

		if IsKustoAsyncOperationStateCompleted(kustoOp.State) {
			t.handleCompletedOperation(ctx, rule, *kustoOp)
		}
		if kustoOp.ShouldRetry != 0 {
			t.handleRetryOperation(ctx, rule, op, *kustoOp)
		}
	}
}

func (t *SummaryRuleTask) handleRetryOperation(ctx context.Context, rule *v1.SummaryRule, operation v1.AsyncOperation, kustoOp AsyncOperationStatus) {
	logger.Infof("Async operation %s for rule %s.%s is marked for retry, retrying submission", kustoOp.OperationId, rule.Spec.Database, rule.Name)
	if operationId, err := t.SubmitRule(ctx, *rule, operation.StartTime, operation.EndTime); err == nil && operationId != operation.OperationId {
		// We've resubmitted an async operation due to a recoverable failure but this has created a new operation-id to track.
		// Remove the existing async operation, we're done processing it, but save the newly created async operation for tracking.
		rule.RemoveAsyncOperation(operation.OperationId)

		// The new operation has all the same time window as the previous, we only
		// need to update the OperationId.
		operation.OperationId = operationId
		rule.SetAsyncOperation(operation)
	}
}

func (t *SummaryRuleTask) handleCompletedOperation(ctx context.Context, rule *v1.SummaryRule, kustoOp AsyncOperationStatus) {
	if kustoOp.State == string(KustoAsyncOperationStateFailed) {
		// Operation failed - mark the rule as failed
		logger.Errorf("Async operation %s for rule %s.%s failed", kustoOp.OperationId, rule.Spec.Database, rule.Name)

		// Use detailed status message if available, otherwise fall back to generic message
		var err error
		if kustoOp.Status != "" {
			// Truncate status message to prevent excessively long condition messages
			status := kustoOp.Status
			if len(status) > 200 { // Leave room for "async operation {id} failed: " prefix
				status = status[:200]
			}
			err = fmt.Errorf("async operation %s failed: %s", kustoOp.OperationId, status)
		} else {
			err = fmt.Errorf("async operation %s failed", kustoOp.OperationId)
		}

		if updateErr := t.updateSummaryRuleStatus(ctx, rule, err); updateErr != nil {
			logger.Errorf("Failed to update summary rule status for failed operation: %v", updateErr)
		}
	}
	// We're done polling this async operation, so we can remove it from the list
	rule.RemoveAsyncOperation(kustoOp.OperationId)
}

func (t *SummaryRuleTask) processBacklogOperation(ctx context.Context, rule *v1.SummaryRule, operation v1.AsyncOperation) {
	if operationId, err := t.SubmitRule(ctx, *rule, operation.StartTime, operation.EndTime); err == nil {
		// Great, we were able to recover the failed submission window.
		operation.OperationId = operationId

		// Parse the operation EndTime and calculate the original window end time.
		// The operation.EndTime has kustoutil.OneTick (100ns) subtracted in handleRuleExecution
		// to avoid Kusto boundary issues, so we need to add it back for timestamp tracking.
		if operationEndTime, parseErr := time.Parse(time.RFC3339Nano, operation.EndTime); parseErr == nil {
			// Restore the original window end time by adding back the subtracted OneTick
			originalWindowEndTime := operationEndTime.Add(kustoutil.OneTick)

			// Only advance timestamp if this window represents forward progress
			currentTimestamp := rule.GetLastExecutionTime()
			if currentTimestamp == nil {
				currentTimestamp = &time.Time{}
			}
			if originalWindowEndTime.After(*currentTimestamp) {
				rule.SetLastExecutionTime(originalWindowEndTime)
			}
		} else {
			logger.Warnf("Failed to parse operation EndTime '%s' for backlog recovery: %v", operation.EndTime, parseErr)
		}

		// Track the operation after timestamp advancement to maintain consistency.
		// This order ensures that if timestamp advancement succeeds, the operation is tracked;
		// if timestamp parsing fails, we still track the operation but without advancing the timestamp.
		rule.SetAsyncOperation(operation)
	}
}

func (t *SummaryRuleTask) submitRule(ctx context.Context, rule v1.SummaryRule, startTime, endTime string) (string, error) {
	// NOTE: We cannot do something like `let _startTime = datetime();` as dot-command do not permit
	// preceding let-statements.
	body := kustoutil.ApplySubstitutions(rule.Spec.Body, startTime, endTime, t.ClusterLabels)

	// Execute asynchronously
	stmt := kql.New(".set-or-append async ").AddUnsafe(rule.Spec.Table).AddLiteral(" with (extend_schema=true) <| ").AddUnsafe(body)
	res, err := t.kustoCli.Mgmt(ctx, stmt)
	if err != nil {
		return "", fmt.Errorf("failed to execute summary rule %s.%s: %w", rule.Spec.Database, rule.Name, err)
	}

	return operationIDFromResult(res)
}

// getOperation retrieves the status of a specific asynchronous Kusto operation by its operationId.
//
// This method performs an individual operation lookup without time limitations, which eliminates
// the 24-hour limitation bug that affected the previous bulk query approach. It ensures accurate
// retrieval of operations regardless of their age by directly querying for a specific operation ID.
//
// Parameters:
//   - ctx: context.Context for cancellation and timeout control
//   - operationId: string identifier of the Kusto async operation to query
//
// Returns:
//   - *AsyncOperationStatus: pointer to the operation status struct if found, or nil if not found
//   - error: error if the query fails or the result cannot be parsed
//
// The method uses parameterized queries to prevent injection attacks and queries the latest
// operation status using arg_max to ensure we get the most recent state information.
func (t *SummaryRuleTask) getOperation(ctx context.Context, operationId string) (*AsyncOperationStatus, error) {
	// Management commands (those starting with a dot) do NOT support QueryParameters per azure-kusto-go docs.

	stmt := kql.New(`.show operations`).
		AddLiteral(" | where OperationId == ").AddString(operationId).
		AddLiteral(" | summarize arg_max(LastUpdatedOn, OperationId, State, ShouldRetry, Status)").
		AddLiteral(" | project LastUpdatedOn, OperationId = tostring(OperationId), State, ShouldRetry = todouble(ShouldRetry), Status")

	rows, err := t.kustoCli.Mgmt(ctx, stmt)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve operation %s: %w", operationId, err)
	}
	defer rows.Stop()

	for {
		row, errInline, errFinal := rows.NextRowOrError()
		if errFinal == io.EOF {
			break
		}
		if errInline != nil {
			continue
		}
		if errFinal != nil {
			return nil, fmt.Errorf("failed to retrieve operation %s: %v", operationId, errFinal)
		}

		var status AsyncOperationStatus
		if err := row.ToStruct(&status); err != nil {
			return nil, fmt.Errorf("failed to parse operation %s: %v", operationId, err)
		}
		if status.State != "" {
			return &status, nil
		}
	}

	// Operation not found
	return nil, nil
}

// logSummaryRule emits a concise wide log for a SummaryRule.
// When skipped is true we only log identity + skip reason fields.
// When processed we include inflight_ops, last_exec_end, and forward_progress.
func (t *SummaryRuleTask) logSummaryRule(rule *v1.SummaryRule, skipped bool, submissionErr error) {
	if skipped {
		logger.Logger().Info("SummaryRule", "ns", rule.Namespace, "rule", rule.Name, "db", rule.Spec.Database, "table", rule.Spec.Table, "criteria_skipped", true)
		return
	}
	asyncOps := rule.GetAsyncOperations()
	inflightOps := len(asyncOps)
	lastExec := rule.GetLastExecutionTime()
	lastExecStr := ""
	if lastExec != nil && !lastExec.IsZero() {
		lastExecStr = lastExec.UTC().Format(time.RFC3339Nano)
	}
	forward := false
	if submissionErr == nil {
		forward = lastExecStr != "" && lastExec.UTC().Add(rule.Spec.Interval.Duration).After(time.Now().Add(-rule.Spec.Interval.Duration))
	}
	logger.Logger().Info("SummaryRule", "ns", rule.Namespace, "rule", rule.Name, "db", rule.Spec.Database, "table", rule.Spec.Table, "criteria_skipped", false, "inflight_ops", inflightOps, "last_exec_end", lastExecStr, "forward_progress", forward)
}

func operationIDFromResult(iter *kusto.RowIterator) (string, error) {
	defer iter.Stop()

	for {
		row, errInline, errFinal := iter.NextRowOrError()
		if errFinal == io.EOF {
			break
		}
		if errInline != nil {
			continue
		}
		if errFinal != nil {
			return "", fmt.Errorf("failed to retrieve operation ID: %v", errFinal)
		}

		if len(row.Values) != 1 {
			return "", fmt.Errorf("unexpected number of values in row: %d", len(row.Values))
		}

		return row.Values[0].String(), nil
	}

	return "", nil
}

// updateSummaryRuleStatus updates the status of a SummaryRule with proper Kusto error parsing
func (t *SummaryRuleTask) updateSummaryRuleStatus(ctx context.Context, rule *v1.SummaryRule, err error) error {
	return t.store.UpdateStatusWithKustoErrorParsing(ctx, rule, err)
}

type AsyncOperationStatus struct {
	OperationId   string    `kusto:"OperationId"`
	LastUpdatedOn time.Time `kusto:"LastUpdatedOn"`
	State         string    `kusto:"State"`
	ShouldRetry   float64   `kusto:"ShouldRetry"`
	Status        string    `kusto:"Status"`
}

type AuditDiskSpaceTask struct {
	QueueSizer cluster.QueueSizer
	StorageDir string
}

func NewAuditDiskSpaceTask(queueSizer cluster.QueueSizer, storageDir string) *AuditDiskSpaceTask {
	return &AuditDiskSpaceTask{
		QueueSizer: queueSizer,
		StorageDir: storageDir,
	}
}

func (t *AuditDiskSpaceTask) Run(ctx context.Context) error {
	var actualSize int64
	var actualCount int64

	err := filepath.Walk(t.StorageDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Ignore no such file or directory errors
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".wal") {
			actualSize += info.Size()
			actualCount++
		}
		return nil
	})
	if err != nil {
		logger.Errorf("AuditDiskSpaceTask: failed to walk storage dir: %v", err)
		return err
	}

	expectedSize := t.QueueSizer.SegmentsSize()
	expectedCount := t.QueueSizer.SegmentsTotal()

	const oneGB = float64(1 << 30)
	if math.Abs(float64(actualSize-expectedSize)) > oneGB || math.Abs(float64(actualCount-expectedCount)) > 10 {
		logger.Warnf("AuditDiskSpaceTask: WAL segment disk usage mismatch: size actual=%d expected=%d, segments actual=%d expected=%d", actualSize, expectedSize, actualCount, expectedCount)
	}
	return nil
}
