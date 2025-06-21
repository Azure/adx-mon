package adx

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	v1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/ingestor/storage"
	"github.com/Azure/adx-mon/pkg/kustoutil"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/data/errors"
	"github.com/Azure/azure-kusto-go/kusto/kql"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
}

func NewSyncFunctionsTask(store storage.Functions, kustoCli StatementExecutor) *SyncFunctionsTask {
	return &SyncFunctionsTask{
		store:    store,
		kustoCli: kustoCli,
	}
}

func (t *SyncFunctionsTask) Run(ctx context.Context) error {
	functions, err := t.store.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list functions: %w", err)
	}
	for _, function := range functions {

		if function.Spec.Database != v1.AllDatabases && function.Spec.Database != t.kustoCli.Database() {
			continue
		}

		if !function.DeletionTimestamp.IsZero() {
			// Until we can parse KQL we don't actually know the function's
			// name as described in this CRD; however, we'll make the assumption
			// that the CRD name is the same as the function name in Kusto and
			// attempt a delete.
			stmt := kql.New(".drop function ").AddUnsafe(function.Name).AddLiteral(" ifexists")
			if _, err := t.kustoCli.Mgmt(ctx, stmt); err != nil {
				logger.Errorf("Failed to delete function %s.%s: %v", function.Spec.Database, function.Name, err)
				// Deletion is best-effort, especially while we still can't parse KQL
			}
			t.updateKQLFunctionStatus(ctx, function, v1.Success, nil)
			return nil
		}

		// If endpoints have changed, or function is not in Success, re-apply
		if t.kustoCli.Endpoint() != function.Spec.AppliedEndpoint || function.Status.Status != v1.Success || function.GetGeneration() != function.Status.ObservedGeneration {
			stmt := kql.New(".execute database script with (ThrowOnErrors=true) <| ").AddUnsafe(function.Spec.Body)
			if _, err := t.kustoCli.Mgmt(ctx, stmt); err != nil {
				if !errors.Retry(err) {
					logger.Errorf("Permanent failure to create function %s.%s: %v", function.Spec.Database, function.Name, err)
					if err = t.updateKQLFunctionStatus(ctx, function, v1.PermanentFailure, err); err != nil {
						logger.Errorf("Failed to update permanent failure status: %v", err)
					}
					continue
				} else {
					t.updateKQLFunctionStatus(ctx, function, v1.Failed, err)
					logger.Warnf("Transient failure to create function %s.%s: %v", function.Spec.Database, function.Name, err)
					continue
				}
			}

			logger.Infof("Successfully created function %s.%s", function.Spec.Database, function.Name)
			if t.kustoCli.Endpoint() != function.Spec.AppliedEndpoint {
				function.Spec.AppliedEndpoint = t.kustoCli.Endpoint()
				if err := t.store.Update(ctx, function); err != nil {
					logger.Errorf("Failed to update function %s.%s: %v", function.Spec.Database, function.Name, err)
				}
			}

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
	}
	if err := t.store.UpdateStatus(ctx, fn); err != nil {
		return fmt.Errorf("failed to update status for function %s.%s: %w", fn.Spec.Database, fn.Name, err)
	}
	return nil
}

type ManagementCommandTask struct {
	store    storage.CRDHandler
	kustoCli StatementExecutor
}

func NewManagementCommandsTask(store storage.CRDHandler, kustoCli StatementExecutor) *ManagementCommandTask {
	return &ManagementCommandTask{
		store:    store,
		kustoCli: kustoCli,
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
	store                   storage.CRDHandler
	kustoCli                StatementExecutor
	GetOperations           func(ctx context.Context) ([]AsyncOperationStatus, error)
	GetOperationErrorDetail func(ctx context.Context, operationId string) (string, error)
	SubmitRule              func(ctx context.Context, rule v1.SummaryRule, startTime, endTime string) (string, error)
	ClusterLabels           map[string]string
}

func NewSummaryRuleTask(store storage.CRDHandler, kustoCli StatementExecutor, clusterLabels map[string]string) *SummaryRuleTask {
	task := &SummaryRuleTask{
		store:         store,
		kustoCli:      kustoCli,
		ClusterLabels: clusterLabels,
	}
	// Set the default implementations
	task.GetOperations = task.getOperations
	task.GetOperationErrorDetail = task.getOperationErrorDetail
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
	// Fetch all summary rules from storage
	summaryRules := &v1.SummaryRuleList{}
	if err := t.store.List(ctx, summaryRules); err != nil {
		return fmt.Errorf("failed to list summary rules: %w", err)
	}

	// Get the status of all async operations currently tracked in Kusto
	// to match against our rules' operations. If this fails (e.g., Kusto unavailable),
	// we can still process rules and store new async operations in CRD subresources.
	kustoAsyncOperations, err := t.GetOperations(ctx)
	if err != nil {
		logger.Warnf("Failed to get async operations from Kusto, continuing with rule processing: %v", err)
		kustoAsyncOperations = []AsyncOperationStatus{}
	}

	// Process each summary rule individually
	for _, rule := range summaryRules.Items {
		// Skip rules not belonging to the current database
		if rule.Spec.Database != t.kustoCli.Database() {
			continue
		}

		// Check if the rule is enabled for this instance by matching any of the criteria against cluster labels
		if !matchesCriteria(rule.Spec.Criteria, t.ClusterLabels) {
			if logger.IsDebug() {
				logger.Debugf("Skipping %s/%s on %s because none of the criteria matched cluster labels: %v", rule.Namespace, rule.Name, rule.Spec.Database, t.ClusterLabels)
			}
			continue
		}

		// Track if there was a submission error for this rule
		var submissionError error

		// Get the current condition of the rule
		cnd := rule.GetCondition()
		if cnd == nil {
			// For first-time execution, initialize the condition with a timestamp
			// that's one interval back from current time
			cnd = &metav1.Condition{
				LastTransitionTime: metav1.Time{Time: time.Now().Add(-rule.Spec.Interval.Duration)},
			}
		}

		// Calculate the next execution window based on the last successful execution
		var windowStartTime, windowEndTime time.Time

		lastSuccessfulEndTime := rule.GetLastExecutionTime()
		if lastSuccessfulEndTime == nil {
			// First execution: start from current time aligned to interval boundary, going back one interval
			now := time.Now().UTC()
			// Align to minute boundary for consistency
			alignedNow := now.Truncate(time.Minute)
			windowEndTime = alignedNow
			windowStartTime = windowEndTime.Add(-rule.Spec.Interval.Duration)
		} else {
			// Subsequent executions: start from where the last successful execution ended
			windowStartTime = *lastSuccessfulEndTime
			windowEndTime = windowStartTime.Add(rule.Spec.Interval.Duration)

			// Ensure we don't execute future windows
			now := time.Now().UTC().Truncate(time.Minute)
			if windowEndTime.After(now) {
				windowEndTime = now
			}
		}

		// Determine if the rule should be executed based on several criteria:
		// 1. The rule is being deleted
		// 2. Rule has been updated (new generation)
		// 3. It's time for the next interval execution (based on actual time windows)
		shouldSubmitRule := rule.DeletionTimestamp != nil || // Rule is being deleted
			cnd.ObservedGeneration != rule.GetGeneration() || // A new version of this CRD was created
			(lastSuccessfulEndTime != nil && time.Since(*lastSuccessfulEndTime) >= rule.Spec.Interval.Duration) || // Time for next interval
			(lastSuccessfulEndTime == nil && time.Since(cnd.LastTransitionTime.Time) >= rule.Spec.Interval.Duration) // First execution timing

		if shouldSubmitRule {
			// Prepare a new async operation with calculated time range
			asyncOp := v1.AsyncOperation{
				StartTime: windowStartTime.Format(time.RFC3339Nano),
				EndTime:   windowEndTime.Format(time.RFC3339Nano),
			}
			operationId, err := t.SubmitRule(ctx, rule, asyncOp.StartTime, asyncOp.EndTime)
			asyncOp.OperationId = operationId
			rule.SetAsyncOperation(asyncOp)
			rule.SetLastExecutionTime(windowEndTime)

			if err != nil {
				submissionError = err
				t.updateSummaryRuleStatus(ctx, &rule, err)
			}
		}

		// Process any outstanding async operations for this rule
		operations := rule.GetAsyncOperations()
		foundOperations := make(map[string]bool)
		hasFailedOperations := false

		for _, op := range operations {
			found := false
			// Check the status of previously submitted operations
			for _, kustoOp := range kustoAsyncOperations {
				if op.OperationId == kustoOp.OperationId {
					found = true
					foundOperations[op.OperationId] = true

					// Process the operation based on its current state
					shouldRemove, operationFailed := t.processKustoOperation(ctx, &rule, op, kustoOp)
					if operationFailed {
						hasFailedOperations = true
					}
					if shouldRemove {
						rule.RemoveAsyncOperation(kustoOp.OperationId)
						continue
					}

					// Operation is still in progress, so we can skip it
				}
			}

			// Handle operations that weren't found in Kusto's operation list
			if !found {
				t.handleMissingOperation(ctx, &rule, op)
			}
		}

		// Only update status to success if there was no submission error and no failed operations
		if submissionError == nil && !hasFailedOperations {
			if err := t.updateSummaryRuleStatus(ctx, &rule, nil); err != nil {
				logger.Errorf("Failed to update summary rule status: %v", err)
				// Not a lot we can do here, we'll end up just retrying next interval.
			}
		}
	}

	return nil
}

// processKustoOperation handles the processing of a single Kusto operation based on its state
// Returns (shouldRemove, operationFailed) indicating whether the operation should be removed
// from tracking and whether it represents a failed operation
func (t *SummaryRuleTask) processKustoOperation(ctx context.Context, rule *v1.SummaryRule, op v1.AsyncOperation, kustoOp AsyncOperationStatus) (bool, bool) {
	// If operation is completed, handle it appropriately
	if IsKustoAsyncOperationStateCompleted(kustoOp.State) {
		return t.processCompletedOperation(ctx, rule, op, kustoOp)
	}

	// If operation is still in progress but should be retried, submit a retry
	if kustoOp.ShouldRetry != 0 {
		if _, err := t.SubmitRule(ctx, *rule, op.StartTime, op.EndTime); err != nil {
			logger.Errorf("Failed to submit rule: %v", err)
		}
	}

	// Operation is still in progress, don't remove it
	return false, false
}

// processCompletedOperation handles operations that have reached a completed state (success or failure)
// Returns (shouldRemove, operationFailed)
func (t *SummaryRuleTask) processCompletedOperation(ctx context.Context, rule *v1.SummaryRule, op v1.AsyncOperation, kustoOp AsyncOperationStatus) (bool, bool) {
	if kustoOp.State == string(KustoAsyncOperationStateFailed) {
		return t.processFailedOperation(ctx, rule, op, kustoOp)
	}

	// Operation succeeded, remove it from tracking
	return true, false
}

// processFailedOperation handles failed operations, including retry logic and error reporting
// Returns (shouldRemove, operationFailed)
func (t *SummaryRuleTask) processFailedOperation(ctx context.Context, rule *v1.SummaryRule, op v1.AsyncOperation, kustoOp AsyncOperationStatus) (bool, bool) {
	// Get detailed error information for better error messaging
	detailedError := fmt.Sprintf("async operation %s failed", kustoOp.OperationId)
	if errorDetail, err := t.GetOperationErrorDetail(ctx, kustoOp.OperationId); err != nil {
		logger.Debugf("Could not retrieve detailed error for operation %s: %v", kustoOp.OperationId, err)
	} else if errorDetail != "" {
		detailedError = fmt.Sprintf("async operation %s failed: %s", kustoOp.OperationId, errorDetail)
	}

	logger.Errorf("Async operation %s for rule %s.%s failed: %s", kustoOp.OperationId, rule.Spec.Database, rule.Name, detailedError)
	if err := t.updateSummaryRuleStatus(ctx, rule, fmt.Errorf("%s", detailedError)); err != nil {
		logger.Errorf("Failed to update summary rule status for failed operation: %v", err)
	}

	// Check if the failed operation should be retried
	if kustoOp.ShouldRetry != 0 {
		logger.Infof("Retrying failed operation %s for rule %s.%s (ShouldRetry=%v)", kustoOp.OperationId, rule.Spec.Database, rule.Name, kustoOp.ShouldRetry)
		if _, err := t.SubmitRule(ctx, *rule, op.StartTime, op.EndTime); err != nil {
			logger.Errorf("Failed to retry operation: %v", err)
			// Keep the operation for future retry attempts
			return false, true
		}
		// Retry submission succeeded, we can remove the original failed operation
		// The new operation will be tracked separately
	}

	// Remove the failed operation from tracking (either retried successfully or ShouldRetry=0)
	return true, true
}

// handleMissingOperation processes operations that are not found in Kusto's operation list
// This handles operations that might have fallen out of the backlog window or were never submitted
func (t *SummaryRuleTask) handleMissingOperation(ctx context.Context, rule *v1.SummaryRule, op v1.AsyncOperation) {
	// Check for backlog operations, or those that failed to be submitted. We need to attempt
	// executing these so that we don't skip any windows.
	if op.OperationId == "" {
		if operationId, err := t.SubmitRule(ctx, *rule, op.StartTime, op.EndTime); err == nil {
			// Great, we were able to recover the failed submission window.
			op.OperationId = operationId
			rule.SetAsyncOperation(op)
		}
		return
	}

	// If the operation wasn't found in the Kusto operations list after checking all operations,
	// it might have fallen out of the backlog window OR completed very quickly.
	// Only remove it if it's older than 25 hours (giving some buffer beyond Kusto's 24h window)
	if startTime, err := time.Parse(time.RFC3339Nano, op.StartTime); err == nil {
		if time.Since(startTime) > 25*time.Hour {
			logger.Infof("Async operation %s for rule %s has fallen out of the Kusto backlog window, removing",
				op.OperationId, rule.Name)
			rule.RemoveAsyncOperation(op.OperationId)
		} else {
			logger.Debugf("Async operation %s for rule %s not found in Kusto operations but is recent, keeping",
				op.OperationId, rule.Name)
		}
	} else {
		// If we can't parse the time, remove it to avoid keeping broken operations
		logger.Warnf("Async operation %s for rule %s has invalid StartTime format, removing",
			op.OperationId, rule.Name)
		rule.RemoveAsyncOperation(op.OperationId)
	}
}

// applySubstitutions applies time and cluster label substitutions to a KQL query body
func applySubstitutions(body, startTime, endTime string, clusterLabels map[string]string) string {
	// Build the wrapped query with let statements, with direct value substitution
	var letStatements []string

	// Add time parameter definitions with direct datetime substitution
	letStatements = append(letStatements, fmt.Sprintf("let _startTime=datetime(%s);", startTime))
	letStatements = append(letStatements, fmt.Sprintf("let _endTime=datetime(%s);", endTime))

	// Add cluster label parameter definitions with direct value substitution
	// Sort keys to ensure deterministic output
	var keys []string
	for k := range clusterLabels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := clusterLabels[k]
		// Escape any double quotes in the value
		escapedValue := strconv.Quote(v)
		// Add underscore prefix for template substitution
		templateKey := k
		if !strings.HasPrefix(templateKey, "_") {
			templateKey = "_" + templateKey
		}
		letStatements = append(letStatements, fmt.Sprintf("let %s=%s;", templateKey, escapedValue))
	}

	// Construct the full query with let statements
	query := fmt.Sprintf("%s\n%s",
		strings.Join(letStatements, "\n"),
		strings.TrimSpace(body))

	return query
}

func (t *SummaryRuleTask) submitRule(ctx context.Context, rule v1.SummaryRule, startTime, endTime string) (string, error) {
	// NOTE: We cannot do something like `let _startTime = datetime();` as dot-command do not permit
	// preceding let-statements.
	body := applySubstitutions(rule.Spec.Body, startTime, endTime, t.ClusterLabels)

	// Execute asynchronously
	stmt := kql.New(".set-or-append async ").AddUnsafe(rule.Spec.Table).AddLiteral(" <| ").AddUnsafe(body)
	res, err := t.kustoCli.Mgmt(ctx, stmt)
	if err != nil {
		return "", fmt.Errorf("failed to execute summary rule %s.%s: %w", rule.Spec.Database, rule.Name, err)
	}

	return operationIDFromResult(res)
}

func (t *SummaryRuleTask) getOperations(ctx context.Context) ([]AsyncOperationStatus, error) {
	// List all the async operations that have been executed in the last 24 hours. If one of our
	// async operations falls out of this window, it's time to stop trying that particular operation.
	stmt := kql.New(".show operations | where StartedOn > ago(1d) | where Operation == 'TableSetOrAppend' | summarize arg_max(LastUpdatedOn, OperationId, State, ShouldRetry) by OperationId | project LastUpdatedOn, OperationId = tostring(OperationId), State, ShouldRetry = todouble(ShouldRetry) | sort by LastUpdatedOn asc")
	rows, err := t.kustoCli.Mgmt(ctx, stmt)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve async operations: %w", err)
	}
	defer rows.Stop()

	var operations []AsyncOperationStatus
	for {
		row, errInline, errFinal := rows.NextRowOrError()
		if errFinal == io.EOF {
			break
		}
		if errInline != nil {
			continue
		}
		if errFinal != nil {
			return nil, fmt.Errorf("failed to retrieve async operations: %v", errFinal)
		}

		var status AsyncOperationStatus
		if err := row.ToStruct(&status); err != nil {
			return nil, fmt.Errorf("failed to parse async operation: %v", err)
		}
		if status.State == "" {
			continue
		}

		operations = append(operations, status)
	}

	return operations, nil
}

// getOperationErrorDetail retrieves detailed error information for a specific failed operation
func (t *SummaryRuleTask) getOperationErrorDetail(ctx context.Context, operationId string) (string, error) {
	stmt := kql.New(".show operations | where OperationId == '").AddUnsafe(operationId).AddLiteral("' | summarize arg_max(LastUpdatedOn, Status)")
	rows, err := t.kustoCli.Mgmt(ctx, stmt)
	if err != nil {
		return "", fmt.Errorf("failed to retrieve operation error detail: %w", err)
	}
	if rows == nil {
		return "", fmt.Errorf("no result from operation error detail query")
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
			return "", fmt.Errorf("failed to retrieve operation error detail: %v", errFinal)
		}

		if len(row.Values) >= 2 {
			// The Status column should contain the detailed error message
			status := row.Values[1].String()
			if status != "" {
				return status, nil
			}
		}
	}

	return "", nil // No detailed error found
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
